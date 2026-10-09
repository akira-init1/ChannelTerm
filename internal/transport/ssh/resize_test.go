package ssh

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/core/channel"
	gossh "golang.org/x/crypto/ssh"
)

// TestResizeExistingShell verifies wire dimensions, invalid sizes, repeated
// changes, and no new PTY/shell request on the established SSH channel.
func TestResizeExistingShell(t *testing.T) {
	server := startTestServer(t, "unread-input")
	stream := connectTestChannel(t, server.config)
	readExactly(t, stream, "ready> ")
	<-server.requests
	<-server.requests
	resizer, ok := stream.(channel.Resizer)
	if !ok {
		t.Fatal("SSH Channel does not implement optional Resizer")
	}
	for _, size := range [][2]uint16{{0, 43}, {132, 0}, {0, 0}} {
		if err := resizer.Resize(size[0], size[1]); err == nil {
			t.Fatalf("accepted invalid dimensions %v", size)
		}
	}
	for _, size := range [][2]uint16{{132, 43}, {80, 24}, {65535, 65535}, {100, 36}} {
		if err := resizer.Resize(size[0], size[1]); err != nil {
			t.Fatal(err)
		}
		select {
		case request := <-server.requests:
			var dimensions struct{ Columns, Rows, Width, Height uint32 }
			if err := gossh.Unmarshal(request.Payload, &dimensions); err != nil {
				t.Fatal(err)
			}
			if request.Type != "window-change" || request.WantReply || dimensions.Columns != uint32(size[0]) || dimensions.Rows != uint32(size[1]) {
				t.Fatalf("request=%s reply=%v dimensions=%+v; want %v", request.Type, request.WantReply, dimensions, size)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("server did not receive window-change")
		}
	}
	if err := stream.Close(); err != nil {
		t.Fatal(err)
	}
	if err := resizer.Resize(132, 43); !errors.Is(err, channel.ErrNotOpen) {
		t.Fatalf("Resize after Close = %v", err)
	}
}

// TestResizeConcurrentClose checks both local cleanup and server disconnect
// while multiple dimension changes are already in flight.
func TestResizeConcurrentClose(t *testing.T) {
	for _, disconnect := range []bool{false, true} {
		t.Run(map[bool]string{false: "close", true: "server-disconnect"}[disconnect], func(t *testing.T) {
			server := startTestServer(t, "unread-input")
			stream := connectTestChannel(t, server.config)
			readExactly(t, stream, "ready> ")
			resizer := stream.(channel.Resizer)
			start := make(chan struct{})
			var workers sync.WaitGroup
			for i := range 16 {
				workers.Go(func() { <-start; _ = resizer.Resize(uint16(80+i), uint16(24+i)) })
			}
			workers.Go(func() {
				<-start
				if disconnect {
					server.disconnect(true)
				}
				_ = stream.Close()
			})
			close(start)
			done := make(chan struct{})
			go func() { workers.Wait(); close(done) }()
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("concurrent Resize/Close did not finish")
			}
			if stream.State() != channel.StateClosed {
				t.Fatal("SSH Channel did not close")
			}
		})
	}
}

// TestCloseInterruptsBlockedResize ensures socket backpressure cannot deadlock
// owner cleanup when it closes the Channel before joining the resize watcher.
func TestCloseInterruptsBlockedResize(t *testing.T) {
	server := startTestServer(t, "unread-input")
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
	resized := make(chan error, 1)
	go func() { resized <- stream.(channel.Resizer).Resize(132, 43) }()
	select {
	case <-broken.blocked:
	case <-time.After(5 * time.Second):
		t.Fatal("resize did not enter the blocked write")
	}
	select {
	case err := <-resized:
		t.Fatalf("Resize returned before the blocked write was released: %v", err)
	default:
	}
	closed := make(chan error, 1)
	go func() { closed <- stream.Close() }()
	for _, done := range []chan error{resized, closed} {
		select {
		case err := <-done:
			if done == resized && err == nil {
				t.Fatal("interrupted resize reported success")
			}
		case <-time.After(5 * time.Second):
			t.Fatal("Close did not release blocked Resize")
		}
	}
}
