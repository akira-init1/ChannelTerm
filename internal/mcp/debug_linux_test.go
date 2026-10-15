package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/core/app"
	"github.com/akira-init1/ChannelTerm/internal/core/channel"
	"github.com/akira-init1/ChannelTerm/internal/core/session"
	"github.com/akira-init1/ChannelTerm/internal/core/tool"
	"github.com/akira-init1/ChannelTerm/internal/mcp/terminal"
	protocol "github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/sys/unix"
)

type debugPTYTransport struct{ master *os.File }

func (p debugPTYTransport) Connect(context.Context) (channel.Channel, error) {
	return channel.NewStream(p.master)
}

type debugFixture struct {
	shell *exec.Cmd
	mu    sync.Mutex
	tail  []byte

	terminal session.Session
	app      *app.Application
	client   *protocol.ClientSession
	slave    *os.File
	original *unix.Termios
	elf      string
	owner    string
	id       string
	cursor   uint64
	sequence int
}

func newDebugFixture(t *testing.T) *debugFixture {
	t.Helper()
	if os.Getenv("CHANNELTERM_TEST_GDB") != "1" {
		t.Skip("opt-in real local GDB/gdbserver/PTY test: CHANNELTERM_TEST_GDB=1")
	}
	for _, name := range []string{"gdb", "gdbserver", "gcc", "bash", "setsid"} {
		if _, e := exec.LookPath(name); e != nil {
			t.Fatal(e)
		}
	}
	dir := t.TempDir()
	source := filepath.Join(dir, "addition.c")
	elf := filepath.Join(dir, "addition")
	code := `#include <pthread.h>
#include <unistd.h>
#include <sys/prctl.h>
volatile int done;
void *worker(void *p) { while (!done) usleep(1000); return 0; }
int plus(int a, int b) {
 int result = a + b;
 return result;
}
int add(int a, int b) {
 int sum = plus(a, b);
 return sum;
}
int main(int argc, char **argv) {
 prctl(PR_SET_PTRACER, PR_SET_PTRACER_ANY, 0, 0, 0);
 if (argc > 1) { sleep(60); return 0; }
 pthread_t thread;
 pthread_create(&thread, 0, worker, 0);
 int result = add(3, 4);
 done = 1;
 pthread_join(thread, 0);
 return result == 7 ? 0 : 1;
}
`
	if e := os.WriteFile(source, []byte(code), 0600); e != nil {
		t.Fatal(e)
	}
	if out, e := exec.Command("gcc", "-g", "-O0", "-fno-omit-frame-pointer", "-pthread", "-o", elf, source).CombinedOutput(); e != nil {
		t.Fatalf("gcc: %v %s", e, out)
	}
	fd, e := unix.Open("/dev/ptmx", unix.O_RDWR|unix.O_NOCTTY|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if e != nil {
		t.Fatal(e)
	}
	master := os.NewFile(uintptr(fd), "debug-pty")
	t.Cleanup(func() { master.Close() })
	if e = unix.IoctlSetPointerInt(fd, unix.TIOCSPTLCK, 0); e != nil {
		t.Fatal(e)
	}
	n, e := unix.IoctlGetInt(fd, unix.TIOCGPTN)
	if e != nil {
		t.Fatal(e)
	}
	slave, e := os.OpenFile(fmt.Sprintf("/dev/pts/%d", n), os.O_RDWR|unix.O_NOCTTY, 0)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { slave.Close() })
	shell := exec.Command("bash", "--noprofile", "--norc", "-i")
	shell.Stdin = slave
	shell.Stdout = slave
	shell.Stderr = slave
	shell.Env = append(os.Environ(), "PS1=CT_DEBUG_READY> ", "HISTFILE=/dev/null", "TERM=dumb")
	shell.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	if e = shell.Start(); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { shell.Process.Kill(); shell.Wait() })
	s, e := session.New("debug-pty", debugPTYTransport{master})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Connect(context.Background()); e != nil {
		t.Fatal(e)
	}
	manager := session.NewManager()
	if e = manager.Register(s); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { manager.Close() })
	a, e := app.New(app.Dependencies{Manager: manager})
	if e != nil {
		t.Fatal(e)
	}
	tools, e := terminal.NewTools(a)
	if e != nil {
		t.Fatal(e)
	}
	registry := tool.NewRegistry()
	for _, x := range tools {
		if e = registry.Register(x); e != nil {
			t.Fatal(e)
		}
	}
	client, closeClient := connectTestClient(t, registry)
	t.Cleanup(closeClient)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cursor := session.OutputCursor(0)
	output := ""
	for !strings.Contains(output, "CT_DEBUG_READY>") {
		chunk, e := s.ReadOutput(ctx, cursor, 4096)
		if e != nil {
			t.Fatal(e)
		}
		output += string(chunk.Data)
		cursor = chunk.Next
	}
	original, e := unix.IoctlGetTermios(int(slave.Fd()), unix.TCGETS)
	if e != nil {
		t.Fatal(e)
	}
	fixture := &debugFixture{shell: shell, terminal: s, app: a, client: client, slave: slave, original: original, elf: elf, owner: strings.Repeat("test-owner-", 4)}
	watch, stopWatch := context.WithCancel(context.Background())
	t.Cleanup(stopWatch)
	go func() {
		cur := cursor
		for {
			chunk, e := s.ReadOutput(watch, cur, 4096)
			if e != nil {
				return
			}
			cur = chunk.Next
			fixture.mu.Lock()
			fixture.tail = append(fixture.tail, chunk.Data...)
			if len(fixture.tail) > 4096 {
				fixture.tail = fixture.tail[len(fixture.tail)-4096:]
			}
			fixture.mu.Unlock()
		}
	}()
	return fixture
}
func (f *debugFixture) call(t *testing.T, name string, args map[string]any) map[string]any {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	result, e := f.client.CallTool(ctx, &protocol.CallToolParams{Name: name, Arguments: args})
	if e != nil {
		t.Fatal(e)
	}
	if result.IsError {
		data, _ := json.Marshal(result.Content)
		tail, _ := f.terminal.ReadRecent(1500)
		t.Logf("terminal tail: %q", tail.Data)
		t.Fatalf("%s: %s", name, data)
	}
	data, e := json.Marshal(result.StructuredContent)
	if e != nil {
		t.Fatal(e)
	}
	var out map[string]any
	if e = json.Unmarshal(data, &out); e != nil {
		t.Fatal(e)
	}
	return out
}
func (f *debugFixture) start(t *testing.T, pid int) {
	t.Helper()
	q := map[string]any{"session_id": "debug-pty", "owner": f.owner, "request_id": "start-addition", "elf": f.elf, "pid": pid}
	out := f.call(t, "debug_start", q)
	f.id = out["status"].(map[string]any)["debug_id"].(string)
	again := f.call(t, "debug_start", q)
	if out["status"].(map[string]any)["debug_id"] != again["status"].(map[string]any)["debug_id"] {
		t.Fatalf("start retry changed result: %v / %v", out, again)
	}
	t.Cleanup(func() {
		if f.id != "" {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			_, _ = f.app.StopDebug(ctx, f.id, f.owner, false)
		}
	})
}
func (f *debugFixture) command(t *testing.T, action, argument string) map[string]any {
	t.Helper()
	f.sequence++
	return f.call(t, "debug_command", map[string]any{"debug_id": f.id, "owner": f.owner, "request_id": fmt.Sprint(f.sequence), "action": action, "argument": argument})
}
func (f *debugFixture) wait(t *testing.T, class string) map[string]any {
	t.Helper()
	for {
		out := f.call(t, "debug_events", map[string]any{"debug_id": f.id, "owner": f.owner, "cursor": f.cursor, "timeout_ms": 20000})
		f.cursor = uint64(out["next"].(float64))
		for _, value := range out["events"].([]any) {
			record := value.(map[string]any)["record"].(map[string]any)
			if record["kind"] == "lifecycle" {
				t.Logf("lifecycle: %v", record)
			}
			if record["class"] == class {
				return record
			}
		}
		status := out["status"].(map[string]any)
		if status["state"] == "failed" {
			f.mu.Lock()
			tail := string(f.tail)
			f.mu.Unlock()
			t.Logf("independent observer tail: %q", tail)
			t.Fatalf("debug failed: %v", status)
		}
	}
}
func (f *debugFixture) assertShell(t *testing.T) {
	t.Helper()
	current, e := unix.IoctlGetTermios(int(f.slave.Fd()), unix.TCGETS)
	if e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(f.original, current) {
		t.Fatalf("termios not restored: before=%+v after=%+v", f.original, current)
	}
	if _, active, e := f.app.LeaseStatus("debug-pty"); e != nil || active {
		t.Fatalf("lease still active=%t err=%v", active, e)
	}
	result, e := f.app.ExecuteTerminalCommand(context.Background(), "debug-pty", "printf 'AFTER_DEBUG_UNIQUE_241809'")
	if e != nil || result.ExitCode != 0 {
		t.Fatalf("shell unusable: %+v %v", result, e)
	}
}

func TestGDBLocalMCPAddition(t *testing.T) {
	f := newDebugFixture(t)
	f.start(t, 0)
	if _, e := f.app.WriteSession(context.Background(), "debug-pty", session.WriteRequest{Actor: session.ActorUser, Data: []byte("bad")}); e == nil {
		t.Fatal("ordinary write bypassed lease")
	}
	f.command(t, "break_insert", "add")
	// Discard the initial remote connection stop before continuing.
	f.wait(t, "stopped")
	f.command(t, "continue", "")
	f.wait(t, "stopped")
	for _, action := range []string{"arguments", "variables", "stack", "threads", "registers", "register_names"} {
		out := f.command(t, action, "")
		t.Logf("%s returned structured MI results", action)
		record := out["record"].(map[string]any)
		results := record["results"].(map[string]any)
		switch action {
		case "arguments":
			b, _ := json.Marshal(results)
			if !strings.Contains(string(b), `"name":"a","value":"3"`) || !strings.Contains(string(b), `"name":"b","value":"4"`) {
				t.Fatalf("arguments: %s", b)
			}
		case "variables":
			if len(results["variables"].([]any)) < 3 {
				t.Fatalf("variables: %v", results)
			}
		case "stack":
			if !strings.Contains(fmt.Sprint(results), "add") {
				t.Fatalf("stack: %v", results)
			}
		case "threads":
			if len(results["threads"].([]any)) != 2 {
				t.Fatalf("threads: %v", results)
			}
		case "registers":
			if len(results["register-values"].([]any)) < 16 {
				t.Fatalf("registers: %v", results)
			}
		case "register_names":
			if len(results["register-names"].([]any)) < 16 {
				t.Fatalf("register names: %v", results)
			}
		}
	}
	if out := f.command(t, "evaluate", "a + b"); !strings.Contains(fmt.Sprint(out), "7") {
		t.Fatalf("wrong addition: %v", out)
	}
	f.command(t, "step", "")
	if stop := f.wait(t, "stopped"); !strings.Contains(fmt.Sprint(stop), "plus") {
		t.Fatalf("step did not enter plus: %v", stop)
	}
	f.command(t, "next", "")
	f.wait(t, "stopped")
	f.command(t, "finish", "")
	f.wait(t, "stopped")
	if out := f.command(t, "evaluate", "sum"); !strings.Contains(fmt.Sprint(out), "7") {
		t.Fatalf("wrong sum: %v", out)
	}
	f.command(t, "next", "")
	f.wait(t, "stopped")
	f.command(t, "finish", "")
	f.wait(t, "stopped")
	f.command(t, "continue", "")
	f.wait(t, "closed")
	out := f.call(t, "debug_stop", map[string]any{"debug_id": f.id, "owner": f.owner})
	if out["status"].(map[string]any)["state"] != "closed" {
		t.Fatalf("stop: %v", out)
	}
	f.assertShell(t)
}

func TestGDBLocalMCPAttachDetach(t *testing.T) {
	f := newDebugFixture(t)
	inferior := exec.Command(f.elf, "wait")
	inferior.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if e := inferior.Start(); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { inferior.Process.Kill(); inferior.Wait() })
	time.Sleep(100 * time.Millisecond)
	f.start(t, inferior.Process.Pid)
	if _, e := f.app.StopDebug(context.Background(), f.id, f.owner, true); e == nil {
		t.Fatal("attached termination accepted")
	}
	f.command(t, "continue", "")
	f.call(t, "debug_stop", map[string]any{"debug_id": f.id, "owner": f.owner})
	if e := inferior.Process.Signal(syscall.Signal(0)); e != nil {
		t.Fatalf("attached process was killed: %v", e)
	}
	stat, e := os.ReadFile(fmt.Sprintf("/proc/%d/status", inferior.Process.Pid))
	if e != nil || !strings.Contains(string(stat), "TracerPid:\t0") {
		t.Fatalf("process still traced: %v %s", e, stat)
	}
	f.assertShell(t)
}

func TestGDBLocalRejectsUnsafeAttachAndMismatchedELF(t *testing.T) {
	f := newDebugFixture(t)
	ctx := context.Background()
	// A Bash process owns the very PTY used by this Session.
	bash, e := os.Readlink(fmt.Sprintf("/proc/%d/exe", f.shell.Process.Pid))
	if e != nil {
		t.Fatal(e)
	}
	_, e = f.app.StartDebug(ctx, app.DebugStartRequest{SessionID: "debug-pty", Owner: f.owner, RequestID: "unsafe-attach", ELF: bash, PID: f.shell.Process.Pid})
	if e == nil || !strings.Contains(e.Error(), "controlling terminal") {
		t.Fatalf("unsafe attach: %v", e)
	}
	data, e := os.ReadFile(f.elf)
	if e != nil {
		t.Fatal(e)
	}
	data[18] = 0
	data[19] = 0
	bad := filepath.Join(t.TempDir(), "wrong-arch")
	if e = os.WriteFile(bad, data, 0700); e != nil {
		t.Fatal(e)
	}
	_, e = f.app.StartDebug(ctx, app.DebugStartRequest{SessionID: "debug-pty", Owner: f.owner, RequestID: "wrong-elf", ELF: bad})
	if e == nil || !strings.Contains(e.Error(), "architecture mismatch") {
		t.Fatalf("ELF mismatch: %v", e)
	}
	f.assertShell(t)
}

func TestGDBLocalLostShellFailsClosed(t *testing.T) {
	f := newDebugFixture(t)
	f.start(t, 0)
	// Leave the PTY slave open but remove the shell, so a successful detach cannot
	// be mistaken for successful terminal recovery.
	if e := f.shell.Process.Kill(); e != nil {
		t.Fatal(e)
	}
	_, e := f.app.StopDebug(context.Background(), f.id, f.owner, false)
	if e == nil || !strings.Contains(e.Error(), "recovery failed") {
		t.Fatalf("missing shell falsely recovered: %v", e)
	}
	if _, e = f.app.WriteSession(context.Background(), "debug-pty", session.WriteRequest{Actor: session.ActorUser, Data: []byte("printf should-not-run\n")}); e == nil {
		t.Fatal("ordinary input reopened after failed recovery")
	}
	if _, e = f.app.StopDebug(context.Background(), f.id, f.owner, false); e == nil {
		t.Fatal("repeat failed stop returned success")
	}
}

func TestGDBLocalMCPAuthorizationAndIdempotency(t *testing.T) {
	f := newDebugFixture(t)
	f.start(t, 0)
	q := app.DebugCommandRequest{DebugID: f.id, Owner: f.owner, RequestID: "same-break", Action: "break_insert", Argument: "add"}
	first, e := f.app.DebugCommand(context.Background(), q)
	if e != nil {
		t.Fatal(e)
	}
	again, e := f.app.DebugCommand(context.Background(), q)
	if e != nil || !reflect.DeepEqual(first, again) {
		t.Fatalf("command retry: %v %v", again, e)
	}
	q.Argument = "main"
	if _, e = f.app.DebugCommand(context.Background(), q); e == nil {
		t.Fatal("conflicting command retry accepted")
	}
	q.Owner = "wrong"
	q.RequestID = "bad-owner"
	if _, e = f.app.DebugCommand(context.Background(), q); e == nil {
		t.Fatal("wrong command owner accepted")
	}
	if _, e = f.app.StopDebug(context.Background(), f.id, "wrong", false); e == nil {
		t.Fatal("wrong stop owner accepted")
	}
	if _, e = f.app.ReadDebugEvents(context.Background(), f.id, "wrong", 0); e == nil {
		t.Fatal("wrong event owner accepted")
	}
	f.command(t, "renew", "")
	f.call(t, "debug_stop", map[string]any{"debug_id": f.id, "owner": f.owner, "terminate": true})
	f.assertShell(t)
}

func TestGDBLocalRenewalAndDisconnectPreservesAttachedProcess(t *testing.T) {
	f := newDebugFixture(t)
	inferior := exec.Command(f.elf, "wait")
	inferior.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if e := inferior.Start(); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { inferior.Process.Kill(); inferior.Wait() })
	time.Sleep(100 * time.Millisecond)
	f.start(t, inferior.Process.Pid)
	before, active, e := f.app.LeaseStatus("debug-pty")
	if e != nil || !active {
		t.Fatalf("initial lease: %v", e)
	}
	// No incoming debug call is needed for the owned 30-second lease renewal.
	time.Sleep(11 * time.Second)
	after, active, e := f.app.LeaseStatus("debug-pty")
	if e != nil || !active || !after.ExpiresAt.After(before.ExpiresAt) {
		t.Fatalf("lease not renewed: %v %v", after, e)
	}
	f.command(t, "continue", "")
	if e = f.terminal.Close(); e != nil {
		t.Fatal(e)
	}
	f.wait(t, "failed")
	if _, e = f.app.StopDebug(context.Background(), f.id, f.owner, false); e == nil {
		t.Fatal("disconnect falsely recovered")
	}
	if e = inferior.Process.Signal(syscall.Signal(0)); e != nil {
		t.Fatalf("disconnect killed attached process: %v", e)
	}
	stat, e := os.ReadFile(fmt.Sprintf("/proc/%d/status", inferior.Process.Pid))
	if e != nil || !strings.Contains(string(stat), "TracerPid:\t0") {
		t.Fatalf("disconnect left tracer: %v %s", e, stat)
	}
}

func TestGDBLocalLiteralArgumentsAndELFPath(t *testing.T) {
	f := newDebugFixture(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "addition with spaces")
	data, e := os.ReadFile(f.elf)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(path, data, 0700); e != nil {
		t.Fatal(e)
	}
	marker := filepath.Join(dir, "must-not-exist")
	argument := "literal $(touch " + marker + ")"
	out := f.call(t, "debug_start", map[string]any{"session_id": "debug-pty", "owner": f.owner, "request_id": "literal-args", "elf": path, "args": []string{argument}})
	f.id = out["status"].(map[string]any)["debug_id"].(string)
	t.Cleanup(func() { _, _ = f.app.StopDebug(context.Background(), f.id, f.owner, true) })
	f.wait(t, "stopped")
	f.command(t, "break_insert", "main")
	f.command(t, "continue", "")
	f.wait(t, "stopped")
	out = f.command(t, "evaluate", "argv[1]")
	if !strings.Contains(fmt.Sprint(out), argument) {
		t.Fatalf("argument was reinterpreted: %v", out)
	}
	if _, e = os.Stat(marker); !os.IsNotExist(e) {
		t.Fatalf("shell expanded inferior argument: %v", e)
	}
	f.call(t, "debug_stop", map[string]any{"debug_id": f.id, "owner": f.owner, "terminate": true})
	f.assertShell(t)
}
