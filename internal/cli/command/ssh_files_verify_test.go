package command

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/akira-init1/ChannelTerm/internal/core/app"
)

// TestSSHFilesVerificationOutput keeps the verification phase visible with and
// without a terminal, and never renders a failed verification as 100% complete.
func TestSSHFilesVerificationOutput(t *testing.T) {
	for _, interactive := range []bool{false, true} {
		for _, operation := range []string{"put", "get"} {
			for _, mismatch := range []bool{false, true} {
				var output bytes.Buffer
				failure := errors.New("SHA-256 mismatch")
				run := func(_ context.Context, r app.SSHFilesRequest) (app.SSHFilesResult, error) {
					if r.Verify != "sha256" || r.Verification == nil {
						t.Fatal("verification option not dispatched")
					}
					if r.Progress != nil {
						if err := r.Progress("file", 0, 3); err != nil {
							t.Fatal(err)
						}
						if err := r.Progress("file", 3, 3); err != nil {
							t.Fatal(err)
						}
					}
					if err := r.Verification(app.SSHFileVerification{Name: "file"}); err != nil {
						t.Fatal(err)
					}
					if mismatch {
						return app.SSHFilesResult{Bytes: 3}, failure
					}
					if err := r.Verification(app.SSHFileVerification{Name: "file", SourceSHA256: "source", DestinationSHA256: "destination"}); err != nil {
						t.Fatal(err)
					}
					return app.SSHFilesResult{Files: 1, Bytes: 3, VerifiedFiles: 1}, nil
				}
				err := runSSHFilesWithProgress(context.Background(), "sftp", []string{"user@host", operation, "file", "file", "--verify", "sha256", "--identity", "fixture-key"}, strings.NewReader(""), &output, run, interactive)
				text := output.String()
				if !strings.Contains(text, "Verifying SHA-256") {
					t.Fatalf("missing phase: %q", text)
				}
				if mismatch {
					if !errors.Is(err, failure) || strings.Contains(text, "100.0%") || strings.Contains(text, "matched") || strings.Contains(text, "put completed") || strings.Contains(text, "get completed") {
						t.Fatalf("failed verification: %q %v", text, err)
					}
				} else {
					local, remote := "source", "destination"
					if operation == "get" {
						local, remote = remote, local
					}
					if err != nil || !strings.Contains(text, "Local SHA-256:  "+local) || !strings.Contains(text, "Remote SHA-256: "+remote) || !strings.Contains(text, "Verify: SHA-256 matched") {
						t.Fatalf("output=%q %v", text, err)
					}
				}
			}
		}
	}
}

// TestSSHFilesDefaultVerification preserves redirected output and avoids
// checksum callbacks unless the user explicitly opts into verification.
func TestSSHFilesDefaultVerification(t *testing.T) {
	var output bytes.Buffer
	err := runSSHFiles(context.Background(), "scp", []string{"user@host", "put", "local", "remote", "--identity", "fixture-key"}, strings.NewReader(""), &output, func(_ context.Context, r app.SSHFilesRequest) (app.SSHFilesResult, error) {
		if r.Verify != "none" || r.Verification != nil {
			t.Fatal("verification enabled by default")
		}
		return app.SSHFilesResult{Files: 1}, nil
	})
	if err != nil || output.String() != "SCP put completed.\n" {
		t.Fatalf("output=%q %v", output.String(), err)
	}
}
