package debugger

import (
	"context"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

func keepaliveEndpoint(t *testing.T, timeout time.Duration) (*Endpoint, <-chan net.Conn) {
	t.Helper()
	e, err := ListenBridge("127.0.0.1:0", "")
	if err != nil {
		t.Fatal(err)
	}
	e.aliveTimeout = timeout
	t.Cleanup(e.Close)
	accepted := make(chan net.Conn, 1)
	go func() { conn, _ := e.Accept(context.Background()); accepted <- conn }()
	return e, accepted
}

func waitClientAlive(t *testing.T, e *Endpoint) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for !e.ClientAlive() {
		if time.Now().After(deadline) {
			t.Fatal("control heartbeat did not establish liveness")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestBridgeKeepaliveWithoutRSPAndClose(t *testing.T) {
	e, accepted := keepaliveEndpoint(t, bridgeAliveTimeout)
	client, err := DialBridgeKeepAlive(context.Background(), e.Connection())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	server := <-accepted
	if server == nil {
		t.Fatal("data authentication failed")
	}
	defer server.Close()
	waitClientAlive(t, e)
	// No challenge, response, or authentication bytes may enter RSP.
	server.SetReadDeadline(time.Now().Add(30 * time.Millisecond))
	if n, err := server.Read(make([]byte, 1)); n != 0 || err == nil {
		t.Fatalf("control bytes reached data stream: %d %v", n, err)
	}
	if second, err := DialBridgeKeepAlive(context.Background(), e.Connection()); err == nil {
		second.Close()
		t.Fatal("second data client admitted")
	}
	e.Close()
	e.Close()
	if e.ClientAlive() {
		t.Fatal("closed endpoint still alive")
	}
	client.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := client.Read(make([]byte, 1)); err == nil {
		t.Fatal("management close left data connected")
	}
}

func TestBridgeKeepaliveMissingSilentAndInvalidControl(t *testing.T) {
	for _, mode := range []string{"missing", "silent", "wrong-response", "closed"} {
		t.Run(mode, func(t *testing.T) {
			e, accepted := keepaliveEndpoint(t, 150*time.Millisecond)
			data, err := dialBridge(context.Background(), e.Connection(), bridgeAlivePreface)
			if err != nil {
				t.Fatal(err)
			}
			defer data.Close()
			server := <-accepted
			if server == nil {
				t.Fatal("data authentication failed")
			}
			defer server.Close()
			if mode != "missing" {
				control, err := dialBridge(context.Background(), e.Connection(), bridgeControlPreface)
				if err != nil {
					t.Fatal(err)
				}
				defer control.Close()
				if mode == "wrong-response" {
					control.Write(make([]byte, 16))
				} else if mode == "closed" {
					control.Close()
				}
			}
			data.SetReadDeadline(time.Now().Add(2 * time.Second))
			_, err = data.Read(make([]byte, 1))
			if err == nil {
				t.Fatal("failed control left data connected")
			}
			if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
				t.Fatal("client deadline, not control failure, closed data")
			}
			if e.ClientAlive() || e.ClientError() == nil {
				t.Fatal("failed control reported healthy")
			}
		})
	}
}

func TestBridgeControlAuthenticationAndDataEOF(t *testing.T) {
	e, accepted := keepaliveEndpoint(t, bridgeAliveTimeout)
	data, err := dialBridge(context.Background(), e.Connection(), bridgeAlivePreface)
	if err != nil {
		t.Fatal(err)
	}
	defer data.Close()
	server := <-accepted
	if server == nil {
		t.Fatal("data authentication failed")
	}
	defer server.Close()
	bad := e.Connection()
	bad.Token = strings.Repeat("0", 64)
	if conn, err := dialBridge(context.Background(), bad, bridgeControlPreface); err == nil {
		conn.Close()
		t.Fatal("wrong control token accepted")
	}
	control, err := dialBridge(context.Background(), e.Connection(), bridgeControlPreface)
	if err != nil {
		t.Fatal(err)
	}
	defer control.Close()
	var challenge [16]byte
	if _, err := io.ReadFull(control, challenge[:]); err != nil {
		t.Fatal(err)
	}
	if _, err := control.Write(challenge[:]); err != nil {
		t.Fatal(err)
	}
	waitClientAlive(t, e)
	server.Close() // relay joins after data EOF, with no shell-restoration claim
	if e.ClientAlive() {
		t.Fatal("data EOF kept heartbeat alive")
	}
	control.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := control.Read(challenge[:]); err == nil {
		t.Fatal("data close did not close control")
	}
}
