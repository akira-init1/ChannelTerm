package command

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/akira-init1/ChannelTerm/internal/core/app"
)

// TestSSHFilesCLIParsing checks target-first, interspersed flags and Windows
// paths without opening a connection or reading personal credentials.
func TestSSHFilesCLIParsing(t *testing.T) {
	for _, protocol := range []string{"sftp", "scp"} {
		var got app.SSHFilesRequest
		run := func(ctx context.Context, request app.SSHFilesRequest) (app.SSHFilesResult, error) {
			got = request
			return app.SSHFilesResult{}, nil
		}
		args := []string{"user@[::1]", "put", `C:\fixture\a b.bin`, "/tmp/a b.bin", "--port", "2222", "--identity", "fixture-key", "--known-hosts", "fixture-hosts", "-r", "--overwrite"}
		var output bytes.Buffer
		if err := runSSHFiles(context.Background(), protocol, args, strings.NewReader(""), &output, run); err != nil {
			t.Fatal(err)
		}
		if got.Protocol != protocol || got.Config.Host != "::1" || got.Config.Port != 2222 || got.Local != `C:\fixture\a b.bin` || !got.Recursive || !got.Overwrite {
			t.Fatalf("request=%+v", got)
		}
		if !strings.Contains(output.String(), "completed") {
			t.Fatal("no completion")
		}
	}
}

// TestSSHFilesCLIValidation protects nonrecursive deletion and rejects malformed
// commands before authentication or any remote/local filesystem mutation.
func TestSSHFilesCLIValidation(t *testing.T) {
	for _, args := range [][]string{
		{"host", "ls"}, {"user@host", "put", "only-one"}, {"user@host", "rm", "/tmp/x", "-r"},
		{"user@host", "ls", "--port", "0"}, {"user@host", "ls", "--operation-timeout", "-1s"},
		{"user@host", "ls", "--unknown"}, {"user@host", "ls", "--identity"},
	} {
		err := runSSHFiles(context.Background(), "sftp", args, strings.NewReader(""), io.Discard, func(context.Context, app.SSHFilesRequest) (app.SSHFilesResult, error) {
			t.Fatal("invalid command dispatched")
			return app.SSHFilesResult{}, nil
		})
		if err == nil {
			t.Errorf("accepted %v", args)
		}
	}
	for _, protocol := range []string{"sftp", "scp"} {
		var output bytes.Buffer
		if err := Run(context.Background(), []string{protocol, "--help"}, strings.NewReader(""), &output, io.Discard); err != nil || !strings.Contains(output.String(), "Usage: channelterm "+protocol) {
			t.Fatalf("help=%s %v", &output, err)
		}
	}
}
