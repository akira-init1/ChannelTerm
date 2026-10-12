package command

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/core/app"
)

func TestSSHFileProgressStyleThrottleAndCompletion(t *testing.T) {
	var out bytes.Buffer
	now := time.Unix(100, 0)
	p := &sshFileProgress{output: &out, now: func() time.Time { return now }, request: app.SSHFilesRequest{Protocol: "sftp", Operation: "put"}}
	if err := p.Report("firmware.bin", 0, 100); err != nil {
		t.Fatal(err)
	}
	initial := out.Len()
	now = now.Add(100 * time.Millisecond)
	if err := p.Report("firmware.bin", 10, 100); err != nil {
		t.Fatal(err)
	}
	if out.Len() != initial {
		t.Fatal("unthrottled payload refresh")
	}
	now = now.Add(900 * time.Millisecond)
	if err := p.Report("firmware.bin", 50, 100); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "[##########----------] 50.0%  50 B / 100 B  50 B/s  ETA 1s\x1b[K") {
		t.Fatalf("serial style mismatch: %q", out.String())
	}
	now = now.Add(time.Second)
	p.Report("firmware.bin", 100, 100)
	if strings.Contains(out.String(), "100.0%") {
		t.Fatal("claimed completion before operation returned")
	}
	if err := p.Finish(app.SSHFilesResult{Files: 1, Bytes: 100}, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "[####################] 100.0%") || !strings.Contains(out.String(), "\x1b[K\nSFTP put completed: 1 file(s) completed, 100 B transferred.") {
		t.Fatalf("missing final frame/summary: %q", out.String())
	}
}

func TestSSHFileProgressEmptyFilesAndNames(t *testing.T) {
	var out bytes.Buffer
	p := &sshFileProgress{output: &out, now: time.Now, request: app.SSHFilesRequest{Protocol: "scp", Operation: "get"}}
	p.Report("first", 0, 1)
	p.Report("first", 1, 1)
	p.Report("empty\n\x1b[2J", 0, 0)
	if strings.Contains(out.String(), "\x1b[2J") || !strings.Contains(out.String(), "(1 completed)") {
		t.Fatal("unsafe name or missing completed-file count")
	}
	p.Finish(app.SSHFilesResult{Files: 2, Bytes: 1}, nil)
	if !strings.Contains(out.String(), "100.0%  0 B / 0 B") || !strings.Contains(out.String(), "2 file(s) completed") {
		t.Fatal("empty file did not finish", out.String())
	}
}

func TestSSHFileProgressFailureDoesNotClaimSuccess(t *testing.T) {
	for _, safe := range []bool{false, true} {
		var out bytes.Buffer
		p := &sshFileProgress{output: &out, now: time.Now, request: app.SSHFilesRequest{Protocol: "sftp", Operation: "get", SafeReplace: safe}}
		p.Report("data", 0, 100)
		p.Report("data", 100, 100)
		if err := p.Finish(app.SSHFilesResult{Bytes: 100}, context.Canceled); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(out.String(), "100.0%") || !strings.Contains(out.String(), "cancelled: 0 file(s) completed") {
			t.Fatal("cancelled close/rename reported success", out.String())
		}
		want := "Partial files may remain"
		if safe {
			want = "remote temporary files may remain"
		}
		if !strings.Contains(out.String(), want) {
			t.Fatal("missing recovery guidance")
		}
	}
}

type sshProgressBrokenOutput struct{ err error }

// Write simulates a closed output sink so the transfer can abort cleanly.
func (w sshProgressBrokenOutput) Write([]byte) (int, error) { return 0, w.err }

func TestSSHFileProgressOutputFailure(t *testing.T) {
	want := errors.New("fixture output closed")
	p := &sshFileProgress{output: sshProgressBrokenOutput{want}, now: time.Now, request: app.SSHFilesRequest{Protocol: "scp", Operation: "put"}}
	if err := p.Report("file", 0, 10); !errors.Is(err, want) {
		t.Fatalf("output failure lost: %v", err)
	}
}

func TestSSHFilesProgressTerminalAndRedirected(t *testing.T) {
	for _, interactive := range []bool{false, true} {
		for _, protocol := range []string{"sftp", "scp"} {
			var out bytes.Buffer
			args := []string{"user@host", "put", "local", "remote", "--identity", "fixture-key", "--known-hosts", "fixture-trust"}
			run := func(_ context.Context, r app.SSHFilesRequest) (app.SSHFilesResult, error) {
				if (r.Progress != nil) != interactive {
					t.Fatal("progress attached to wrong output mode")
				}
				if r.Progress != nil {
					if err := r.Progress("local", 0, 5); err != nil {
						return app.SSHFilesResult{}, err
					}
					if err := r.Progress("local", 5, 5); err != nil {
						return app.SSHFilesResult{}, err
					}
				}
				return app.SSHFilesResult{Files: 1, Bytes: 5}, nil
			}
			if err := runSSHFilesWithProgress(context.Background(), protocol, args, strings.NewReader(""), &out, run, interactive); err != nil {
				t.Fatal(err)
			}
			if !interactive && out.String() != strings.ToUpper(protocol)+" put completed.\n" {
				t.Fatalf("redirected output changed: %q", out.String())
			}
			if interactive && !strings.Contains(out.String(), "100.0%") {
				t.Fatal("interactive output missing progress")
			}
		}
	}
	// Errors retain their cause and do not produce a successful summary.
	err := runSSHFilesWithProgress(context.Background(), "sftp", []string{"user@host", "get", "remote", "local", "-i", "key", "--known-hosts", "hosts"}, strings.NewReader(""), io.Discard, func(_ context.Context, r app.SSHFilesRequest) (app.SSHFilesResult, error) {
		r.Progress("remote", 0, 1)
		return app.SSHFilesResult{}, context.Canceled
	}, true)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel cause lost: %v", err)
	}
}
