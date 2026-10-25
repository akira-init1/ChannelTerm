package command

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/core/app"
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
		{"user@host", "ls", "--json"},
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
