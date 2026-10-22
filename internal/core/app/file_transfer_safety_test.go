package app

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestFileTransferSendCommandsHandleMinimalDD executes the generated shell
// protocol against a dd shim that rejects fullblock, with fragmented binary
// stdin and a fake stty. No physical terminal or device is opened.
func TestFileTransferSendCommandsHandleMinimalDD(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires a POSIX shell and dd")
	}
	for _, minimal := range []bool{false, true} {
		for _, directory := range []bool{false, true} {
			for _, short := range []bool{false, true} {
				name := "fullblock"
				if minimal {
					name = "minimal"
				}
				if directory {
					name += "/directory"
				} else {
					name += "/file"
				}
				if short {
					name += "/short-input"
				}
				t.Run(name, func(t *testing.T) {
					path := filepath.Join(t.TempDir(), "payload.bin")
					init := sendInitCommand("abc123", quoteTerminalCommand(path), quoteTerminalCommand(filepath.Dir(path)))
					if directory {
						init = directorySendInitCommand("abc123", quoteTerminalCommand(path), quoteTerminalCommand(filepath.Dir(path)))
					}
					prelude := `stty(){ if [ "$1" = -g ]; then printf saved; fi; }; `
					if minimal {
						prelude += `dd(){ case " $* " in *" iflag=fullblock "*) return 1;; esac; command dd "$@"; }; `
					}
					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancel()
					cmd := exec.CommandContext(ctx, "sh", "-c", prelude+init+"; "+sendChunkCommand("abc123", quoteTerminalCommand(path), FileTransferChunkSize))
					var output bytes.Buffer
					cmd.Stdout, cmd.Stderr = &output, &output
					input, err := cmd.StdinPipe()
					if err != nil {
						t.Fatal(err)
					}
					if err := cmd.Start(); err != nil {
						t.Fatal(err)
					}
					payload := bytes.Repeat([]byte{0, 255, '\n', 3}, FileTransferChunkSize/4)
					if short {
						payload = payload[:len(payload)/2]
					}
					for offset := 0; offset < len(payload); offset += 137 {
						if _, err := input.Write(payload[offset:min(offset+137, len(payload))]); err != nil {
							break // Inspect protocol output below, including early dd rejection.
						}
						time.Sleep(time.Millisecond)
					}
					_ = input.Close()
					if err := cmd.Wait(); err != nil {
						t.Fatalf("shell execution: %v; %s", err, output.String())
					}
					if !strings.Contains(output.String(), ":INIT:OK") {
						t.Fatalf("initialization rejected dd: %s", output.String())
					}
					want := ":ACK:8192"
					if short {
						want = ":ERROR:write"
					}
					if !strings.Contains(output.String(), want) {
						t.Fatalf("chunk result = %s, want %s", output.String(), want)
					}
					got, err := os.ReadFile(path)
					if err != nil || !bytes.Equal(got, payload) {
						t.Fatalf("stored payload mismatch: got %d bytes, want %d; %v", len(got), len(payload), err)
					}
				})
			}
		}
	}
}
