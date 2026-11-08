package command

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestNormalizeShortcutLocalPath(t *testing.T) {
	for _, tt := range []struct {
		input       string
		wantWindows string
	}{
		{`C:\new folder (2)\test_configSet`, `C:\new folder (2)\test_configSet`},
		{`"C:\new folder (2)\test_configSet"`, `C:\new folder (2)\test_configSet`},
		{`"C:\new folder\"`, `C:\new folder\`},
		{`"\\server\share\new folder\test.bin"`, `\\server\share\new folder\test.bin`},
		{`"C:\new folder\test.bin`, `"C:\new folder\test.bin`},
		{`C:\new folder\test.bin"`, `C:\new folder\test.bin"`},
		{`'C:\new folder\test.bin'`, `'C:\new folder\test.bin'`},
		{`"C:\%TEMP%\$name\test.bin"`, `C:\%TEMP%\$name\test.bin`},
		{`"a"b"`, `a"b`},
		{`""nested""`, `"nested"`},
		{`" leading and trailing spaces "`, ` leading and trailing spaces `},
		{` leading and trailing spaces `, ` leading and trailing spaces `},
		{`""`, ``},
		{`"`, `"`},
		{``, ``},
	} {
		t.Run(tt.input, func(t *testing.T) {
			want := tt.input
			if runtime.GOOS == "windows" {
				want = tt.wantWindows
			}
			if got := normalizeShortcutLocalPath(tt.input); got != want {
				t.Fatalf("normalized path = %q, want %q", got, want)
			}
		})
	}
}

func TestAttachFileShortcutQuotedLocalPath(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows terminal drag-and-drop paths")
	}
	localPath := filepath.Join(t.TempDir(), "new folder (2)", "test_configSet")
	if err := os.MkdirAll(filepath.Dir(localPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(localPath, []byte("config"), 0o600); err != nil {
		t.Fatal(err)
	}
	remotePath := "/tmp/cterm/user-files/test_configSet"
	for _, direction := range []string{"send", "receive"} {
		t.Run(direction, func(t *testing.T) {
			var output bytes.Buffer
			attached := &fakeAttachSession{}
			runner := func(_ context.Context, _ attachSession, _ io.Writer, _ func() bool, gotDirection, first, second string) error {
				local, remote := first, second
				if gotDirection == "receive" {
					local, remote = second, first
				}
				if gotDirection != direction || local != localPath || remote != remotePath {
					return fmt.Errorf("transfer arguments = %q, %q, %q", gotDirection, local, remote)
				}
				_, err := os.Lstat(local)
				return err
			}
			dispatcher := newAttachInputDispatcherWithPump(context.Background(), &attachInputPump{}, attached, func(data []byte) error {
				_, err := output.Write(data)
				return err
			}, func() error { return nil }, func() {}, runner)
			input := "\x1dfs\"" + localPath + "\"\r\n\r\n"
			if direction == "receive" {
				input = "\x1dfr" + remotePath + "\r\n\"" + localPath + "\"\r\n"
			}
			if !dispatcher.dispatch([]byte(input)) || dispatcher.transferDone == nil {
				t.Fatal("quoted path did not start a transfer")
			}
			select {
			case err := <-dispatcher.transferDone:
				dispatcher.finishTransfer(err)
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("transfer worker did not finish")
			}
			if direction == "send" && !strings.Contains(output.String(), "Remote path ["+remotePath+"]:") {
				t.Fatalf("default remote path contains pasted quotes: %q", output.String())
			}
			if len(attached.writtenData()) != 0 {
				t.Fatal("path input leaked to the Session")
			}
		})
	}

	// The synchronous shortcut must also derive its default after unwrapping.
	pump := newAttachInputPump(strings.NewReader("s\"" + localPath + "\"\n\x1b"))
	var output bytes.Buffer
	if !runAttachFileShortcut(context.Background(), &pump, &fakeAttachSession{}, func(data []byte) error {
		_, err := output.Write(data)
		return err
	}) || !strings.Contains(output.String(), "Remote path ["+remotePath+"]:") {
		t.Fatalf("shortcut output = %q, want unquoted default and cancellation", output.String())
	}
}

func TestAttachFileShortcutQuotedMissingSourceRestoresInput(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows terminal drag-and-drop paths")
	}
	localPath := filepath.Join(t.TempDir(), "missing file.bin")
	attached := &fakeAttachSession{}
	var output bytes.Buffer
	dispatcher := newAttachInputDispatcherWithPump(context.Background(), &attachInputPump{}, attached, func(data []byte) error {
		_, err := output.Write(data)
		return err
	}, func() error { return nil }, func() {}, runAttachShortcutFileTransfer)
	if !dispatcher.dispatch([]byte("\x1dfs\""+localPath+"\"\n\n")) || dispatcher.transferDone == nil {
		t.Fatal("quoted path did not start a transfer")
	}
	select {
	case err := <-dispatcher.transferDone:
		var pathErr *os.PathError
		if !errors.Is(err, os.ErrNotExist) || !errors.As(err, &pathErr) || pathErr.Path != localPath {
			t.Fatalf("error = %v, want missing unquoted source", err)
		}
		dispatcher.finishTransfer(err)
	case <-time.After(5 * time.Second):
		t.Fatal("missing source did not fail promptly")
	}
	if len(attached.writtenData()) != 0 || !strings.Contains(output.String(), "File transfer failed:") {
		t.Fatal("missing source must fail locally before writing to the Session")
	}
	if !dispatcher.dispatch([]byte("echo ok\r")) || string(attached.writtenData()) != "echo ok\r" {
		t.Fatal("normal terminal input did not resume after failure")
	}
}
