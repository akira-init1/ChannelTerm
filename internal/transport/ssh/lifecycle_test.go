package ssh

import (
	"context"
	"errors"
	"io"
	"net"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/core/channel"
	"github.com/akira-init1/ChannelTerm/internal/core/session"
)

// TestSessionDisconnectAndReopen checks both orderly server EOF and a TCP reset.
// The Manager must reap the old Session; recovery is an explicit new connection,
// never replaying input or silently reusing a dead shell.
func TestSessionDisconnectAndReopen(t *testing.T) {
	for _, reset := range []bool{false, true} {
		name := "server-close"
		if reset {
			name = "network-reset"
		}
		t.Run(name, func(t *testing.T) {
			server := startTestServer(t, "")
			manager := session.NewManager()
			t.Cleanup(func() { _ = manager.Close() })
			transport, err := New(server.config)
			if err != nil {
				t.Fatal(err)
			}
			for _, id := range []string{"before-disconnect", "after-reconnect"} {
				terminal, err := session.New(id, transport)
				if err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if err := terminal.Connect(ctx); err != nil {
					t.Fatal(err)
				}
				if err := manager.Register(terminal); err != nil {
					_ = terminal.Close()
					t.Fatal(err)
				}
				chunk, err := terminal.ReadOutput(ctx, 0, 4096)
				if err != nil || string(chunk.Data) != "ready> " {
					t.Fatalf("initial shell = %q, %v", chunk.Data, err)
				}
				events, err := terminal.ReadRecentEvents(32)
				if err != nil {
					t.Fatal(err)
				}
				reads := make(chan error, 3)
				for range 2 {
					go func() { _, err := terminal.ReadOutput(ctx, chunk.Next, 4096); reads <- err }()
				}
				go func() { _, err := terminal.ReadEvents(ctx, events.Next, 32); reads <- err }()
				server.disconnect(reset)
				for range 3 {
					select {
					case err := <-reads:
						if err == nil || errors.Is(err, context.DeadlineExceeded) {
							t.Fatalf("disconnect did not end waiting reader: %v", err)
						}
					case <-ctx.Done():
						t.Fatal("Session output/event reader survived disconnect")
					}
				}
				waitSSHCondition(t, func() bool {
					_, registered := manager.Get(id)
					return !registered && terminal.State() == session.StateClosed
				}, "Manager did not close and unregister the disconnected Session")
				if _, err := terminal.Write(session.WriteRequest{Actor: session.ActorUser, Data: []byte("must-not-replay\n")}); !errors.Is(err, session.ErrNotOpen) {
					t.Fatalf("dead Session accepted input: %v", err)
				}
			}
		})
	}
}

// TestRepeatedChannelCloseReleasesWorkers checks owned SSH workers across
// repeated opens, including remote exits. It filters stack signatures instead
// of comparing total goroutine counts, which include unrelated runtime work.
func TestRepeatedChannelCloseReleasesWorkers(t *testing.T) {
	before := sshWorkerStacks()
	t.Run("connections", func(t *testing.T) {
		server := startTestServer(t, "")
		for range 8 {
			stream := connectTestChannel(t, server.config)
			readExactly(t, stream, "ready> ")
			if _, err := stream.Write([]byte("exit\n")); err != nil {
				t.Fatal(err)
			}
			if _, err := io.Copy(io.Discard, stream); err != nil {
				t.Fatal(err)
			}
			if err := stream.Close(); err != nil {
				t.Fatal(err)
			}
			if stream.State() != channel.StateClosed {
				t.Fatal("Channel did not reach closed state")
			}
		}
	})
	waitSSHCondition(t, func() bool { return len(sshWorkerStacks()) <= len(before) }, "SSH goroutines survived all Channel and server closes")
}

func sshWorkerStacks() []string {
	buffer := make([]byte, 2<<20)
	buffer = buffer[:runtime.Stack(buffer, true)]
	var found []string
	for _, stack := range strings.Split(string(buffer), "\n\n") {
		if strings.Contains(stack, "golang.org/x/crypto/ssh.") || strings.Contains(stack, "/internal/transport/ssh.(*Transport).Connect.func") {
			found = append(found, stack)
		}
	}
	return found
}

func waitSSHCondition(t *testing.T, condition func() bool, message string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("%s\n%s", message, strings.Join(sshWorkerStacks(), "\n\n"))
}

// TestBlackholedConnectionRequiresClose checks a silent network failure rather
// than a FIN/RST. A setup deadline is not a shell liveness timeout. Local Close
// must still release an SSH write blocked by the simulated blackhole.
func TestBlackholedConnectionRequiresClose(t *testing.T) {
	server := startTestServer(t, "")
	transport, err := New(server.config)
	if err != nil {
		t.Fatal(err)
	}
	var broken *blackholeConn
	transport.dial = func(ctx context.Context, network, address string) (net.Conn, error) {
		conn, err := (&net.Dialer{}).DialContext(ctx, network, address)
		if err != nil {
			return nil, err
		}
		broken = &blackholeConn{Conn: conn, blocked: make(chan struct{}), closed: make(chan struct{})}
		return broken, nil
	}
	stream, err := transport.Connect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	readExactly(t, stream, "ready> ")
	broken.drop.Store(true)
	readDone, writeDone := make(chan error, 1), make(chan error, 1)
	go func() { _, err := stream.Read(make([]byte, 1)); readDone <- err }()
	go func() { _, err := stream.Write([]byte("must-not-replay\n")); writeDone <- err }()
	select {
	case <-broken.blocked:
	case <-time.After(5 * time.Second):
		t.Fatal("test did not intercept the SSH packet write")
	}
	select {
	case err := <-readDone:
		t.Fatalf("silent blackhole unexpectedly ended Read: %v", err)
	case err := <-writeDone:
		t.Fatalf("silent blackhole unexpectedly ended Write: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	closed := make(chan error, 1)
	go func() { closed <- stream.Close() }()
	for _, done := range []chan error{closed, readDone, writeDone} {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("Close did not interrupt blackholed SSH I/O")
		}
	}
}

// blackholeConn stops carrying packets after setup without notifying the SSH
// peer. Closing it releases both an already blocked read and any stalled write.
type blackholeConn struct {
	net.Conn
	drop    atomic.Bool
	once    sync.Once
	blocked chan struct{}
	closed  chan struct{}
}

// Write stalls a packet after fault injection until the connection is closed.
func (c *blackholeConn) Write(data []byte) (int, error) {
	if c.drop.Load() {
		select {
		case <-c.blocked:
		default:
			close(c.blocked)
		}
		<-c.closed
		return 0, net.ErrClosed
	}
	return c.Conn.Write(data)
}

// Close releases simulated backpressure before closing the real socket.
func (c *blackholeConn) Close() error {
	c.once.Do(func() { close(c.closed) })
	return c.Conn.Close()
}
