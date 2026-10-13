package command

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestSSHOwnerEscapeEndsLiveReaders covers both independent output cursors and
// an event cursor. Closure is currently a read error, not a guaranteed final
// structured event; clients must not wait indefinitely for SESSION_CLOSED.
func TestSSHOwnerEscapeEndsLiveReaders(t *testing.T) {
	t.Setenv(httpAuthTokenEnvVar, "ssh-lifecycle-test-token")
	args, remoteClosed := sshCommandServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	input, writer := io.Pipe()
	defer input.Close()
	defer writer.Close()
	var output lockedBuffer
	done := make(chan error, 1)
	go func() { done <- runSSH(ctx, args, input, &output, nil) }()
	endpoint := sshTestEndpoint(waitSSHOutput(t, &output, "test-shell$ "))
	observer, err := newMCPAttachSession(ctx, endpoint, "SSH-1")
	if err != nil {
		t.Fatal(err)
	}
	defer observer.Close()
	chunk, err := observer.ReadRecent(ctx, 4096)
	if err != nil {
		t.Fatal(err)
	}
	events := observer.(attachEventSession)
	recentEvents, err := events.ReadRecentEvents(32)
	if err != nil {
		t.Fatal(err)
	}
	reads := make(chan error, 3)
	for range 2 {
		go func() { _, err := observer.ReadOutput(ctx, chunk.Next, 4096); reads <- err }()
	}
	go func() {
		cursor := recentEvents.Next
		for {
			chunk, err := events.ReadEvents(ctx, cursor, 32)
			if err != nil {
				reads <- err
				return
			}
			cursor = chunk.Next
		}
	}()
	if _, err := writer.Write([]byte{0x1d, 'q'}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("owner escape did not close the SSH Host")
	}
	for range 3 {
		select {
		case err := <-reads:
			if err == nil || errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("observer termination = %v", err)
			}
		case <-ctx.Done():
			t.Fatal("observer remained blocked after owner exit")
		}
	}
	select {
	case <-remoteClosed:
	case <-ctx.Done():
		t.Fatal("owner escape leaked the remote connection")
	}
}

// TestSSHCtrlCWhileConnecting cancels a peer that accepts TCP but never sends
// an SSH banner, proving the CLI signal path interrupts setup independently of
// a remote shell or a user-supplied context cancellation.
func TestSSHCtrlCWhileConnecting(t *testing.T) {
	t.Setenv(httpAuthTokenEnvVar, "ssh-lifecycle-test-token")
	args, _ := sshCommandServer(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	_, port, _ := net.SplitHostPort(listener.Addr().String())
	args = append(args, "--port", port)
	accepted := make(chan net.Conn, 1)
	go func() {
		conn, err := listener.Accept()
		if err == nil {
			accepted <- conn
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	interrupts := make(chan os.Signal, 1)
	done := make(chan error, 1)
	go func() { done <- runSSH(ctx, args, strings.NewReader(""), io.Discard, interrupts) }()
	select {
	case conn := <-accepted:
		defer conn.Close()
	case <-ctx.Done():
		t.Fatal("SSH did not reach the stalled peer")
	}
	interrupts <- os.Interrupt
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Ctrl+C during setup = %v", err)
		}
	case <-ctx.Done():
		t.Fatal("Ctrl+C did not cancel SSH setup")
	}
}

// TestSSHRepeatedOwnerExitReleasesWorkers checks transport, Session, Manager,
// attachment and input workers after caller-owned input is also released.
// An arbitrary io.Reader cannot be interrupted by cancelling a context alone.
func TestSSHRepeatedOwnerExitReleasesWorkers(t *testing.T) {
	t.Setenv(httpAuthTokenEnvVar, "ssh-lifecycle-test-token")
	before := sshCLIWorkers()
	for range 6 {
		t.Run("owner", func(t *testing.T) {
			args, closed := sshCommandServer(t)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			input, writer := io.Pipe()
			defer input.Close()
			defer writer.Close()
			var output lockedBuffer
			done := make(chan error, 1)
			go func() { done <- runSSH(ctx, args, input, &output, nil) }()
			waitSSHOutput(t, &output, "test-shell$ ")
			if _, err := writer.Write([]byte{0x1d, 'q'}); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-ctx.Done():
				t.Fatal("SSH owner did not exit")
			}
			select {
			case <-closed:
			case <-ctx.Done():
				t.Fatal("SSH peer remained connected")
			}
		})
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if len(sshCLIWorkers()) <= len(before) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("SSH lifecycle workers remain:\n%s", strings.Join(sshCLIWorkers(), "\n\n"))
}

func sshTestEndpoint(output string) string {
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, "Shared Host: ") {
			return strings.Fields(line)[2]
		}
	}
	return ""
}

func sshCLIWorkers() []string {
	buffer := make([]byte, 2<<20)
	buffer = buffer[:runtime.Stack(buffer, true)]
	var found []string
	for _, stack := range strings.Split(string(buffer), "\n\n") {
		for _, signature := range []string{"golang.org/x/crypto/ssh.", "/internal/core/session.", "command.runSSH.func", "command.forwardAttachInput", "command.newAttachInputPumpContext.func", "command.(*attachInputDispatcher).run"} {
			if strings.Contains(stack, signature) {
				found = append(found, stack)
				break
			}
		}
	}
	return found
}

// TestSSHOwnerInputEOFClosesHost verifies redirected input ending closes the
// owner, even while the remote shell remains silent and ready for more input.
func TestSSHOwnerInputEOFClosesHost(t *testing.T) {
	t.Setenv(httpAuthTokenEnvVar, "ssh-lifecycle-test-token")
	args, closed := sshCommandServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	input, writer := io.Pipe()
	defer input.Close()
	defer writer.Close()
	var output lockedBuffer
	done := make(chan error, 1)
	go func() { done <- runSSH(ctx, args, input, &output, nil) }()
	waitSSHOutput(t, &output, "test-shell$ ")
	_ = writer.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		cancel()
		<-done
		t.Fatal("owner input EOF left SSH Host waiting for remote output")
	}
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("owner input EOF left the SSH connection alive")
	}
}
