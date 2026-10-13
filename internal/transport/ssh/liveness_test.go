package ssh

import (
	"bytes"
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

func TestKeepAliveUnansweredProbeClosesShell(t *testing.T) {
	server := startTestServer(t, "ignore-keepalive")
	server.config.KeepAliveInterval = 30 * time.Millisecond
	server.config.KeepAliveCountMax = 2
	stream := connectTestChannel(t, server.config)
	readExactly(t, stream, "ready> ")
	done := make(chan error, 1)
	go func() { _, err := stream.Read(make([]byte, 1)); done <- err }()
	select {
	case err := <-done:
		if !errors.Is(err, ErrKeepAliveTimeout) {
			t.Fatalf("read = %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("unanswered probe did not close shell")
	}
	if err := stream.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestKeepAliveNegativeReplyKeepsShellAlive(t *testing.T) {
	server := startTestServer(t, "") // DiscardRequests sends request-failure replies.
	server.config.KeepAliveInterval = 20 * time.Millisecond
	server.config.KeepAliveCountMax = 3
	server.config.WriteTimeout = 100 * time.Millisecond
	stream := connectTestChannel(t, server.config)
	readExactly(t, stream, "ready> ")
	time.Sleep(150 * time.Millisecond)
	if _, err := stream.Write([]byte("still-alive\n")); err != nil {
		t.Fatal(err)
	}
	readExactly(t, stream, "echo: still-alive\n")
	time.Sleep(150 * time.Millisecond)
	if _, err := stream.Write([]byte("after-write-deadline\n")); err != nil {
		t.Fatal("successful write left an armed timer", err)
	}
	readExactly(t, stream, "echo: after-write-deadline\n")
	if err := stream.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestLivenessTimeoutInterruptsBlockedSocket(t *testing.T) {
	for _, operation := range []string{"probe", "resize"} {
		t.Run(operation, func(t *testing.T) {
			server := startTestServer(t, "")
			want := ErrKeepAliveTimeout
			if operation == "probe" {
				server.config.KeepAliveInterval = 30 * time.Millisecond
				server.config.KeepAliveCountMax = 2
			} else {
				server.config.WriteTimeout = 50 * time.Millisecond
				want = ErrWriteTimeout
			}
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
			done := make(chan error, 1)
			go func() {
				if operation == "resize" {
					done <- stream.(*shellChannel).Resize(100, 40)
				} else {
					_, err := stream.Read(make([]byte, 1))
					done <- err
				}
			}()
			select {
			case err := <-done:
				if !errors.Is(err, want) {
					t.Fatalf("timeout=%v", err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("blocked socket survived timeout")
			}
		})
	}
}

func TestWriteTimeoutInterruptsChannelFlowControl(t *testing.T) {
	server := startTestServer(t, "unread-input")
	server.config.WriteTimeout = 50 * time.Millisecond
	stream := connectTestChannel(t, server.config)
	readExactly(t, stream, "ready> ")
	done := make(chan error, 1)
	go func() { _, err := stream.Write(bytes.Repeat([]byte("x"), 4<<20)); done <- err }()
	select {
	case err := <-done:
		if !errors.Is(err, ErrWriteTimeout) {
			t.Fatalf("write = %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("write timeout did not release flow control")
	}
	if err := stream.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestKeepAliveCloseJoinsBlockedProbe(t *testing.T) {
	server := startTestServer(t, "")
	server.config.KeepAliveInterval = 20 * time.Millisecond
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
	select {
	case <-broken.blocked:
	case <-time.After(3 * time.Second):
		t.Fatal("probe did not start")
	}
	done := make(chan error, 2)
	for range 2 {
		go func() { done <- stream.Close() }()
	}
	for range 2 {
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Fatal("Close did not join blocked probe")
		}
	}
}

func TestLivenessConfigurationValidation(t *testing.T) {
	server := startTestServer(t, "")
	for _, alter := range []func(*Config){
		func(c *Config) { c.KeepAliveInterval = -1 },
		func(c *Config) { c.KeepAliveCountMax = -1 },
		func(c *Config) { c.WriteTimeout = -1 },
		func(c *Config) { c.KeepAliveInterval = time.Duration(1 << 62); c.KeepAliveCountMax = 3 },
	} {
		config := server.config
		alter(&config)
		if _, err := New(config); err == nil {
			t.Fatal("invalid liveness configuration accepted")
		}
	}
}
