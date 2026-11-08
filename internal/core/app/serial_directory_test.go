package app

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/akira-init1/ChannelTerm/internal/core/session"
)

type directoryTestSession struct {
	*fileTransferTestSession
	records []string
}

func (s *directoryTestSession) Write(request session.WriteRequest) (int, error) {
	command := string(request.Data)
	if strings.Contains(command, ":LIST:CWD:") {
		token := fileTestTokenPattern.FindStringSubmatch(command)[1]
		s.mu.Lock()
		defer s.mu.Unlock()
		for _, record := range s.records {
			s.emitLocked(fmt.Sprintf("\n@CTERM:%s:%s\n", token, record))
		}
		return len(request.Data), nil
	}
	return s.fileTransferTestSession.Write(request)
}

func TestSerialDirectoryDecodesBoundedRecordsAndClosesChild(t *testing.T) {
	for _, invalid := range []bool{false, true} {
		t.Run(fmt.Sprint(invalid), func(t *testing.T) {
			name := "quote' and space.txt"
			records := []string{"LIST:CWD:2f746d70", "LIST:file:" + hex.EncodeToString([]byte(name)), "LIST:directory:2e68696464656e", "LIST:DONE:1"}
			if invalid {
				records[1] = "LIST:file:2e2e2f657363617065"
			}
			terminal := &directoryTestSession{newFileTransferTestSession(nil), records}
			result, err := ListSerialDirectory(context.Background(), terminal, ".")
			if invalid {
				if err == nil {
					t.Fatal("accepted a traversal entry")
				}
			} else if err != nil || result.Path != "/tmp" || len(result.Entries) != 2 || result.Entries[0].Name != name || !result.Truncated {
				t.Fatalf("result=%+v error=%v", result, err)
			}
			if terminal.shellActive {
				t.Fatal("child shell not closed")
			}
			if len(terminal.interactiveCommands) != 1 {
				t.Fatal("did not reuse one shell")
			}
		})
	}
}

func TestSerialDirectoryRejectsInvalidPathsBeforeWriting(t *testing.T) {
	for _, path := range []string{"", "a\nb", "x\x00y", strings.Repeat("x", 4097)} {
		terminal := newFileTransferTestSession(nil)
		if _, err := ListSerialDirectory(context.Background(), terminal, path); err == nil {
			t.Fatalf("accepted %q", path)
		}
		if len(terminal.interactiveCommands) != 0 {
			t.Fatal("wrote invalid path")
		}
	}
}

func TestSerialDirectoryShellQuotingAndParentDirectory(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("POSIX shell unavailable")
	}
	dir := filepath.Join(t.TempDir(), strings.Repeat("b", 80))
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	names := []string{"a' file.txt", strings.Repeat("a", 80) + ".txt"}
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("test"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	quoted, _ := quoteRemotePath(filepath.ToSlash(dir))
	command := exec.Command(sh, "-c", "before=$(pwd); ("+serialDirectoryCommand("test", quoted)+"); [ \"$before\" = \"$(pwd)\" ]")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("shell query failed: %v %s", err, output)
	}
	for _, name := range names {
		if !strings.Contains(string(output), "LIST:file:"+hex.EncodeToString([]byte(name))) {
			t.Errorf("missing filename %q in listing: %s", name, output)
		}
	}
	// Ask the selected shell for its physical path: Git Bash and native Windows
	// can name the same directory differently, as can macOS temporary symlinks.
	physical, err := exec.Command(sh, "-c", "cd -- "+quoted+" && pwd -P").Output()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(output), "LIST:CWD:"+hex.EncodeToString(bytes.TrimSpace(physical))) || !strings.Contains(string(output), "LIST:DONE:0") {
		t.Fatalf("bad listing: %s", output)
	}
}
