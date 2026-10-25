package command

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"strings"
	"testing"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/core/app"
	sshtransport "github.com/akira-init1/ChannelTerm/internal/transport/ssh"
)

func TestSSHFilesJSONAndPrivateCredentialInput(t *testing.T) {
	for _, protocol := range []string{"sftp", "scp"} {
		var output bytes.Buffer
		args := []string{"user@host", "put", "file", "/file", "--json", "--credentials-stdin", "--identity", "fixture-key", "--known-hosts", "fixture-hosts"}
		err := runSSHFiles(context.Background(), protocol, args, strings.NewReader(`{"password":"fake-password","key_passphrase":"fake-passphrase"}`), &output, func(ctx context.Context, r app.SSHFilesRequest) (app.SSHFilesResult, error) {
			password, err := r.Config.Password(ctx)
			if err != nil || string(password) != "fake-password" || r.Config.ConfirmHostKey != nil {
				t.Fatal("incorrect noninteractive authentication")
			}
			if _, err := r.Config.Password(ctx); err == nil {
				t.Fatal("replayed a rejected password")
			}
			if err := r.Progress("file", 0, 10); err != nil {
				t.Fatal(err)
			}
			if err := r.Progress("file", 10, 10); err != nil {
				t.Fatal(err)
			}
			return app.SSHFilesResult{}, context.Canceled
		})
		if !errors.Is(err, context.Canceled) || strings.Contains(output.String(), "fake-") {
			t.Fatalf("err=%v output=%s", err, &output)
		}
		decoder := json.NewDecoder(&output)
		var kinds []string
		for {
			var record map[string]any
			if err := decoder.Decode(&record); err == io.EOF {
				break
			} else if err != nil {
				t.Fatal(err)
			}
			kinds = append(kinds, record["type"].(string))
		}
		if strings.Join(kinds, ",") != "started,progress,progress,cancelled" {
			t.Fatalf("record order=%v", kinds)
		}
	}
}

func TestSSHFilesResumeAndMachineValidation(t *testing.T) {
	for _, args := range [][]string{
		{"user@host", "put", "a", "b", "--credentials-stdin"},
		{"user@host", "mkdir", "/new", "--json"},
		{"user@host", "put", "a", "b", "--resume", "--recursive"},
		{"user@host", "put", "a", "b", "--resume", "--overwrite"},
		{"user@host", "put", "a", "b", "--resume", "--safe-replace"},
	} {
		if err := runSSHFiles(context.Background(), "sftp", args, strings.NewReader(""), io.Discard, func(context.Context, app.SSHFilesRequest) (app.SSHFilesResult, error) {
			t.Fatal("dispatched invalid request")
			return app.SSHFilesResult{}, nil
		}); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
	for _, payload := range []string{"null", `{}`, `{"extra":"x"}`, `{"password":2}`, `{} {}`, strings.Repeat("x", 65537)} {
		args := []string{"user@host", "put", "a", "b", "--json", "--credentials-stdin", "--identity", "fixture", "--known-hosts", "fixture"}
		called := false
		err := runSSHFiles(context.Background(), "sftp", args, strings.NewReader(payload), io.Discard, func(context.Context, app.SSHFilesRequest) (app.SSHFilesResult, error) {
			called = true
			return app.SSHFilesResult{}, nil
		})
		if (payload == "{}") != (err == nil && called) {
			t.Fatalf("credential validation: %q, %v, called=%v", payload[:min(60, len(payload))], err, called)
		}
	}
	for _, protocol := range []string{"sftp", "scp"} {
		err := runSSHFiles(context.Background(), protocol, []string{"user@host", "get", "/file", "file", "--resume", "--identity", "fixture", "--known-hosts", "fixture"}, strings.NewReader(""), io.Discard, func(_ context.Context, r app.SSHFilesRequest) (app.SSHFilesResult, error) {
			if protocol != "sftp" || !r.Resume || r.Verify != "sha256" {
				t.Fatal("invalid resume dispatch")
			}
			return app.SSHFilesResult{}, nil
		})
		if (protocol == "sftp") != (err == nil) {
			t.Fatalf("%s resume: %v", protocol, err)
		}
	}
}

func TestSSHResumedProgressExcludesExistingBytesFromSpeed(t *testing.T) {
	var output bytes.Buffer
	now := time.Unix(100, 0)
	p := sshFileProgress{output: &output, now: func() time.Time { return now }}
	if err := p.Report("file", 500, 1000); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Second)
	if err := p.Report("file", 600, 1000); err != nil {
		t.Fatal(err)
	}
	if p.last.speed != 100 || p.last.percent != 60 {
		t.Fatalf("progress=%+v", p.last)
	}
}

func TestSSHFilesJSONListsAndStatsWithoutMutatingTransferContract(t *testing.T) {
	for _, operation := range []string{"ls", "stat"} {
		var output bytes.Buffer
		args := []string{"user@host", operation, "/entry", "--json", "--credentials-stdin", "--identity", "fixture", "--known-hosts", "fixture"}
		err := runSSHFiles(context.Background(), "sftp", args, strings.NewReader(`{}`), &output, func(_ context.Context, r app.SSHFilesRequest) (app.SSHFilesResult, error) {
			if r.Operation != operation || r.Remote != "/entry" || r.Config.ConfirmHostKey != nil {
				t.Fatal("unexpected read-only request")
			}
			return app.SSHFilesResult{Entries: []sshtransport.FileInfo{{Name: "entry", Size: 42, Mode: fs.ModeSymlink | 0o777, Modified: time.Unix(0, 0)}}}, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		decoder := json.NewDecoder(&output)
		var started, entry, completed map[string]any
		for _, record := range []*map[string]any{&started, &entry, &completed} {
			if err := decoder.Decode(record); err != nil {
				t.Fatal(err)
			}
		}
		if started["type"] != "started" || entry["type"] != "entry" || entry["kind"] != "symlink" || entry["size"] != float64(42) || entry["modified"] != "1970-01-01T00:00:00Z" || completed["type"] != "completed" || completed["entries"] != float64(1) {
			t.Fatalf("records=%v %v %v", started, entry, completed)
		}
		if err := decoder.Decode(new(any)); err != io.EOF {
			t.Fatalf("trailing records: %v", err)
		}
	}
}

func TestSSHFilesJSONListingRejectsOversizeAndFailureWithoutPartialEntries(t *testing.T) {
	for _, tc := range []struct {
		name    string
		entries []sshtransport.FileInfo
		err     error
	}{
		{"count", make([]sshtransport.FileInfo, 1025), nil},
		{"name", []sshtransport.FileInfo{{Name: strings.Repeat("a", 4097)}}, nil},
		{"invalid UTF8", []sshtransport.FileInfo{{Name: string([]byte{0xff})}}, nil},
		{"negative size", []sshtransport.FileInfo{{Name: "file", Size: -1}}, nil},
		{"total name bytes", repeatedSSHEntries(257, strings.Repeat("a", 4096)), nil},
		{"cancelled", nil, context.Canceled},
		{"error", []sshtransport.FileInfo{{Name: "must-not-emit"}}, errors.New("private diagnostic")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var output bytes.Buffer
			err := runSSHFilesJSON(context.Background(), &output, app.SSHFilesRequest{Operation: "ls"}, func(context.Context, app.SSHFilesRequest) (app.SSHFilesResult, error) {
				return app.SSHFilesResult{Entries: tc.entries}, tc.err
			})
			if err == nil || strings.Contains(output.String(), `"type":"entry"`) || strings.Contains(output.String(), "private diagnostic") {
				t.Fatalf("err=%v output=%s", err, &output)
			}
		})
	}
}

func repeatedSSHEntries(count int, name string) []sshtransport.FileInfo {
	entries := make([]sshtransport.FileInfo, count)
	for i := range entries {
		entries[i].Name = name
	}
	return entries
}

func TestSSHFilesJSONStatRequiresOneEntryAndHonorsCancellation(t *testing.T) {
	for _, entries := range [][]sshtransport.FileInfo{nil, {{Name: "a"}, {Name: "b"}}} {
		var output bytes.Buffer
		err := runSSHFilesJSON(context.Background(), &output, app.SSHFilesRequest{Operation: "stat"}, func(context.Context, app.SSHFilesRequest) (app.SSHFilesResult, error) {
			return app.SSHFilesResult{Entries: entries}, nil
		})
		if err == nil || strings.Contains(output.String(), `"type":"entry"`) || !strings.Contains(output.String(), `"error_code":"listing_limit"`) {
			t.Fatalf("err=%v output=%s", err, &output)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var output bytes.Buffer
	err := runSSHFilesJSON(ctx, &output, app.SSHFilesRequest{Operation: "ls"}, func(context.Context, app.SSHFilesRequest) (app.SSHFilesResult, error) {
		cancel()
		return app.SSHFilesResult{}, nil
	})
	if !errors.Is(err, context.Canceled) || !strings.Contains(output.String(), `"type":"cancelled"`) || strings.Contains(output.String(), `"type":"completed"`) {
		t.Fatalf("err=%v output=%s", err, &output)
	}
}
