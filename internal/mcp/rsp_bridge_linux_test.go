package mcp

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/core/session"
)

type externalBridgeFixture struct {
	*debugFixture
	ctx                                         context.Context
	dir, helper, ttyState, bridgeID, descriptor string
}

func newExternalBridgeFixture(t *testing.T, timeout time.Duration) *externalBridgeFixture {
	t.Helper()
	f := newDebugFixture(t) // Opt-in: CHANNELTERM_TEST_GDB=1, with GDB tools on PATH.
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	t.Cleanup(cancel)
	dir := t.TempDir()
	helper := filepath.Join(dir, "channelterm")
	build := exec.CommandContext(ctx, "go", "build", "-o", helper, "./cmd/channelterm")
	build.Dir = "../.."
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("helper build: %v %s", err, out)
	}
	result, err := f.app.ExecuteTerminalCommand(ctx, f.terminal.ID(), "stty -g")
	if err != nil || result.ExitCode != 0 {
		t.Fatalf("save TTY: %v", err)
	}
	chunk, err := f.terminal.ReadOutput(ctx, result.OutputStart, int(result.OutputEnd-result.OutputStart))
	if err != nil {
		t.Fatal(err)
	}
	ttyState := strings.TrimSpace(string(chunk.Data))
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }
	cursorChunk, err := f.terminal.ReadRecent(1)
	if err != nil {
		t.Fatal(err)
	}
	cursor := cursorChunk.Next
	// The test operator prepares gdbserver; the bridge itself sends no command.
	command := "stty raw -echo; printf '\\036EXTERNAL_READY\\037'; gdbserver --once stdio " + quote(f.elf) + " 2>" + quote(filepath.Join(dir, "gdbserver.log")) + "; stty " + quote(ttyState) + "\n"
	if _, err := f.app.WriteSession(ctx, f.terminal.ID(), session.WriteRequest{Actor: session.ActorAgent, Data: []byte(command)}); err != nil {
		t.Fatal(err)
	}
	var preparation string
	for !strings.Contains(preparation, "\x1eEXTERNAL_READY\x1f") {
		chunk, err := f.terminal.ReadOutput(ctx, cursor, 4096)
		if err != nil {
			t.Fatal(err)
		}
		cursor = chunk.Next
		preparation += string(chunk.Data)
	}
	start := f.call(t, "debug_bridge_start", map[string]any{"session_id": f.terminal.ID(), "owner": f.owner, "request_id": "external-1", "acknowledge_unverified_target": true})
	id := start["status"].(map[string]any)["bridge_id"].(string)
	t.Cleanup(func() { _, _ = f.app.CloseRSPBridge(id, f.owner) })
	descriptor := filepath.Join(dir, "connection.json")
	data, _ := json.Marshal(start["connection"])
	if err := os.WriteFile(descriptor, data, 0600); err != nil {
		t.Fatal(err)
	}
	return &externalBridgeFixture{debugFixture: f, ctx: ctx, dir: dir, helper: helper, ttyState: ttyState, bridgeID: id, descriptor: descriptor}
}

// TestGDBExternalPTY uses real independently launched GDB/gdbserver and a Linux
// PTY. It validates neither a Windows Host nor an ARM board.
func TestGDBExternalPTY(t *testing.T) {
	x := newExternalBridgeFixture(t, 90*time.Second)
	f, ctx, dir, helper, ttyState, id, descriptor := x.debugFixture, x.ctx, x.dir, x.helper, x.ttyState, x.bridgeID, x.descriptor
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }

	args := []string{"--nx", "--nh", "--batch", "-iex", "set auto-load off", "-iex", "set debuginfod enabled off", f.elf}
	commands := []string{
		"set pagination off", "set confirm off", "set sysroot /",
		"target remote | " + quote(helper) + " rsp-client --connection " + quote(descriptor),
		"break add", "continue", "info registers", `printf "PC_VALUE=%lx\n", $pc`, "step", "next", "finish", "next", `printf "SUM_VALUE=%d\n", sum`, "bt", "detach",
	}
	for _, command := range commands {
		args = append(args, "-ex", command)
	}
	// This process is owned by the external test driver, never Application.
	out, err := exec.CommandContext(ctx, "gdb", args...).CombinedOutput()
	if err != nil || !strings.Contains(string(out), "SUM_VALUE=7") || !strings.Contains(string(out), "#0") || !strings.Contains(string(out), "add (") || !strings.Contains(string(out), "PC_VALUE=") {
		log, _ := os.ReadFile(filepath.Join(dir, "gdbserver.log"))
		t.Fatalf("external GDB: %v\n%s\nserver: %s", err, out, log)
	}
	t.Log("independent GDB confirmed breakpoint, step/next/finish, sum=7, registers, backtrace, and detach over TLS/PTY")
	// The external driver reports only after executing GDB. Reporting never
	// substitutes for execution and deliberately omits the connection command.
	f.call(t, "debug_bridge_report", map[string]any{"bridge_id": id, "owner": f.owner, "sequence": 1, "kind": "command", "text": strings.Join(commands[4:], "\n")})
	f.call(t, "debug_bridge_report", map[string]any{"bridge_id": id, "owner": f.owner, "sequence": 2, "kind": "result", "text": string(out)})
	reports := f.call(t, "terminal_session_events", map[string]any{"session_id": f.terminal.ID(), "cursor": 0, "max_events": 100})
	encodedReports, _ := json.Marshal(reports)
	if strings.Count(string(encodedReports), "DEBUG_CLIENT_REPORT") != 2 || !strings.Contains(string(encodedReports), "SUM_VALUE=7") {
		t.Fatalf("actual GDB observations missing: %s", encodedReports)
	}
	for {
		status, err := f.app.RSPBridgeStatus(id, f.owner)
		if err != nil {
			t.Fatal(err)
		}
		if status.State == "disconnected" {
			break
		}
		if ctx.Err() != nil {
			t.Fatalf("disconnect: %+v", status)
		}
		time.Sleep(time.Millisecond)
	}
	restored := f.call(t, "debug_bridge_restore", map[string]any{"bridge_id": id, "owner": f.owner, "confirm_shell_ready": true, "tty_state": ttyState})
	if restored["status"].(map[string]any)["state"] != "restored" {
		t.Fatal(restored)
	}
	f.assertShell(t)
}
