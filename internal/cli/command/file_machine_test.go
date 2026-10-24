package command

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/akira-init1/ChannelTerm/internal/core/app"
)

func TestFileMachineOutputDoesNotContainHumanProgress(t *testing.T) {
	var output bytes.Buffer
	w := machineFileOutput(&output, true)
	_, _ = w.Write([]byte("\r[####] CLI progress\x1b[K"))
	if err := writeFileTransferVerificationSummary(w, strings.Repeat("a", 64), "/tmp/a b.bin"); err != nil {
		t.Fatal(err)
	}
	var record map[string]any
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	if record["type"] != "verified" || record["resolved_path"] != "/tmp/a b.bin" {
		t.Fatalf("record=%v", record)
	}
	if strings.Contains(output.String(), "CLI progress") {
		t.Fatal("human progress escaped")
	}
}

func TestFileMachineSendUsesSharedHostAndEmitsIdentifiedResult(t *testing.T) {
	host := newAttachTestHost(t)
	defer host.close()
	source := filepath.Join(t.TempDir(), "payload.bin")
	if err := os.WriteFile(source, []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	dependencies := fileCommandDependencies{
		newAttach: func(ctx context.Context, endpoint, id string) (attachSession, error) {
			return newMCPAttachSession(ctx, endpoint, id)
		},
		sendFile: func(_ context.Context, _ app.FileTransferSession, _ io.Reader, size int64, path string, _ app.FileTransferCancelRequested, progress app.FileTransferProgress) (app.FileTransferResult, error) {
			if err := progress(size, size); err != nil {
				return app.FileTransferResult{}, err
			}
			return app.FileTransferResult{Size: size, SHA256: strings.Repeat("a", 64), RemotePath: path}, nil
		},
	}
	var output bytes.Buffer
	err := runFileSend(context.Background(), []string{source, "/tmp/payload.bin", "--session", "board", "--endpoint", host.server.URL, "--json"}, &output, dependencies)
	if err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(&output)
	var started, verified map[string]any
	if err := decoder.Decode(&started); err != nil {
		t.Fatal(err)
	}
	if err := decoder.Decode(&verified); err != nil {
		t.Fatal(err)
	}
	if started["type"] != "started" || started["transfer_id"] == "" || started["transfer_id"] == nil || verified["type"] != "verified" || verified["resolved_path"] != "/tmp/payload.bin" {
		t.Fatalf("unexpected records: %v, %v", started, verified)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		t.Fatalf("unexpected trailing output: %v, %v", extra, err)
	}
}

func TestFileMachineFlagPreservesDefault(t *testing.T) {
	var output bytes.Buffer
	options, err := parseFileOptions("file send", []string{"--json", "--session", "SER-1"}, &output, writeFileUsage)
	if err != nil || !options.json || options.session != "SER-1" {
		t.Fatalf("options=%+v err=%v", options, err)
	}
	options, err = parseFileOptions("file send", nil, &output, writeFileUsage)
	if err != nil || options.json {
		t.Fatal("changed default presentation")
	}
}
