package command

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/core/session"
	gossh "golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// sshCommandServer supplies one real SSH handshake with an in-memory shell.
// No test invokes a system shell, requires sshd, or uses personal credentials.
func sshCommandServer(t *testing.T) (args []string, closed <-chan struct{}) {
	t.Helper()
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := gossh.NewSignerFromKey(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	key, err := gossh.MarshalPrivateKey(privateKey, "ephemeral test key")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	identity, trusted := filepath.Join(dir, "id_test"), filepath.Join(dir, "known_hosts")
	if err := os.WriteFile(identity, pem.EncodeToMemory(key), 0600); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(trusted, []byte(knownhosts.Line([]string{listener.Addr().String()}, signer.PublicKey())+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	serverConfig := &gossh.ServerConfig{PublicKeyCallback: func(meta gossh.ConnMetadata, key gossh.PublicKey) (*gossh.Permissions, error) {
		if meta.User() != "test-user" || !bytes.Equal(key.Marshal(), signer.PublicKey().Marshal()) {
			return nil, errors.New("test authentication rejected")
		}
		return nil, nil
	}}
	serverConfig.AddHostKey(signer)
	serverCtx, stop := context.WithCancel(context.Background())
	go func() {
		defer close(done)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		stopClose := context.AfterFunc(serverCtx, func() { _ = conn.Close() })
		defer stopClose()
		server, channels, requests, err := gossh.NewServerConn(conn, serverConfig)
		if err != nil {
			return
		}
		defer server.Close()
		go gossh.DiscardRequests(requests)
		for request := range channels {
			if request.ChannelType() != "session" {
				_ = request.Reject(gossh.UnknownChannelType, "session required")
				continue
			}
			ch, requests, err := request.Accept()
			if err != nil {
				return
			}
			for req := range requests {
				_ = req.Reply(req.Type == "pty-req" || req.Type == "shell", nil)
				if req.Type != "shell" {
					continue
				}
				_, _ = ch.Write([]byte("test-shell$ "))
				reader := bufio.NewReader(ch)
				for {
					line, err := reader.ReadString('\n')
					if err != nil {
						return
					}
					if line == "uname -a\n" {
						_, _ = ch.Write([]byte("Linux test-kernel\r\ntest-shell$ "))
					} else {
						_, _ = ch.Write([]byte("echo: " + line))
					}
				}
			}
		}
	}()
	t.Cleanup(func() {
		stop()
		_ = listener.Close()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("test SSH server leaked")
		}
	})
	_, port, _ := net.SplitHostPort(listener.Addr().String())
	return []string{"test-user@127.0.0.1", "--port", port, "--identity", identity, "--known-hosts", trusted, "--listen", "127.0.0.1:0", "--highlight=false"}, done
}

func waitSSHOutput(t *testing.T, output *lockedBuffer, expected string) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if got := output.String(); strings.Contains(got, expected) {
			return got
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("output did not contain %q: %s", expected, output.String())
	return ""
}

// TestSSHCommandSharesShellWithAttach verifies authenticated shared reads/writes and owner cleanup.
func TestSSHCommandSharesShellWithAttach(t *testing.T) {
	t.Setenv(httpAuthTokenEnvVar, "test-ssh-host-token")
	args, closed := sshCommandServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	input, writer := io.Pipe()
	defer input.Close()
	defer writer.Close()
	var output lockedBuffer
	commandDone := make(chan error, 1)
	go func() { commandDone <- Run(ctx, append([]string{"ssh"}, args...), input, &output, io.Discard) }()
	got := waitSSHOutput(t, &output, "test-shell$ ")
	var endpoint string
	for _, line := range strings.Split(got, "\n") {
		if strings.HasPrefix(line, "Shared Host: ") {
			endpoint = strings.Fields(line)[2]
		}
	}
	if endpoint == "" {
		t.Fatalf("sharing endpoint missing: %s", got)
	}
	// An unauthenticated client must not gain access to the new SSH Session.
	response, err := http.Get(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status=%d", response.StatusCode)
	}
	observer, err := newMCPAttachSession(ctx, endpoint, "SSH-1")
	if err != nil {
		t.Fatal(err)
	}
	defer observer.Close()
	before, err := observer.ReadRecent(ctx, 4096)
	if err != nil || string(before.Data) != "test-shell$ " {
		t.Fatalf("recent=%q, %v", before.Data, err)
	}
	if _, err := writer.Write([]byte("uname -a\n")); err != nil {
		t.Fatal(err)
	}
	waitSSHOutput(t, &output, "Linux test-kernel")
	observed, err := observer.ReadOutput(ctx, before.Next, 4096)
	if err != nil || !bytes.Contains(observed.Data, []byte("Linux test-kernel")) {
		t.Fatalf("observer=%q %v", observed.Data, err)
	}
	// A second cursor sees the same retained bytes without consuming them.
	replayed, err := observer.ReadOutput(ctx, before.Next, 4096)
	if err != nil || !bytes.Equal(observed.Data, replayed.Data) {
		t.Fatalf("independent cursor=%q %v", replayed.Data, err)
	}
	listed, err := listMCPSessions(ctx, endpoint)
	if err != nil || len(listed) != 1 || listed[0].Reference != "SSH-1" || listed[0].Transport != "ssh" {
		t.Fatalf("list=%+v %v", listed, err)
	}
	if _, err := observer.Write(session.WriteRequest{Actor: session.ActorUser, Data: []byte("from-observer\n")}); err != nil {
		t.Fatal(err)
	}
	waitSSHOutput(t, &output, "echo: from-observer")
	if err := observer.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write([]byte("still-open\n")); err != nil {
		t.Fatal(err)
	}
	waitSSHOutput(t, &output, "echo: still-open")
	if _, err := writer.Write([]byte{0x1d, 'q'}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-commandDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("SSH owner did not exit")
	}
	select {
	case <-closed:
	case <-ctx.Done():
		t.Fatal("SSH connection remained open after owner exit")
	}
	if conn, err := net.DialTimeout("tcp", strings.TrimSuffix(strings.TrimPrefix(endpoint, "http://"), "/mcp"), time.Second); err == nil {
		_ = conn.Close()
		t.Fatal("Host listener remained open")
	}
}

// TestSSHCommandHelpAndValidation checks the CLI contract without personal credentials or external servers.
func TestSSHCommandHelpAndValidation(t *testing.T) {
	for _, args := range [][]string{{"ssh", "--help"}, {"ssh", "user@host", "--help"}} {
		var output bytes.Buffer
		if err := Run(context.Background(), args, strings.NewReader(""), &output, io.Discard); err != nil || !strings.Contains(output.String(), "Usage: channelterm ssh USER@HOST") {
			t.Fatalf("help=%s %v", &output, err)
		}
	}
	for _, tc := range []struct {
		args    []string
		message string
	}{
		{nil, "exactly one"},
		{[]string{"host"}, "USER@HOST"},
		{[]string{"user@host", "extra"}, "exactly one"},
		{[]string{"user@host", "--port", "0"}, "port"},
		{[]string{"user@host", "--timeout", "0s"}, "timeout"},
		{[]string{"user@host", "--listen", "0.0.0.0:1234"}, "loopback"},
		{[]string{"user@host", "--listen", "localhost:1234"}, "loopback"},
		{[]string{"user@host", "--identity", filepath.Join(t.TempDir(), "missing")}, "read SSH identity"},
	} {
		err := runSSH(context.Background(), tc.args, strings.NewReader(""), io.Discard, nil)
		if err == nil || !strings.Contains(err.Error(), tc.message) {
			t.Fatalf("args=%v: %v", tc.args, err)
		}
	}
}

// TestSSHCommandRejectsUntrustedHostAndOccupiedListener protects host verification and existing listeners.
func TestSSHCommandRejectsUntrustedHostAndOccupiedListener(t *testing.T) {
	t.Setenv(httpAuthTokenEnvVar, "test-ssh-host-token")
	t.Run("unknown-host", func(t *testing.T) {
		args, _ := sshCommandServer(t)
		empty := filepath.Join(t.TempDir(), "known_hosts")
		if err := os.WriteFile(empty, nil, 0600); err != nil {
			t.Fatal(err)
		}
		args = append(args, "--known-hosts", empty)
		err := runSSH(context.Background(), args, strings.NewReader(""), io.Discard, nil)
		if err == nil || !strings.Contains(err.Error(), "key is unknown") {
			t.Fatalf("untrusted host=%v", err)
		}
	})
	t.Run("occupied-listener", func(t *testing.T) {
		args, _ := sshCommandServer(t)
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer listener.Close()
		args = append(args, "--listen", listener.Addr().String())
		err = runSSH(context.Background(), args, strings.NewReader(""), io.Discard, nil)
		if err == nil || !strings.Contains(err.Error(), "reserve SSH sharing address") {
			t.Fatalf("occupied listener=%v", err)
		}
	})
}

// TestSSHCommandCancelledSetup ensures cancellation reaches the Application boundary.
func TestSSHCommandCancelledSetup(t *testing.T) {
	t.Setenv(httpAuthTokenEnvVar, "test-ssh-host-token")
	args, _ := sshCommandServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := runSSH(ctx, args, strings.NewReader(""), io.Discard, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatal(fmt.Errorf("cancelled setup: %w", err))
	}
}

// TestSSHCommandTerminationClosesLiveObservers covers outstanding HTTP long
// reads during shutdown, not just an already-detached secondary client.
func TestSSHCommandTerminationClosesLiveObservers(t *testing.T) {
	t.Setenv(httpAuthTokenEnvVar, "test-ssh-host-token")
	args, closed := sshCommandServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	input, writer := io.Pipe()
	defer input.Close()
	defer writer.Close()
	interrupts := make(chan os.Signal, 1)
	var output lockedBuffer
	commandDone := make(chan error, 1)
	go func() { commandDone <- runSSH(ctx, args, input, &output, interrupts) }()
	got := waitSSHOutput(t, &output, "test-shell$ ")
	var endpoint string
	for _, line := range strings.Split(got, "\n") {
		if strings.HasPrefix(line, "Shared Host: ") {
			endpoint = strings.Fields(line)[2]
		}
	}
	observer, err := newMCPAttachSession(ctx, endpoint, "SSH-1")
	if err != nil {
		t.Fatal(err)
	}
	defer observer.Close()
	// Ctrl+C during attachment must reach SSH, without terminating the Host.
	interrupts <- os.Interrupt
	waitSSHOutput(t, &output, "test-shell$ ")
	if _, err := writer.Write([]byte("interrupt-probe\n")); err != nil {
		t.Fatal(err)
	}
	waitSSHOutput(t, &output, "interrupt-probe")
	chunk, err := observer.ReadRecent(ctx, 4096)
	if err != nil {
		t.Fatal(err)
	}
	readDone := make(chan error, 1)
	go func() { _, err := observer.ReadOutput(ctx, chunk.Next, 4096); readDone <- err }()
	interrupts <- syscall.SIGTERM
	select {
	case err := <-commandDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("termination signal did not stop SSH Host")
	}
	select {
	case <-readDone:
	case <-ctx.Done():
		t.Fatal("observer read survived Host shutdown")
	}
	select {
	case <-closed:
	case <-ctx.Done():
		t.Fatal("termination signal leaked SSH connection")
	}
}

// TestSSHCLIHelperProcess runs the production command dispatcher in a child
// process so the acceptance test crosses a real process/HTTP boundary.
func TestSSHCLIHelperProcess(t *testing.T) {
	if os.Getenv("CHANNELTERM_TEST_SSH_CLI_HELPER") != "1" {
		return
	}
	for i, arg := range os.Args {
		if arg == "--" {
			if err := Run(context.Background(), os.Args[i+1:], os.Stdin, os.Stdout, os.Stderr); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			os.Exit(0)
		}
	}
	os.Exit(2)
}

// TestSSHAndAttachAcrossProcesses checks two independent CLI processes share SSH output and detach separately.
func TestSSHAndAttachAcrossProcesses(t *testing.T) {
	t.Setenv(httpAuthTokenEnvVar, "test-ssh-process-token")
	args, closed := sshCommandServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	start := func(args []string, output *lockedBuffer) (io.WriteCloser, func()) {
		command := exec.CommandContext(ctx, executable, append([]string{"-test.run=^TestSSHCLIHelperProcess$", "--"}, args...)...)
		command.Env = append(os.Environ(), "CHANNELTERM_TEST_SSH_CLI_HELPER=1")
		command.Stdout, command.Stderr = output, output
		stdin, err := command.StdinPipe()
		if err != nil {
			t.Fatal(err)
		}
		if err := command.Start(); err != nil {
			t.Fatal(err)
		}
		done := make(chan struct{})
		var result error
		go func() { result = command.Wait(); close(done) }()
		t.Cleanup(func() { _ = stdin.Close(); _ = command.Process.Kill(); <-done })
		return stdin, func() {
			select {
			case <-done:
				if result != nil {
					t.Fatalf("CLI process: %v; output: %s", result, output.String())
				}
			case <-ctx.Done():
				t.Fatalf("CLI process did not stop: %s", output.String())
			}
		}
	}
	var ownerOutput, attachedOutput lockedBuffer
	ownerInput, waitOwner := start(append([]string{"ssh"}, args...), &ownerOutput)
	got := waitSSHOutput(t, &ownerOutput, "test-shell$ ")
	var endpoint string
	for _, line := range strings.Split(got, "\n") {
		if strings.HasPrefix(line, "Shared Host: ") {
			endpoint = strings.Fields(line)[2]
		}
	}
	attachedInput, waitAttached := start([]string{"attach", "SSH-1", "--endpoint", endpoint, "--highlight=false"}, &attachedOutput)
	waitSSHOutput(t, &attachedOutput, "test-shell$ ")
	if _, err := ownerInput.Write([]byte("uname -a\n")); err != nil {
		t.Fatal(err)
	}
	waitSSHOutput(t, &ownerOutput, "Linux test-kernel")
	waitSSHOutput(t, &attachedOutput, "Linux test-kernel")
	if _, err := attachedInput.Write([]byte{0x1d, 'q'}); err != nil {
		t.Fatal(err)
	}
	waitAttached()
	if _, err := ownerInput.Write([]byte("owner-still-connected\n")); err != nil {
		t.Fatal(err)
	}
	waitSSHOutput(t, &ownerOutput, "owner-still-connected")
	if _, err := ownerInput.Write([]byte{0x1d, 'q'}); err != nil {
		t.Fatal(err)
	}
	waitOwner()
	select {
	case <-closed:
	case <-ctx.Done():
		t.Fatal("process exit left SSH connected")
	}
}
