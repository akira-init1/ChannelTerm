package app

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/core/session"
)

// TestFileTransferCancellationBeforeRawMarker recovers a command that reached
// the board even when cancellation hides its READY/DATA response from the caller.
func TestFileTransferCancellationBeforeRawMarker(t *testing.T) {
	for _, send := range []bool{false, true} {
		phase := ":DATA:"
		if send {
			phase = ":READY:"
		}
		t.Run(phase, func(t *testing.T) {
			terminal := &interruptedRawMarkerSession{fileTransferTestSession: newFileTransferTestSession([]byte("payload")), phase: phase}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			var err error
			if send {
				_, err = SendFile(ctx, terminal, strings.NewReader("payload"), 7, "/tmp/payload", nil)
			} else {
				var destination bytes.Buffer
				_, err = ReceiveFile(ctx, terminal, &destination, "/tmp/payload", nil)
			}
			if !errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("transfer = %v, want recovered cancellation", err)
			}
			if !terminal.recoveryAcknowledged || terminal.pendingSend != 0 || terminal.shellActive {
				t.Fatalf("incomplete recovery: acknowledged=%t pending=%d shell=%t", terminal.recoveryAcknowledged, terminal.pendingSend, terminal.shellActive)
			}
		})
	}
}

type interruptedRawMarkerSession struct {
	*fileTransferTestSession
	phase                string
	interrupted          bool
	recoveryAcknowledged bool
}

func (s *interruptedRawMarkerSession) ReadOutput(ctx context.Context, next session.OutputCursor, limit int) (session.OutputChunk, error) {
	chunk, err := s.fileTransferTestSession.ReadOutput(ctx, next, limit)
	if !s.interrupted && bytes.Contains(chunk.Data, []byte(s.phase)) {
		s.interrupted = true
		return session.OutputChunk{}, context.Canceled
	}
	if s.interrupted && bytes.Contains(chunk.Data, []byte(":ACK:")) {
		s.recoveryAcknowledged = true
	}
	return chunk, err
}

// TestReceiveRecoveryWaitsForTTYRestoration requires the post-stty ACK before
// recovery considers a drained raw block safe for subsequent shell commands.
func TestReceiveRecoveryWaitsForTTYRestoration(t *testing.T) {
	for _, acknowledgement := range []string{"ACK:7", "ERROR:read"} {
		t.Run(acknowledgement, func(t *testing.T) {
			terminal := newFileTransferTestSession(nil)
			terminal.output = []byte("\n@CTERM:abc123:" + acknowledgement + "\n")
			shell := &shellFileTransferSession{terminal: terminal}
			protocol := &fileProtocol{terminal: shell, token: "abc123", pending: []byte("payload")}
			err := protocol.finishReceiveChunk(make([]byte, 7), 7)
			if acknowledgement == "ACK:7" && err != nil {
				t.Fatal(err)
			}
			if acknowledgement == "ERROR:read" && (err == nil || !strings.Contains(err.Error(), "read")) {
				t.Fatalf("recovery error = %v, want remote read failure", err)
			}
			if !shell.idle || protocol.cursor != session.OutputCursor(len(terminal.output)) || len(protocol.pending) != 0 {
				t.Fatalf("recovery returned before consuming the post-stty result: idle=%t cursor=%d pending=%q", shell.idle, protocol.cursor, protocol.pending)
			}
		})
	}
}

// TestFileTransferLeaseRejectsPendingShellInput covers typed and recalled lines,
// rejection without target writes, and a retry after the user clears the line.
func TestFileTransferLeaseRejectsPendingShellInput(t *testing.T) {
	for _, input := range []string{"echo unfinished", "\x1b[A"} {
		t.Run(input, func(t *testing.T) {
			a, terminal := newSerialTestApplication(t)
			_, err := a.WriteSession(context.Background(), terminal.ID(), session.WriteRequest{Actor: session.ActorUser, Data: []byte(input)})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := a.AcquireLease(terminal.ID(), "transfer", LeaseTypeFileTransfer); !errors.Is(err, ErrTerminalCommandInputPending) || !strings.Contains(err.Error(), "Ctrl+C") {
				t.Fatalf("acquire with pending shell input = %v, want actionable rejection", err)
			}
			if _, active, err := a.LeaseStatus(terminal.ID()); err != nil || active {
				t.Fatalf("rejected transfer retained lease: active=%t error=%v", active, err)
			}
			if got := string(terminal.writtenData()); got != input {
				t.Fatalf("rejection changed pending user input: %q", got)
			}
			if _, err := a.WriteSession(context.Background(), terminal.ID(), session.WriteRequest{Actor: session.ActorUser, Data: []byte{3}}); err != nil {
				t.Fatal(err)
			}
			if _, err := a.AcquireLease(terminal.ID(), "retry", LeaseTypeFileTransfer); err != nil {
				t.Fatalf("acquire after clearing input: %v", err)
			}
			if err := a.ReleaseLease(terminal.ID(), "retry"); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// TestFileTransferShellBoundsStartupWithoutLimitingTransfer checks that setup
// I/O is bounded while a completed handshake leaves the payload context usable.
func TestFileTransferShellBoundsStartupWithoutLimitingTransfer(t *testing.T) {
	terminal := &startupDeadlineSession{fileTransferTestSession: newFileTransferTestSession(nil)}
	ctx := context.Background()
	shell, err := newShellFileTransferSession(ctx, terminal)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		if err := shell.close(closeCtx); err != nil {
			t.Error(err)
		}
	}()
	if terminal.deadline.IsZero() || time.Until(terminal.deadline) > 15*time.Second {
		t.Fatalf("startup has no bounded deadline: %v", terminal.deadline)
	}
	if _, err := SendFile(ctx, shell, strings.NewReader("payload"), 7, "/tmp/file", nil); err != nil {
		t.Fatalf("transfer inherited expired startup context: %v", err)
	}
}

type startupDeadlineSession struct {
	*fileTransferTestSession
	deadline time.Time
}

// TestFileTransferShellRecoversMissingReadiness verifies that a lost startup
// response reaches its deadline and still closes the child through fresh I/O.
func TestFileTransferShellRecoversMissingReadiness(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		terminal := &startupReadTimeoutSession{fileTransferTestSession: newFileTransferTestSession(nil)}
		start := time.Now()
		_, err := newShellFileTransferSession(context.Background(), terminal)
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("missing readiness error = %v, want deadline", err)
		}
		if elapsed := time.Since(start); elapsed != 15*time.Second {
			t.Fatalf("startup took %s, want 15s", elapsed)
		}
		if terminal.shellActive {
			t.Fatal("startup recovery left the child shell active")
		}
	})
}

type startupReadTimeoutSession struct {
	*fileTransferTestSession
	reads int
}

func (s *startupReadTimeoutSession) ReadOutput(ctx context.Context, cursor session.OutputCursor, limit int) (session.OutputChunk, error) {
	s.reads++
	if s.reads == 1 {
		<-ctx.Done()
		return session.OutputChunk{}, ctx.Err()
	}
	return s.fileTransferTestSession.ReadOutput(ctx, cursor, limit)
}

func (s *startupDeadlineSession) WriteContext(ctx context.Context, request session.WriteRequest) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if strings.Contains(string(request.Data), "while read -r x;do eval") {
		s.deadline, _ = ctx.Deadline()
	}
	return s.Write(request)
}

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

// TestFileTransferBootstrapPreservesIgnoredBashHistory verifies that removing a
// bootstrap never deletes an older entry when Bash did not record that bootstrap.
func TestFileTransferBootstrapPreservesIgnoredBashHistory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires Bash")
	}
	for _, setup := range []string{"HISTIGNORE='*CTERM_FT*'", "set +o history"} {
		t.Run(setup, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "history")
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, "bash", "--noprofile", "--norc", "-i")
			cmd.Env = append(os.Environ(), "HISTFILE=/dev/null", "HISTSIZE=100", "HISTCONTROL=", "HISTIGNORE=", "PS1=", "PS2=")
			cmd.Stdin = strings.NewReader("stty(){ :; }\nhistory -c\necho USER_BEFORE_TRANSFER\n" + setup + "\n" + fileTransferShellBootstrapCommand("00112233445566778899aabb") + "\nexit\nhistory -w " + quoteTerminalCommand(path) + "\nexit\n")
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("Bash: %v; %s", err, output)
			}
			history, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(history), setup+"\n") || !strings.Contains(string(history), "echo USER_BEFORE_TRANSFER\n") {
				t.Fatalf("transfer deleted user history: %s", history)
			}
			if strings.Contains(string(history), "CTERM_FT=") {
				t.Fatalf("transfer bootstrap remains in history: %s", history)
			}
		})
	}
}
