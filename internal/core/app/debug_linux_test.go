package app

import (
	"context"
	"github.com/akira-init1/ChannelTerm/internal/core/debugger"
	"github.com/akira-init1/ChannelTerm/internal/core/session"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"
)

func TestDebugSocketAuthenticatesExactLocalProcess(t *testing.T) {
	dir, e := os.MkdirTemp("", "ct-peer-")
	if e != nil {
		t.Fatal(e)
	}
	defer os.RemoveAll(dir)
	listener, e := net.Listen("unix", filepath.Join(dir, "rsp"))
	if e != nil {
		t.Fatal(e)
	}
	defer listener.Close()
	conn, e := net.Dial("unix", listener.Addr().String())
	if e != nil {
		t.Fatal(e)
	}
	defer conn.Close()
	peer, e := listener.Accept()
	if e != nil {
		t.Fatal(e)
	}
	defer peer.Close()
	if e = authenticateDebugPeer(peer, os.Getpid()+1); e == nil {
		t.Fatal("different PID authenticated")
	}
	if e = authenticateDebugPeer(peer, os.Getpid()); e != nil {
		t.Fatal(e)
	}
}
func TestDebugServerPIDFileRejectsReusedIdentity(t *testing.T) {
	dir := t.TempDir()
	if e := os.WriteFile(filepath.Join(dir, "server.pid"), []byte("1 invalid-start-time"), 0600); e != nil {
		t.Fatal(e)
	}
	r := &debugRun{dir: dir}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if e := r.openServer(ctx); e == nil || r.server != nil {
		t.Fatalf("stale PID trusted: %v", e)
	}
}

func simulatedDebugRun(t *testing.T) *debugRun {
	t.Helper()
	manager := session.NewManager()
	terminal := newFakeConnectedSession("simulated-debug")
	if e := terminal.Connect(context.Background()); e != nil {
		t.Fatal(e)
	}
	if e := manager.Register(terminal); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { manager.Close() })
	a, e := New(Dependencies{Manager: manager})
	if e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(t.TempDir(), "fake-mi")
	if e = os.WriteFile(path, []byte("#!/bin/sh\nwhile IFS= read -r line;do token=${line%%-*};printf '%s^done\\n' \"$token\";done\n"), 0700); e != nil {
		t.Fatal(e)
	}
	client, e := debugger.Start(path, nil)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(client.Close)
	r := &debugRun{app: a, terminal: terminal, owner: "internal-owner", id: "debug-simulated", token: "fixture", state: "stopped", client: client, changed: make(chan struct{}), finished: make(chan struct{}), expires: time.Now().Add(time.Minute), touched: true}
	r.protocol = &terminalCommandProtocol{terminal: terminal, token: r.token}
	a.debug.runs = map[string]*debugRun{r.id: r}
	if _, e = a.AcquireLease(terminal.ID(), r.owner, LeaseTypeDebug); e != nil {
		t.Fatal(e)
	}
	return r
}

func TestDebugClientHeartbeatExpiryDetachesAndProbes(t *testing.T) {
	r := simulatedDebugRun(t)
	terminal := r.terminal.(*fakeConnectedSession)
	terminal.emitOutput([]byte("\x1d\x1cfixture:0\x1c\x1d"))
	tokenPattern := regexp.MustCompile(`'([0-9a-f]{12})''([0-9a-f]{12})'`)
	terminal.onWrite = func(q session.WriteRequest) {
		m := tokenPattern.FindSubmatch(q.Data)
		if len(m) == 3 {
			terminal.emitOutput([]byte("\x1e\x1f" + string(m[1]) + string(m[2]) + "\x1f\x1e"))
		}
	}
	if e := r.app.ReleaseLease(terminal.ID(), r.owner); e == nil {
		t.Fatal("managed lease released outside stop")
	}
	r.expires = time.Now().Add(-time.Second)
	go r.monitor()
	select {
	case <-r.finished:
	case <-time.After(3 * time.Second):
		t.Fatal("heartbeat expiry did not recover")
	}
	if s := r.status(); s.State != "closed" {
		t.Fatalf("expiry result: %+v", s)
	}
	if terminal.closed {
		t.Fatal("successful simulated recovery closed Session")
	}
}

func TestDebugLostOutputClosesSession(t *testing.T) {
	r := simulatedDebugRun(t)
	r.bridgeDone = make(chan struct{})
	r.bridgeErr = debugger.ErrDataLost
	r.bridgeStarted = true
	close(r.bridgeDone)
	go r.monitor()
	select {
	case <-r.finished:
	case <-time.After(time.Second):
		t.Fatal("buffer loss did not terminate debugging")
	}
	if !r.terminal.(*fakeConnectedSession).closed {
		t.Fatal("output loss left Session open")
	}
	if _, e := r.app.WriteSession(context.Background(), r.terminal.ID(), session.WriteRequest{Actor: session.ActorUser, Data: []byte("unsafe")}); e == nil {
		t.Fatal("ordinary writes reopened")
	}
}

func TestDebugMissingFinalACKCannotRestore(t *testing.T) {
	r := simulatedDebugRun(t)
	r.state = "exited"
	r.bridgeDone = make(chan struct{})
	close(r.bridgeDone)
	r.terminal.(*fakeConnectedSession).emitOutput([]byte("\x1d\x1cfixture:0\x1c\x1d"))
	if e := r.stop(false); e == nil {
		t.Fatal("missing final ACK returned success")
	}
	if s := r.status(); s.State != "failed" {
		t.Fatal(s)
	}
}

func TestDebugExpiredHeartbeatCannotBeResurrected(t *testing.T) {
	r := simulatedDebugRun(t)
	r.expires = time.Now().Add(-time.Second)
	r.request.Owner = "client-owner"
	r.commands = map[string]debugCommandResult{}
	if _, e := r.app.DebugCommand(context.Background(), DebugCommandRequest{DebugID: r.id, Owner: r.request.Owner, RequestID: "late-renew", Action: "renew"}); e == nil {
		t.Fatal("late command resurrected heartbeat")
	}
	r.mu.Lock()
	r.appendLocked(debugger.Record{Kind: "lifecycle", Class: "stopped"})
	r.mu.Unlock()
	if _, e := r.app.ReadDebugEvents(context.Background(), r.id, r.request.Owner, 0); e != nil {
		t.Fatal(e)
	}
	if r.status().ExpiresAt.After(time.Now()) {
		t.Fatal("event read resurrected expired heartbeat")
	}
	r.fail(context.DeadlineExceeded)
}
