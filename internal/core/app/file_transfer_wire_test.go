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
	"time"

	"github.com/akira-init1/ChannelTerm/internal/core/session"
)

func TestBase64FileAndDirectoryRoundTrips(t *testing.T) {
	content := make([]byte, FileTransferChunkSize+57)
	for i := range content {
		content[i] = byte(i)
	}
	peer := newFileTransferTestSession(nil)
	peer.wire = FileTransferWireBase64
	if _, err := SendFile(context.Background(), peer, bytes.NewReader(content), int64(len(content)), "/tmp/file", nil); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(peer.received, content) {
		t.Fatal("send changed bytes")
	}
	peer.remote = append([]byte(nil), peer.received...)
	var received bytes.Buffer
	if _, err := ReceiveFile(context.Background(), peer, &received, "/tmp/file", nil); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(received.Bytes(), content) {
		t.Fatal("receive changed bytes")
	}
	source, destination := t.TempDir(), t.TempDir()
	mustWriteDirectoryTestFile(t, filepath.Join(source, "nested", "binary"), content)
	peer.receiving = false
	if _, err := SendDirectory(context.Background(), peer, source, "/tmp/directory", nil); err != nil {
		t.Fatal(err)
	}
	peer.remote = append([]byte(nil), peer.received...)
	if _, err := ReceiveDirectory(context.Background(), peer, "/tmp/directory", destination, nil); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(destination, "nested", "binary"))
	if err != nil || !bytes.Equal(got, content) {
		t.Fatalf("directory roundtrip: %v", err)
	}
}

func TestBase64MissingToolsAndMalformedPayloadFailClosed(t *testing.T) {
	oversized := &fileProtocol{pending: bytes.Repeat([]byte("x"), 2*fileProtocolReadSize+1)}
	if _, err := oversized.expectPhase(context.Background(), "B64"); !errors.Is(err, ErrFileTransferProtocol) {
		t.Fatalf("oversized record: %v", err)
	}
	peer := newFileTransferTestSession([]byte("data"))
	peer.wire = FileTransferWireBase64
	peer.missingBase64 = true
	if _, err := SendFile(context.Background(), peer, strings.NewReader("data"), 4, "/tmp/file", nil); err == nil || !strings.Contains(err.Error(), "base64-tools") {
		t.Fatalf("missing tools: %v", err)
	}
	if len(peer.received) != 0 || peer.shellActive {
		t.Fatal("failed preflight left data or shell")
	}
	peer.missingBase64 = false
	peer.malformedBase64 = true
	var output bytes.Buffer
	if _, err := ReceiveFile(context.Background(), peer, &output, "/tmp/file", nil); !errors.Is(err, ErrFileTransferProtocol) {
		t.Fatalf("malformed payload: %v", err)
	}
	if output.Len() != 0 || peer.shellActive {
		t.Fatal("malformed payload was published or shell retained")
	}
}

type asciiCancelPeer struct{ *fileTransferTestSession }

func (p asciiCancelPeer) FileTransferCancelRequested() bool {
	return len(p.received) >= fileTransferASCIIBlockSize
}

func TestBase64CancellationDoesNotReplayConfirmedBlock(t *testing.T) {
	peer := newFileTransferTestSession(nil)
	peer.wire = FileTransferWireBase64
	_, err := SendFile(context.Background(), asciiCancelPeer{peer}, bytes.NewReader(make([]byte, FileTransferChunkSize)), FileTransferChunkSize, "/tmp/file", nil)
	if !errors.Is(err, context.Canceled) || len(peer.received) != fileTransferASCIIBlockSize || peer.shellActive {
		t.Fatalf("cancel: bytes=%d shell=%v err=%v", len(peer.received), peer.shellActive, err)
	}
}

// Execute the generated bounded commands with an actual local POSIX shell.
// No serial endpoint, interactive shell, or production path is accessed.
type asciiCommandPeer struct {
	*fileTransferTestSession
	ctx context.Context
	t   *testing.T
}

func (s asciiCommandPeer) Write(request session.WriteRequest) (int, error) {
	if len(request.Data) > 3600 || bytes.IndexByte(request.Data, 0x11) >= 0 || bytes.IndexByte(request.Data, 0x13) >= 0 {
		s.t.Fatal("unsafe ASCII framing")
	}
	result, err := exec.CommandContext(s.ctx, "sh", "-c", string(request.Data)).Output()
	if err != nil {
		return 0, err
	}
	s.mu.Lock()
	s.emitLocked(string(result))
	s.mu.Unlock()
	return len(request.Data), nil
}

func TestBase64ActualShellBinaryBlocks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell test runs on Linux/macOS")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	content := make([]byte, FileTransferChunkSize+77)
	for i := range content {
		content[i] = byte(i)
	}
	path := filepath.Join(t.TempDir(), "a ' file")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	quoted, err := quoteRemotePath(path)
	if err != nil {
		t.Fatal(err)
	}
	peer := asciiCommandPeer{newFileTransferTestSession(nil), ctx, t}
	peer.wire = FileTransferWireBase64
	p, err := newFileProtocol(ctx, peer)
	if err != nil {
		t.Fatal(err)
	}
	for start := 0; start < len(content); start += FileTransferChunkSize {
		end := min(start+FileTransferChunkSize, len(content))
		if err := p.sendBlock(ctx, quoted, content[start:end]); err != nil {
			t.Fatal(err)
		}
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, content) {
		t.Fatalf("send: %v", err)
	}
	result := make([]byte, len(content))
	for start := 0; start < len(content); start += FileTransferChunkSize {
		end := min(start+FileTransferChunkSize, len(content))
		if err := p.receiveBlock(ctx, quoted, int64(start/FileTransferChunkSize), result[start:end]); err != nil {
			t.Fatal(err)
		}
	}
	if !bytes.Equal(result, content) {
		t.Fatal("receive changed binary bytes")
	}
}
