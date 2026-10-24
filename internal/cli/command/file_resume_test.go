package command

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/core/app"
)

func TestFileResumePeerBindingAndExplicitTarget(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	d := fileCommandDependencies{
		transferRoot: func() (string, error) { return root, nil },
		listSessions: func(context.Context, string) ([]mcpListedSession, error) {
			return []mcpListedSession{{ID: "new-id", Reference: "SER-2", Transport: "serial", Endpoint: "COM8", State: "open"}}, nil
		},
		newAttach: func(context.Context, string, string) (attachSession, error) {
			t.Fatal("must not attach on invalid resume")
			return nil, errors.New("unexpected")
		},
	}
	options := fileOptions{session: "SER-2", endpoint: defaultMCPEndpoint}
	peer, id, err := fileCheckpointPeer(ctx, options, d)
	if err != nil || id != "new-id" {
		t.Fatalf("peer %s %s %v", peer, id, err)
	}
	source := filepath.Join(t.TempDir(), "firmware")
	if err := os.WriteFile(source, []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := app.CreateTransferCheckpoint(ctx, root, "send", source, "/tmp/firmware", peer, app.FileTransferWireBase64)
	if err != nil {
		t.Fatal(err)
	}
	if err := runFileResume(ctx, []string{c.ID}, io.Discard, d); err == nil || !strings.Contains(err.Error(), "explicit --session") {
		t.Fatal(err)
	}
	if err := runFileResume(ctx, []string{c.ID, "--session", "SER-2", "--endpoint", "http://127.0.0.1:9999/mcp"}, io.Discard, d); err == nil || !strings.Contains(err.Error(), "differs") {
		t.Fatal(err)
	}
	options.endpoint = "http://secret:password@127.0.0.1:37099/mcp"
	if _, _, err := fileCheckpointPeer(ctx, options, d); err == nil {
		t.Fatal("persisted credential-bearing endpoint")
	}
	var output bytes.Buffer
	if err := runFileJobs(ctx, []string{"--json", "--session", "SER-2"}, &output, d); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), c.ID) || strings.Contains(output.String(), "local_path") || strings.Contains(output.String(), "COM8") {
		t.Fatal(output.String())
	}
	parsed, err := parseFileOptions("send", []string{"--resumable"}, io.Discard, writeFileUsage)
	if err != nil || !parsed.resumable {
		t.Fatalf("resumable flag: %+v %v", parsed, err)
	}
}

func TestResumedProgressDoesNotCountOldBytesAsThroughput(t *testing.T) {
	p := fileTransferReporter{resumed: 8192, started: time.Now().Add(-time.Second)}
	s := p.snapshot(8192, 16384)
	if s.speed != 0 || s.percent != 50 {
		t.Fatalf("resumed progress %+v", s)
	}
}

type cancelledPreparationSession struct{ fakeAttachSession }

func (*cancelledPreparationSession) FileTransferCancellationCheckpoint(context.Context) error {
	return context.Canceled
}

func TestSnapshotPreparationObservesHostCancellationAndJoinsWorker(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	terminal := internalFileTransferSession{attachSession: &cancelledPreparationSession{}}
	err := withFilePreparationCancellation(ctx, terminal, func(workCtx context.Context) error { <-workCtx.Done(); return workCtx.Err() })
	if !errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	// A completed local operation also stops and joins its checkpoint poller.
	terminal = internalFileTransferSession{attachSession: &fakeAttachSession{}}
	if err := withFilePreparationCancellation(ctx, terminal, func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
}
