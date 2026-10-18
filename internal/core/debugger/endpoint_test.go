package debugger

import (
	"bytes"
	"context"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

func TestBridgeEndpointSafeBindings(t *testing.T) {
	for _, tc := range []struct{ address, peer string }{
		{"0.0.0.0:0", "127.0.0.1"}, {"[::]:0", "::1"}, {"localhost:0", ""},
		{"192.0.2.1:0", ""}, {"127.0.0.1:-1", ""}, {"127.0.0.1:0", "0.0.0.0"},
		{"127.0.0.1:0", "127.0.0.0/8"}, {"127.0.0.1:0", "localhost"},
	} {
		if endpoint, err := ListenBridge(tc.address, tc.peer); err == nil {
			endpoint.Close()
			t.Fatalf("unsafe listener accepted: %+v", tc)
		}
	}
}

func TestBridgeEndpointAuthorizationAndSingleClient(t *testing.T) {
	e, err := ListenBridge("", "")
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	if !strings.HasPrefix(e.Connection().Address, "127.0.0.1:") {
		t.Fatal(e.Connection().Address)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	accepted := make(chan net.Conn, 1)
	go func() { conn, _ := e.Accept(ctx); accepted <- conn }()
	for _, field := range []string{"token", "fingerprint"} {
		bad := e.Connection()
		if field == "token" {
			bad.Token = strings.Repeat("0", 64)
		} else {
			bad.Fingerprint = strings.Repeat("0", 64)
		}
		if conn, err := DialBridge(ctx, bad); err == nil {
			conn.Close()
			t.Fatalf("accepted invalid %s", field)
		}
	}
	client, err := DialBridge(ctx, e.Connection())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	server := <-accepted
	if server == nil {
		t.Fatal("authorized client was rejected")
	}
	defer server.Close()
	client.SetDeadline(time.Now().Add(time.Second))
	server.SetDeadline(time.Now().Add(time.Second))
	payload := []byte{0, 0xff, '$', '}', '#', '+', '-', 3}
	go func() { _, _ = server.Write(payload) }()
	got := make([]byte, len(payload))
	if _, err := io.ReadFull(client, got); err != nil || !bytes.Equal(payload, got) {
		t.Fatalf("binary response: %x %v", got, err)
	}
	go func() { _, _ = client.Write(payload) }()
	if _, err := io.ReadFull(server, got); err != nil || !bytes.Equal(payload, got) {
		t.Fatalf("binary request: %x %v", got, err)
	}
	if second, err := DialBridge(ctx, e.Connection()); err == nil {
		second.Close()
		t.Fatal("second client accepted")
	}
}

func TestBridgeEndpointRejectsOtherIPAndClosesPendingHandshake(t *testing.T) {
	for _, peer := range []string{"127.0.0.2", ""} {
		t.Run(peer, func(t *testing.T) {
			e, err := ListenBridge("", peer)
			if err != nil {
				t.Fatal(err)
			}
			defer e.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() {
				conn, err := e.Accept(ctx)
				if conn != nil {
					conn.Close()
				}
				done <- err
			}()
			conn, err := net.Dial("tcp", e.Connection().Address)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			if peer != "" {
				conn.SetReadDeadline(time.Now().Add(time.Second))
				if _, err := conn.Read(make([]byte, 1)); err != io.EOF {
					t.Fatalf("peer filter did not close: %v", err)
				}
			} else {
				deadline := time.Now().Add(time.Second)
				for {
					e.mu.Lock()
					pending := e.pending != nil
					e.mu.Unlock()
					if pending {
						break
					}
					if time.Now().After(deadline) {
						t.Fatal("handshake did not start")
					}
					time.Sleep(time.Millisecond)
				}
			}
			cancel()
			e.Close()
			e.Close()
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("pending authentication leaked")
			}
		})
	}
}
