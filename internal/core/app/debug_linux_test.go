package app

import (
	"bytes"
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/core/debugger"
	"github.com/akira-init1/ChannelTerm/internal/core/session"
)

func TestDebugCommandsPreserveBashHistory(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("Bash is unavailable")
	}
	const token = "00112233445566778899aabb"
	r := &debugRun{dir: t.TempDir(), token: token}
	if err := os.WriteFile(filepath.Join(r.dir, "identity"), []byte("wrong-identity"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []struct{ name, setup string }{
		{"normal", ":"},
		{"ignore-space", "HISTCONTROL=ignorespace"},
		{"ignore-command", "HISTIGNORE='*history*'"},
		{"disabled", "set +o history"},
		{"timestamps", "HISTTIMEFORMAT='%F %T '"},
	} {
		for _, command := range []struct {
			name, setup, text string
			marker            bool
		}{
			// Abort before starting a server: these cases isolate bootstrap history
			// cleanup from GDB and exercise two startup error paths.
			{"bootstrap-stty-error", "stty() { return 1; }", r.bootstrap() + "\n", false},
			{"bootstrap-identity-error", "stty() { printf '1:2:3'; }", r.bootstrap() + "\n", false},
			{"shell-probe", ":", debugShellProbeCommand(token), true},
		} {
			t.Run(mode.name+"/"+command.name, func(t *testing.T) {
				historyPath := filepath.Join(t.TempDir(), "history")
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				cmd := exec.CommandContext(ctx, bash, "--noprofile", "--norc", "-i")
				cmd.Env = append(os.Environ(), "HISTFILE=/dev/null", "HISTSIZE=100", "HISTCONTROL=", "HISTIGNORE=", "HISTTIMEFORMAT=", "PS1=", "PS2=")
				cmd.Stdin = strings.NewReader(command.setup + "\nhistory -c\n" + mode.setup + "\necho USER_BEFORE_DEBUG\n" + command.text + "echo USER_AFTER_DEBUG\nhistory -w " + quoteTerminalCommand(historyPath) + "\nexit 0\n")
				output, err := cmd.CombinedOutput()
				if err != nil {
					t.Fatalf("Bash: %v, output=%q", err, output)
				}
				history, err := os.ReadFile(historyPath)
				if err != nil {
					t.Fatal(err)
				}
				if strings.Contains(string(history), token[:12]) || strings.Contains(string(history), "history -d") {
					t.Fatalf("debug command retained in history: %s", history)
				}
				want := []string{"echo USER_BEFORE_DEBUG\n", "echo USER_AFTER_DEBUG\n"}
				if mode.name == "disabled" {
					want = []string{"set +o history\n"}
				}
				for _, line := range want {
					if !strings.Contains(string(history), line) {
						t.Fatalf("user history lost %q: %s", line, history)
					}
				}
				if got := bytes.Contains(output, []byte("\x1e\x1f"+token+"\x1f\x1e")); got != command.marker {
					t.Fatalf("shell response=%t, want %t; output=%q", got, command.marker, output)
				}
			})
		}
	}
}

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
