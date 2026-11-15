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

	"github.com/akira-init1/ChannelTerm/internal/core/app"
)

func TestSSHManagementJSONUsesExistingOperationsAndStructuredOutcomes(t *testing.T) {
	for _, operation := range []string{"mkdir", "rename", "rm", "rmdir"} {
		for _, failure := range []struct {
			err  error
			code string
		}{
			{nil, ""}, {fs.ErrNotExist, "not_found"}, {fs.ErrExist, "already_exists"},
			{fs.ErrPermission, "permission_denied"}, {context.Canceled, "cancelled"},
			{context.DeadlineExceeded, "timeout"}, {errors.New("fixture secret remote diagnostic"), "operation_failed"},
		} {
			var output bytes.Buffer
			args := []string{"user@fixture.invalid", operation, "/tmp/explicit-target", "--json", "--credentials-stdin"}
			if operation == "rename" {
				args = append(args, "/tmp/new-target")
			}
			err := runSSHFiles(context.Background(), "sftp", args, strings.NewReader(`{"password":"fixture-not-a-real-password"}`), &output, func(_ context.Context, r app.SSHFilesRequest) (app.SSHFilesResult, error) {
				if r.Operation != operation || r.Remote != "/tmp/explicit-target" || r.Recursive || r.Overwrite {
					t.Fatalf("unexpected request: %+v", r)
				}
				if operation == "rename" && r.Target != "/tmp/new-target" {
					t.Fatal("rename target lost")
				}
				return app.SSHFilesResult{}, failure.err
			})
			if !errors.Is(err, failure.err) {
				t.Fatal(err)
			}
			decoder := json.NewDecoder(&output)
			var start, result map[string]any
			if err := decoder.Decode(&start); err != nil || start["type"] != "started" {
				t.Fatal(start, err)
			}
			if err := decoder.Decode(&result); err != nil {
				t.Fatal(err)
			}
			if result["operation"] != operation || result["error_code"] != failure.code {
				t.Fatal(result)
			}
			if failure.err == nil {
				if result["type"] != "completed" || result["outcome"] != "confirmed" {
					t.Fatal(result)
				}
			} else if result["type"] == "completed" || result["outcome"] != "unknown" {
				t.Fatal(result)
			}
			if err := decoder.Decode(new(any)); err != io.EOF {
				t.Fatal("extra record", err)
			}
		}
	}
}

func TestSSHManagementJSONCancellationAfterAcknowledgementIsUncertain(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var output bytes.Buffer
	err := runSSHFilesJSON(ctx, &output, app.SSHFilesRequest{Protocol: "sftp", Operation: "mkdir"}, func(context.Context, app.SSHFilesRequest) (app.SSHFilesResult, error) {
		cancel()
		return app.SSHFilesResult{}, nil
	})
	if !errors.Is(err, context.Canceled) || strings.Contains(output.String(), `"completed"`) || !strings.Contains(output.String(), `"outcome":"unknown"`) {
		t.Fatal(output.String(), err)
	}
}
