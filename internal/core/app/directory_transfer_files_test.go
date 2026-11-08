package app

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/core/session"
)

func TestDirectoryManifestRejectsUnsafeOrIncompleteTrees(t *testing.T) {
	hash := strings.Repeat("a", 64)
	for _, m := range []directoryManifest{
		{{Name: "../escape", Kind: "file", SHA256: hash}},
		{{Name: "/absolute", Kind: "directory"}},
		{{Name: "a\\b", Kind: "directory"}},
		{{Name: "line\nbreak", Kind: "directory"}},
		{{Name: "a/file", Kind: "file", SHA256: hash}},
		{{Name: "a", Kind: "directory"}, {Name: "a", Kind: "directory"}},
		{{Name: "link", Kind: "other"}},
		{{Name: "bad", Kind: "file", SHA256: "bad"}},
	} {
		if err := m.validate(); err == nil {
			t.Fatalf("accepted %+v", m)
		}
	}
	want := directoryManifest{{Name: "file", Kind: "file", Size: 1, SHA256: hash}}
	if err := compareDirectoryManifests(want, directoryManifest{}); !errors.Is(err, ErrFileTransferChecksumMismatch) {
		t.Fatal(err)
	}
	got := append(directoryManifest{}, want...)
	got[0].Size = 2
	if err := compareDirectoryManifests(want, got); !errors.Is(err, ErrFileTransferChecksumMismatch) {
		t.Fatal(err)
	}
}

func TestLocalDirectoryManifestIncludesEmptyDirectoriesAndContents(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "empty"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a file"), []byte("before"), 0600); err != nil {
		t.Fatal(err)
	}
	m, err := localDirectoryManifest(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(m) != 2 || m.result("").Files != 1 || m.result("").Bytes != 6 {
		t.Fatalf("manifest=%+v", m)
	}
	if err := os.WriteFile(filepath.Join(root, "a file"), []byte("after!"), 0600); err != nil {
		t.Fatal(err)
	}
	changed, err := localDirectoryManifest(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if err := compareDirectoryManifests(m, changed); !errors.Is(err, ErrFileTransferChecksumMismatch) {
		t.Fatal(err)
	}
}

// Exercise the complete per-file algorithms against generated real POSIX shell
// commands. The wrapper replaces only interactive bootstrap/TTY ownership; all
// files are disposable, and no hardware or network endpoint is opened.
func directoryFilesShell(t *testing.T, ctx context.Context) FileTransferSession {
	t.Helper()
	peer := asciiCommandPeer{newFileTransferTestSession(nil), ctx, t}
	peer.wire = FileTransferWireBase64
	return &shellFileTransferSession{terminal: directoryCommandPeer{peer}}
}

type directoryCommandPeer struct{ asciiCommandPeer }

func (p directoryCommandPeer) Write(request session.WriteRequest) (int, error) {
	// Each command uses a fresh child shell instead of a physical interactive
	// shell. Supply the already-bootstrapped control functions for its envelope.
	size := len(request.Data)
	request.Data = append([]byte("CTERM_FT=test; ct_active() { :; }; ct_idle() { :; }; "), request.Data...)
	_, err := p.asciiCommandPeer.Write(request)
	if err != nil {
		return 0, err
	}
	return size, nil
}

func (p directoryCommandPeer) ReadOutput(ctx context.Context, cursor session.OutputCursor, limit int) (session.OutputChunk, error) {
	// Model bounded Session history. A nested file transfer can overwrite an
	// outer directory cursor even though its own block reads never lose output.
	p.mu.Lock()
	dropped := int(cursor) < len(p.output)-8192
	p.mu.Unlock()
	if dropped {
		return session.OutputChunk{Dropped: true}, nil
	}
	return p.fileTransferTestSession.ReadOutput(ctx, cursor, limit)
}

func TestDirectoryFilesActualShellRoundTripAndCollision(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux target shell tools")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	source, parent, destination := t.TempDir(), t.TempDir(), t.TempDir()
	content := make([]byte, FileTransferChunkSize+73)
	for i := range content {
		content[i] = byte(i)
	}
	mustWriteDirectoryTestFile(t, filepath.Join(source, "nested", "a ' file"), content)
	mustWriteDirectoryTestFile(t, filepath.Join(source, "empty file"), nil)
	if err := os.Mkdir(filepath.Join(source, "empty directory"), 0700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(parent, "target")
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	verified := 0
	sent, err := SendDirectoryFiles(ctx, directoryFilesShell(t, ctx), source, target, func(_ string, n, _ int, _, _ int64) error { verified = n; return nil })
	if err != nil {
		t.Fatal(err)
	}
	if sent.RemotePath != target+"_1" || sent.Files != 2 || verified != 2 || sent.Bytes != int64(len(content)) {
		t.Fatalf("result=%+v verified=%d", sent, verified)
	}
	got, err := ReceiveDirectoryFiles(ctx, directoryFilesShell(t, ctx), sent.RemotePath, destination, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.SHA256 != sent.SHA256 || got.Files != 2 {
		t.Fatalf("send=%+v receive=%+v", sent, got)
	}
	data, err := os.ReadFile(filepath.Join(destination, "nested", "a ' file"))
	if err != nil || !bytes.Equal(data, content) {
		t.Fatalf("binary roundtrip: %v", err)
	}
	if info, err := os.Stat(filepath.Join(destination, "empty directory")); err != nil || !info.IsDir() {
		t.Fatalf("empty directory: %v", err)
	}
	entries, err := os.ReadDir(parent)
	if err != nil || len(entries) != 2 {
		t.Fatalf("temporary data survived: %+v %v", entries, err)
	}
}

func TestDirectoryFilesFailureNeverPublishesTree(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux target shell tools")
	}
	for _, scenario := range []string{"cancel", "source changed", "destination changed", "destination appeared"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			source, parent := t.TempDir(), t.TempDir()
			mustWriteDirectoryTestFile(t, filepath.Join(source, "file"), []byte("before"))
			target := filepath.Join(parent, "target")
			changed := false
			_, err := SendDirectoryFiles(ctx, directoryFilesShell(t, ctx), source, target, func(_ string, verified, _ int, n, _ int64) error {
				if scenario == "cancel" && n > 0 {
					return context.Canceled
				}
				if verified != 1 || changed {
					return nil
				}
				changed = true
				switch scenario {
				case "source changed":
					return os.WriteFile(filepath.Join(source, "file"), []byte("after!"), 0600)
				case "destination changed":
					stages, e := filepath.Glob(filepath.Join(parent, ".target.cterm-part-*"))
					if e != nil {
						return e
					}
					if len(stages) != 1 {
						return errors.New("missing staging directory")
					}
					return os.WriteFile(filepath.Join(stages[0], "file"), []byte("after!"), 0600)
				case "destination appeared":
					return os.WriteFile(target, []byte("preserve"), 0600)
				}
				return nil
			})
			if err == nil {
				t.Fatal("unexpected success")
			}
			if scenario == "cancel" && !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			if (scenario == "source changed" || scenario == "destination changed") && !errors.Is(err, ErrFileTransferChecksumMismatch) {
				t.Fatal(err)
			}
			if scenario == "destination appeared" {
				data, e := os.ReadFile(target)
				if e != nil || string(data) != "preserve" {
					t.Fatalf("existing destination changed: %v", e)
				}
			} else if _, e := os.Stat(target); !errors.Is(e, os.ErrNotExist) {
				t.Fatalf("partial destination published: %v", e)
			}
			stages, _ := filepath.Glob(filepath.Join(parent, ".target.cterm-part-*"))
			if len(stages) != 0 {
				t.Fatalf("staging retained: %v", stages)
			}
		})
	}
}

func TestDirectoryFilesReceiveDetectsDestinationCorruption(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux target shell tools")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	source, destination := t.TempDir(), t.TempDir()
	mustWriteDirectoryTestFile(t, filepath.Join(source, "file"), []byte("before"))
	_, err := ReceiveDirectoryFiles(ctx, directoryFilesShell(t, ctx), source, destination, func(_ string, verified, _ int, _, _ int64) error {
		if verified == 1 {
			return os.WriteFile(filepath.Join(destination, "file"), []byte("after!"), 0600)
		}
		return nil
	})
	if !errors.Is(err, ErrFileTransferChecksumMismatch) {
		t.Fatalf("corruption result: %v", err)
	}
}

func TestDirectoryFilesEmptyTreeAndUnsupportedSource(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux target shell tools")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	source, parent, destination := t.TempDir(), t.TempDir(), t.TempDir()
	sent, err := SendDirectoryFiles(ctx, directoryFilesShell(t, ctx), source, filepath.Join(parent, "empty"), nil)
	if err != nil || sent.Files != 0 || sent.Bytes != 0 {
		t.Fatalf("empty send: %+v %v", sent, err)
	}
	received, err := ReceiveDirectoryFiles(ctx, directoryFilesShell(t, ctx), sent.RemotePath, destination, nil)
	if err != nil || received.SHA256 != sent.SHA256 {
		t.Fatalf("empty receive: %+v %v", received, err)
	}
	link := filepath.Join(parent, "link")
	if err := os.Symlink(source, link); err != nil {
		t.Fatal(err)
	}
	if _, err := ReceiveDirectoryFiles(ctx, directoryFilesShell(t, ctx), link, destination, nil); err == nil {
		t.Fatal("accepted a source root symlink")
	}
	if err := os.Symlink(parent, filepath.Join(source, "link")); err != nil {
		t.Fatal(err)
	}
	if _, err := SendDirectoryFiles(ctx, directoryFilesShell(t, ctx), source, filepath.Join(parent, "rejected"), nil); !errors.Is(err, ErrUnsupportedDirectoryEntry) {
		t.Fatalf("source link accepted: %v", err)
	}
	if _, err := ReceiveDirectoryFiles(ctx, directoryFilesShell(t, ctx), source, destination, nil); !errors.Is(err, ErrUnsupportedDirectoryEntry) {
		t.Fatalf("remote link accepted: %v", err)
	}
}

func TestPublishReceivedDirectoryNeverReplacesOrNests(t *testing.T) {
	for _, collision := range []string{"absent", "file", "directory"} {
		t.Run(collision, func(t *testing.T) {
			parent := t.TempDir()
			staging, target := filepath.Join(parent, "stage"), filepath.Join(parent, "target")
			mustWriteDirectoryTestFile(t, filepath.Join(staging, "received"), []byte("contents"))
			switch collision {
			case "file":
				mustWriteDirectoryTestFile(t, target, []byte("preserve"))
			case "directory":
				mustWriteDirectoryTestFile(t, filepath.Join(target, "existing"), []byte("preserve"))
			}
			err := PublishReceivedDirectory(staging, target)
			if collision == "absent" {
				if err != nil {
					t.Fatal(err)
				}
				data, err := os.ReadFile(filepath.Join(target, "received"))
				if err != nil || string(data) != "contents" {
					t.Fatalf("publication: %q %v", data, err)
				}
				return
			}
			if err == nil {
				t.Fatal("replaced or nested into a racing destination")
			}
			preserved := target
			if collision == "directory" {
				preserved = filepath.Join(target, "existing")
				entries, err := os.ReadDir(target)
				if err != nil || len(entries) != 1 {
					t.Fatalf("destination changed: %+v %v", entries, err)
				}
			}
			data, err := os.ReadFile(preserved)
			if err != nil || string(data) != "preserve" {
				t.Fatalf("original destination changed: %q %v", data, err)
			}
			if _, err := os.Stat(filepath.Join(staging, "received")); err != nil {
				t.Fatalf("staging lost after refused publication: %v", err)
			}
		})
	}
}
