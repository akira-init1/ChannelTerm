package mcp

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/core/app"
	"github.com/akira-init1/ChannelTerm/internal/core/session"
	"golang.org/x/sys/unix"
)

type externalGDBOutput struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

// Write captures the disposable test process's output without racing polling.
func (b *externalGDBOutput) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.Write(p)
}

func (b *externalGDBOutput) snapshot() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.String()
}

type externalGDBProcess struct {
	cmd      *exec.Cmd
	input    io.WriteCloser
	output   *externalGDBOutput
	done     chan struct{}
	err      error // published by closing done
	helperFD int
}

func startExternalGDB(t *testing.T, x *externalBridgeFixture) *externalGDBProcess {
	t.Helper()
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }
	pidFile := filepath.Join(x.dir, "helper.pid")
	wrapper := filepath.Join(x.dir, "pipe-helper")
	// The wrapper records only its own PID, then execs the real helper. No
	// credentials enter argv; the existing private descriptor remains unchanged.
	script := "#!/bin/sh\nprintf '%s\\n' \"$$\" > " + quote(pidFile) + "\nexec " + quote(x.helper) + " rsp-client --connection " + quote(x.descriptor) + "\n"
	if err := os.WriteFile(wrapper, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(x.ctx, "gdb", "--nx", "--nh", "--quiet", "-iex", "set auto-load off", "-iex", "set debuginfod enabled off", x.elf)
	input, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	p := &externalGDBProcess{cmd: cmd, input: input, output: &externalGDBOutput{}, done: make(chan struct{}), helperFD: -1}
	cmd.Stdout, cmd.Stderr = p.output, p.output
	cmd.WaitDelay = time.Second
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() { p.err = cmd.Wait(); close(p.done) }()
	t.Cleanup(func() {
		input.Close()
		cmd.Process.Kill()
		if p.helperFD >= 0 {
			unix.PidfdSendSignal(p.helperFD, unix.SIGKILL, nil, 0)
			unix.Close(p.helperFD)
		}
		select {
		case <-p.done:
		case <-time.After(3 * time.Second):
			t.Error("GDB process did not exit")
		}
	})
	commands := "set pagination off\nset confirm off\nset sysroot /\ntarget remote | " + quote(wrapper) + "\nbreak add\ncontinue\nprintf \"IDLE_READY\\n\"\n"
	if _, err := io.WriteString(input, commands); err != nil {
		t.Fatal(err)
	}
	p.waitText(t, x.ctx, "IDLE_READY")
	if !strings.Contains(p.output.snapshot(), "add (") {
		t.Fatalf("no breakpoint: %s", p.output.snapshot())
	}
	pidBytes, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(pidBytes)))
	if err != nil {
		t.Fatal(err)
	}
	p.helperFD, err = unix.PidfdOpen(pid, 0)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func (p *externalGDBProcess) waitText(t *testing.T, ctx context.Context, text string) {
	t.Helper()
	deadline := time.NewTimer(20 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for !strings.Contains(p.output.snapshot(), text) {
		select {
		case <-ctx.Done():
			t.Fatalf("GDB wait: %v", ctx.Err())
		case <-deadline.C:
			t.Fatalf("GDB missing %s: %s", text, p.output.snapshot())
		case <-p.done:
			t.Fatalf("GDB exited early: %v %s", p.err, p.output.snapshot())
		case <-ticker.C:
		}
	}
}

func waitExternalBridge(t *testing.T, x *externalBridgeFixture, want string) app.BridgeStatus {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		s, err := x.app.RSPBridgeStatus(x.bridgeID, x.owner)
		if err != nil {
			t.Fatal(err)
		}
		if s.State == want {
			return s
		}
		if time.Now().After(deadline) {
			t.Fatalf("want %s: %+v", want, s)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestGDBExternalPTYLongIdle holds a real GDB breakpoint for 310 seconds without
// RSP traffic or management renewal, then completes debugging and shell recovery.
func TestGDBExternalPTYLongIdle(t *testing.T) {
	if os.Getenv("CHANNELTERM_TEST_RSP_LONG") != "1" {
		t.Skip("set CHANNELTERM_TEST_RSP_LONG=1 for real-time long debugging")
	}
	x := newExternalBridgeFixture(t, 7*time.Minute)
	p := startExternalGDB(t, x)
	before := waitExternalBridge(t, x, "connected").ExpiresAt
	chunk, err := x.terminal.ReadRecent(1)
	if err != nil {
		t.Fatal(err)
	}
	cursor := chunk.Next
	deadline := time.NewTimer(310 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
idle:
	for {
		select {
		case <-deadline.C:
			break idle
		case <-p.done:
			t.Fatalf("GDB ended during idle: %v", p.err)
		case <-x.ctx.Done():
			t.Fatal(x.ctx.Err())
		case <-ticker.C:
			s := waitExternalBridge(t, x, "connected")
			if !s.AutoKeepalive || !s.ExpiresAt.After(before) {
				t.Fatalf("idle bridge not renewed: %+v", s)
			}
			lease, active, err := x.app.LeaseStatus(x.terminal.ID())
			if err != nil || !active || !time.Now().Before(lease.ExpiresAt) {
				t.Fatalf("internal lease expired: %v", err)
			}
			chunk, err := x.terminal.ReadRecent(1)
			if err != nil || chunk.Next != cursor {
				t.Fatalf("unexpected RSP during idle: %v", err)
			}
		}
	}
	commands := "step\nnext\nfinish\nnext\nprintf \"SUM_VALUE=%d\\n\", sum\ninfo registers\nprintf \"PC_VALUE=%lx\\n\", $pc\nbt\ndetach\nquit\n"
	if _, err := io.WriteString(p.input, commands); err != nil {
		t.Fatal(err)
	}
	select {
	case <-p.done:
		if p.err != nil {
			t.Fatalf("GDB: %v %s", p.err, p.output.snapshot())
		}
	case <-time.After(20 * time.Second):
		t.Fatal("GDB did not detach/exit")
	}
	for _, marker := range []string{"SUM_VALUE=7", "PC_VALUE=", "#0"} {
		if !strings.Contains(p.output.snapshot(), marker) {
			t.Fatalf("missing %s: %s", marker, p.output.snapshot())
		}
	}
	waitExternalBridge(t, x, "disconnected")
	x.call(t, "debug_bridge_restore", map[string]any{"bridge_id": x.bridgeID, "owner": x.owner, "confirm_shell_ready": true, "tty_state": x.ttyState})
	x.assertShell(t)
	t.Log("310-second real GDB breakpoint idle; no RSP/renew calls; step/next/finish, variable/register/backtrace, detach and shell/TTY restoration passed")
}

// TestGDBExternalPTYAbnormalExit uses actual process death and MCP closure. It
// never assumes that losing the helper has stopped gdbserver or restored a shell.
func TestGDBExternalPTYAbnormalExit(t *testing.T) {
	for _, mode := range []string{"gdb-killed", "helper-killed", "management-close"} {
		t.Run(mode, func(t *testing.T) {
			x := newExternalBridgeFixture(t, 90*time.Second)
			p := startExternalGDB(t, x)
			s := waitExternalBridge(t, x, "connected")
			switch mode {
			case "gdb-killed":
				if err := p.cmd.Process.Kill(); err != nil {
					t.Fatal(err)
				}
			case "helper-killed":
				if err := unix.PidfdSendSignal(p.helperFD, unix.SIGKILL, nil, 0); err != nil {
					t.Fatal(err)
				}
			case "management-close":
				x.call(t, "debug_bridge_close", map[string]any{"bridge_id": x.bridgeID, "owner": x.owner})
			}
			if mode != "management-close" {
				s = waitExternalBridge(t, x, "disconnected")
				if s.AutoKeepalive || s.TargetState == "shell_verified" {
					t.Fatalf("crash misreported: %+v", s)
				}
				if _, err := x.app.WriteSession(x.ctx, x.terminal.ID(), session.WriteRequest{Actor: session.ActorUser, Data: []byte("unsafe\n")}); !errors.Is(err, app.ErrSessionBusy) {
					t.Fatalf("crash reopened writes: %v", err)
				}
				if _, err := x.app.RestoreRSPBridge(x.bridgeID, x.owner, x.ttyState, false); err == nil {
					t.Fatal("unconfirmed shell restored")
				}
				time.Sleep(1100 * time.Millisecond) // allow a monitor pass to prove renewal stopped
				later, _ := x.app.RSPBridgeStatus(x.bridgeID, x.owner)
				if !later.ExpiresAt.Equal(s.ExpiresAt) {
					t.Fatal("auto renewal survived client death")
				}
				x.call(t, "debug_bridge_close", map[string]any{"bridge_id": x.bridgeID, "owner": x.owner})
			}
			waitExternalBridge(t, x, "closed")
			if x.terminal.State() != session.StateClosed {
				t.Fatal("MCP close left serial open")
			}
			if _, err := x.app.WriteSession(x.ctx, x.terminal.ID(), session.WriteRequest{Actor: session.ActorUser, Data: []byte("unsafe\n")}); err == nil {
				t.Fatal("closed Session accepted writes")
			}
			t.Logf("%s: connection stopped; no automatic shell recovery; MCP close terminated Session", mode)
		})
	}
}
