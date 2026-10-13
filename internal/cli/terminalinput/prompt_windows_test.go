//go:build windows

package terminalinput

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"reflect"
	"syscall"
	"testing"
	"time"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/term"
)

// TestPromptWindowsConsole creates an isolated hidden console in a child so
// native input/mode tests never consume or alter the user's active terminal.
func TestPromptWindowsConsole(t *testing.T) {
	if os.Getenv("CHANNELTERM_TEST_PROMPT_CONSOLE") == "1" {
		testPromptWindowsConsole(t)
		return
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, "-test.run=^TestPromptWindowsConsole$", "-test.v")
	cmd.Env = append(os.Environ(), "CHANNELTERM_TEST_PROMPT_CONSOLE=1")
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_CONSOLE, HideWindow: true}
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("console test: %v\n%s", err, output)
	}
}

func testPromptWindowsConsole(t *testing.T) {
	input, err := os.OpenFile("CONIN$", os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	original, err := term.GetState(int(input.Fd()))
	if err != nil {
		t.Fatal(err)
	}
	writeInput := windowsKernel32.NewProc("WriteConsoleInputW")
	for _, tc := range []struct {
		input, want string
		wantErr     error
	}{
		{"fake中\b😀\r", "fake😀", nil}, {"fake\x03", "", context.Canceled}, {"", "", context.DeadlineExceeded},
	} {
		for _, value := range utf16.Encode([]rune(tc.input)) {
			record := windowsInputRecord{eventType: windowsKeyEvent, key: windowsKeyEventRecord{keyDown: 1, repeatCount: 1, unicodeChar: value}}
			var count uint32
			ok, _, err := writeInput.Call(input.Fd(), uintptr(unsafe.Pointer(&record)), 1, uintptr(unsafe.Pointer(&count)))
			if ok == 0 || count != 1 {
				t.Fatal(err)
			}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		var output bytes.Buffer
		value, err := ReadLine(ctx, input, &output, "Password: ", true)
		cancel()
		if string(value) != tc.want || !errors.Is(err, tc.wantErr) {
			t.Fatalf("value=%q err=%v", value, err)
		}
		if output.String() != "Password: \r\n" {
			t.Fatalf("secret echoed: %q", &output)
		}
		current, err := term.GetState(int(input.Fd()))
		if err != nil || !reflect.DeepEqual(current, original) {
			t.Fatal("console mode not restored")
		}
	}
}
