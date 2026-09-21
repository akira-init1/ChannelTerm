package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/core/session"
)

func TestExecuteTerminalCommandUsesIsolatedHistoryFreeShell(t *testing.T) {
	manager := session.NewManager()
	terminal := newFakeConnectedSession("session-1")
	if err := terminal.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := manager.RegisterWithMetadata(terminal, session.SessionMetadata{Transport: "serial", Endpoint: "COM1"}); err != nil {
		t.Fatal(err)
	}
	application, err := New(Dependencies{Manager: manager})
	if err != nil {
		t.Fatal(err)
	}

	terminal.onWrite = func(request session.WriteRequest) {
		if request.Actor != session.ActorSystem || len(request.Data) == 1 {
			return
		}
		terminal.mu.Lock()
		started := terminal.events[len(terminal.events)-1]
		terminal.mu.Unlock()
		token := strings.TrimPrefix(started.Metadata["command_id"].(string), "CMD-")
		startMarker := "\x1e\x1f" + token + "\x1f\x1e"
		endMarker := "\x1d\x1c" + token + ":7\x1c\x1d"
		terminal.emitOutput(append(append(append([]byte{}, request.Data...), []byte(startMarker)...), []byte("command output\r\n"+endMarker+"root# ")...))
	}

	result, err := application.ExecuteTerminalCommand(context.Background(), "SER-1", "printf command-output")
	if err != nil {
		t.Fatalf("ExecuteTerminalCommand() error = %v", err)
	}
	if result.SessionID != "session-1" || result.ExitCode != 7 || result.OutputStart == 0 || result.OutputEnd <= result.OutputStart {
		t.Fatalf("ExecuteTerminalCommand() = %#v, want completed command cursor range and exit 7", result)
	}
	written := terminal.writtenData()
	if !strings.Contains(string(written), bashDeleteCurrentHistoryEntry) || !strings.Contains(string(written), `"$BASH" --noprofile --norc -c 'printf command-output'`) {
		t.Fatalf("terminal bootstrap = %q, want history deletion and isolated child command", written)
	}
	terminal.mu.Lock()
	activity := append([]session.SessionEvent(nil), terminal.activity...)
	events := append([]session.Event(nil), terminal.events...)
	terminal.mu.Unlock()
	if len(activity) != 1 || activity[0].Actor != session.ActorSystem {
		t.Fatalf("write activity = %#v, want one internal system write", activity)
	}
	wantTypes := []session.EventType{
		session.EventSessionCreated,
		session.EventLeaseAcquired,
		session.EventTerminalCommandStarted,
		session.EventTerminalCommandOutputStarted,
		session.EventTerminalCommandCompleted,
		session.EventLeaseReleased,
	}
	if len(events) != len(wantTypes) {
		t.Fatalf("events = %#v, want %v", events, wantTypes)
	}
	for index, want := range wantTypes {
		if events[index].Type != want {
			t.Errorf("event[%d] = %s, want %s", index, events[index].Type, want)
		}
	}
	if got := events[2].Metadata["command"]; got != "printf command-output" {
		t.Errorf("command presentation = %#v, want original command", got)
	}
	if _, active, err := application.LeaseStatus("SER-1"); err != nil || active {
		t.Errorf("LeaseStatus() = active:%t err:%v, want released", active, err)
	}
}

func TestExecuteTerminalCommandRejectsPendingInteractiveInputWithoutBootstrap(t *testing.T) {
	manager := session.NewManager()
	terminal := newFakeConnectedSession("session-1")
	if err := terminal.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := manager.Register(terminal); err != nil {
		t.Fatal(err)
	}
	application, err := New(Dependencies{Manager: manager})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.WriteSession(context.Background(), "session-1", session.WriteRequest{Actor: session.ActorUser, Data: []byte("ls")}); err != nil {
		t.Fatal(err)
	}

	_, err = application.ExecuteTerminalCommand(context.Background(), "session-1", "pwd")
	if !errors.Is(err, ErrTerminalCommandInputPending) {
		t.Fatalf("ExecuteTerminalCommand() error = %v, want ErrTerminalCommandInputPending", err)
	}
	if got := string(terminal.writtenData()); got != "ls" {
		t.Fatalf("terminal writes = %q, want no command bootstrap after pending input", got)
	}
	if _, active, err := application.LeaseStatus("session-1"); err != nil || active {
		t.Errorf("LeaseStatus() = active:%t err:%v, want released", active, err)
	}
}

func TestExecuteTerminalCommandRejectsNonBashParentAndReleasesLease(t *testing.T) {
	manager := session.NewManager()
	terminal := newFakeConnectedSession("session-1")
	if err := terminal.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := manager.Register(terminal); err != nil {
		t.Fatal(err)
	}
	application, err := New(Dependencies{Manager: manager})
	if err != nil {
		t.Fatal(err)
	}
	terminal.onWrite = func(request session.WriteRequest) {
		terminal.mu.Lock()
		started := terminal.events[len(terminal.events)-1]
		terminal.mu.Unlock()
		token := strings.TrimPrefix(started.Metadata["command_id"].(string), "CMD-")
		terminal.emitOutput([]byte("\x1d\x1c" + token + ":UNSUPPORTED\x1c\x1d"))
	}

	_, err = application.ExecuteTerminalCommand(context.Background(), "session-1", "pwd")
	if !errors.Is(err, ErrTerminalCommandRequiresBash) {
		t.Fatalf("ExecuteTerminalCommand() error = %v, want ErrTerminalCommandRequiresBash", err)
	}
	if terminal.closed {
		t.Fatal("unsupported shell closed the Session")
	}
	if _, active, err := application.LeaseStatus("session-1"); err != nil || active {
		t.Errorf("LeaseStatus() = active:%t err:%v, want released", active, err)
	}
}

func TestExecuteTerminalCommandCancellationRestoresProtocolAndReleasesLease(t *testing.T) {
	manager := session.NewManager()
	terminal := newFakeConnectedSession("session-1")
	if err := terminal.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := manager.Register(terminal); err != nil {
		t.Fatal(err)
	}
	application, err := New(Dependencies{Manager: manager})
	if err != nil {
		t.Fatal(err)
	}
	var token string
	terminal.onWrite = func(request session.WriteRequest) {
		if len(request.Data) == 1 && request.Data[0] == 0x03 {
			terminal.emitOutput([]byte("\x1d\x1c" + token + ":130\x1c\x1droot# "))
			return
		}
		terminal.mu.Lock()
		started := terminal.events[len(terminal.events)-1]
		terminal.mu.Unlock()
		token = strings.TrimPrefix(started.Metadata["command_id"].(string), "CMD-")
		terminal.emitOutput([]byte("\x1e\x1f" + token + "\x1f\x1e"))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err = application.ExecuteTerminalCommand(ctx, "session-1", "sleep 60")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("ExecuteTerminalCommand() error = %v, want deadline exceeded", err)
	}
	if terminal.closed {
		t.Fatal("bounded Ctrl+C recovery closed a usable Session")
	}
	if !strings.HasSuffix(string(terminal.writtenData()), string([]byte{0x03})) {
		t.Fatalf("terminal writes = %q, want recovery Ctrl+C", terminal.writtenData())
	}
	if _, active, err := application.LeaseStatus("session-1"); err != nil || active {
		t.Errorf("LeaseStatus() = active:%t err:%v, want released", active, err)
	}
}

func TestTerminalCommandBootstrapDeletesItsBashHistoryEntry(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Bash history check is unavailable on Windows")
	}
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("Bash is unavailable")
	}
	bootstrap := terminalCommandBootstrap("printf AI_COMMAND_OUTPUT", "00112233445566778899aabb")
	command := exec.Command(bash, "--noprofile", "--norc", "-i")
	command.Env = append(os.Environ(), "HISTFILE=/dev/null")
	command.Stdin = strings.NewReader("echo USER_COMMAND_BEFORE_AGENT\n" + bootstrap + "\nhistory\nexit\n")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("interactive Bash history check error = %v, output = %q", err, output)
	}
	if regexp.MustCompile(`(?m)^\s*[0-9]+\s+.*ct_finish`).Match(output) {
		t.Fatalf("interactive Bash history retained terminal command bootstrap: %q", output)
	}
	if !strings.Contains(string(output), "echo USER_COMMAND_BEFORE_AGENT") {
		t.Fatalf("interactive Bash history lost prior user command: %q", output)
	}
}

func TestTerminalCommandBootstrapRestoresPTYEcho(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("util-linux script PTY check runs only on Linux")
	}
	bash, bashErr := exec.LookPath("bash")
	script, scriptErr := exec.LookPath("script")
	if bashErr != nil || scriptErr != nil {
		t.Skip("Bash or util-linux script is unavailable")
	}
	bootstrap := terminalCommandBootstrap("printf AI_PTY_OUTPUT", "00112233445566778899aabb")
	commandLine := fmt.Sprintf("HISTFILE=/dev/null %s --noprofile --norc -i", quoteTerminalCommand(bash))
	command := exec.Command(script, "-qfec", commandLine, "/dev/null")
	command.Stdin = strings.NewReader("echo USER_BEFORE_PTY_AGENT\n" + bootstrap + "\nprintf '__CTERM_STTY__ '; stty -a\nhistory\nexit\n")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("PTY Bash command error = %v, output = %q", err, output)
	}
	if !strings.Contains(string(output), "AI_PTY_OUTPUT") {
		t.Fatalf("PTY Bash output = %q, want child command output", output)
	}
	if !regexp.MustCompile(`__CTERM_STTY__[\s\S]*[ ;]echo[ ;]`).Match(output) {
		t.Fatalf("PTY Bash did not restore terminal echo: %q", output)
	}
	if regexp.MustCompile(`(?m)^\s*[0-9]+\s+.*ct_finish`).Match(output) {
		t.Fatalf("PTY Bash history retained terminal command bootstrap: %q", output)
	}
}

func TestValidateTerminalCommandRejectsUnsafeOrOversizedInput(t *testing.T) {
	tests := []struct {
		name    string
		command string
		want    error
	}{
		{name: "empty", command: "  ", want: ErrTerminalCommandRequired},
		{name: "multiline", command: "pwd\nwhoami", want: ErrTerminalCommandControlCharacter},
		{name: "escape", command: "printf \x1b", want: ErrTerminalCommandControlCharacter},
		{name: "too large", command: strings.Repeat("x", MaxTerminalCommandBytes+1), want: ErrTerminalCommandTooLarge},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := validateTerminalCommand(test.command); !errors.Is(err, test.want) {
				t.Fatalf("validateTerminalCommand() error = %v, want %v", err, test.want)
			}
		})
	}
}
