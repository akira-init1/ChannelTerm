package app

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestTransferCheckpointSnapshotValidationAndLock(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "jobs")
	source := filepath.Join(t.TempDir(), "source")
	if err := os.WriteFile(source, []byte("snapshot"), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := CreateTransferCheckpoint(ctx, root, "send", source, "/tmp/out", "test-peer", FileTransferWireBase64)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte("later edit"), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(root, c.ID, "payload"))
	if err != nil || string(got) != "snapshot" {
		t.Fatalf("snapshot: %q %v", got, err)
	}
	jobs, err := ListTransferCheckpoints(root)
	if err != nil || len(jobs) != 1 || jobs[0].ID != c.ID {
		t.Fatalf("list: %+v %v", jobs, err)
	}
	if err := os.Mkdir(filepath.Join(root, "000000000000000000000000"), 0700); err != nil {
		t.Fatal(err)
	}
	if jobs, err := ListTransferCheckpoints(root); err != nil || len(jobs) != 1 {
		t.Fatalf("unfinished preparation hides saved jobs: %v %v", jobs, err)
	}
	lock, err := lockTransferCheckpoint(root, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if second, err := lockTransferCheckpoint(root, c.ID); err == nil {
		_ = second.Close()
		t.Fatal("duplicate worker acquired job")
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
	lock, err = lockTransferCheckpoint(root, c.ID)
	if err != nil {
		t.Fatal("lock survived owner exit:", err)
	}
	_ = lock.Close()
	if _, err := ReadTransferCheckpoint(root, "../outside"); err == nil {
		t.Fatal("accepted path instead of ID")
	}
	c.Offset = 1
	if err := saveTransferCheckpoint(filepath.Join(root, c.ID), c); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadTransferCheckpoint(root, c.ID); err == nil {
		t.Fatal("accepted invalid offset")
	}
}

func TestTransferCheckpointCancelledSnapshotLeavesNoJob(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	root := t.TempDir()
	source := filepath.Join(t.TempDir(), "source")
	if err := os.WriteFile(source, []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := CreateTransferCheckpoint(ctx, root, "send", source, "/tmp/out", "", FileTransferWireBase64)
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	jobs, err := ListTransferCheckpoints(root)
	if err != nil || len(jobs) != 0 {
		t.Fatalf("partial job exposed: %v %v", jobs, err)
	}
}

func resumeTestProtocol(t *testing.T, ctx context.Context) *fileProtocol {
	t.Helper()
	peer := asciiCommandPeer{newFileTransferTestSession(nil), ctx, t}
	peer.wire = FileTransferWireBase64
	p, err := newFileProtocol(ctx, peer)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestResumableActualShellFilesAndDirectories(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fixture runs on Linux/macOS")
	}
	for _, direction := range []string{"send", "receive"} {
		for _, directory := range []bool{false, true} {
			t.Run(direction+map[bool]string{false: "File", true: "Directory"}[directory], func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				root := t.TempDir()
				local := filepath.Join(t.TempDir(), "source")
				remote := filepath.Join(t.TempDir(), "device")
				content := make([]byte, FileTransferChunkSize*2+317)
				for i := range content {
					content[i] = byte(i)
				}
				source := local
				if direction == "receive" {
					source = remote
				}
				if directory {
					if err := os.Mkdir(source, 0700); err != nil {
						t.Fatal(err)
					}
					source = filepath.Join(source, "nested file")
				}
				if err := os.WriteFile(source, content, 0600); err != nil {
					t.Fatal(err)
				}
				c, err := CreateTransferCheckpoint(ctx, root, direction, local, remote, "fixture", FileTransferWireBase64)
				if err != nil {
					t.Fatal(err)
				}
				dir := filepath.Join(root, c.ID)
				p := resumeTestProtocol(t, ctx)
				if err := p.prepareCheckpoint(ctx, dir, &c); err != nil {
					t.Fatal(err)
				}
				if err := p.checkCheckpointPublication(ctx, c); err != nil {
					t.Fatal(err)
				}
				// /tmp snapshots belong exclusively to this randomly generated fixture.
				if direction == "receive" && directory {
					defer os.RemoveAll(checkpointRemoteDirectory(c))
				}
				err = p.streamCheckpoint(ctx, dir, &c, func(done, total int64) error {
					if done >= FileTransferChunkSize {
						return context.Canceled
					}
					return nil
				})
				if !errors.Is(err, context.Canceled) || c.Offset != FileTransferChunkSize {
					t.Fatalf("checkpoint: %+v %v", c, err)
				}
				// Simulate a killed client after extra unconfirmed data reached disk.
				partial := filepath.Join(dir, "payload")
				if direction == "send" {
					partial = checkpointRemotePayload(c)
				}
				f, err := os.OpenFile(partial, os.O_APPEND|os.O_WRONLY, 0600)
				if err != nil {
					t.Fatal(err)
				}
				_, err = f.Write([]byte("unconfirmed tail"))
				_ = f.Close()
				if err != nil {
					t.Fatal(err)
				}
				// A fresh protocol and JSON reload represent GUI/Host/client restarts.
				c, err = ReadTransferCheckpoint(root, c.ID)
				if err != nil {
					t.Fatal(err)
				}
				p = resumeTestProtocol(t, ctx)
				if err := p.streamCheckpoint(ctx, dir, &c, nil); err != nil {
					t.Fatal(err)
				}
				got, err := os.ReadFile(partial)
				if err != nil {
					t.Fatal(err)
				}
				if directory {
					extracted := t.TempDir()
					if err := extractDirectoryTar(bytes.NewReader(got), extracted); err != nil {
						t.Fatal(err)
					}
					got, err = os.ReadFile(filepath.Join(extracted, "nested file"))
					if err != nil {
						t.Fatal(err)
					}
				}
				if !bytes.Equal(got, content) {
					t.Fatal("resumed payload changed")
				}
				if err := p.publishCheckpoint(ctx, dir, &c); err != nil {
					t.Fatal(err)
				}
				if c.Phase != "completed" {
					t.Fatal("publication not recorded")
				}
				published := c.RemotePath
				if direction == "receive" {
					published = c.LocalPath
				}
				if directory {
					published = filepath.Join(published, "nested file")
				}
				got, err = os.ReadFile(published)
				if err != nil || !bytes.Equal(got, content) {
					t.Fatalf("published data: %v", err)
				}
				jobs, err := ListTransferCheckpoints(root)
				if err != nil || len(jobs) != 0 {
					t.Fatalf("completed listed: %v %v", jobs, err)
				}
			})
		}
	}
}

func TestResumableUncertainPublicationNeverReplays(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(source, []byte("source"), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := CreateTransferCheckpoint(context.Background(), root, "send", source, "/tmp/result", "", FileTransferWireBase64)
	if err != nil {
		t.Fatal(err)
	}
	c.Phase = "publishing"
	if err := saveTransferCheckpoint(filepath.Join(root, c.ID), c); err != nil {
		t.Fatal(err)
	}
	// A nil terminal proves the guard runs before even opening a child shell.
	_, err = RunResumableTransfer(context.Background(), nil, root, c.ID, nil)
	if !errors.Is(err, ErrTransferPublicationUncertain) {
		t.Fatal(err)
	}
}

func TestTransferPublicationNeverReplacesExistingDestination(t *testing.T) {
	for _, directory := range []bool{false, true} {
		root := t.TempDir()
		from := filepath.Join(root, "stage")
		target := filepath.Join(root, "existing")
		if directory {
			if err := os.Mkdir(from, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(target, 0700); err != nil {
				t.Fatal(err)
			}
		} else {
			if err := os.WriteFile(from, []byte("new"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(target, []byte("old"), 0600); err != nil {
				t.Fatal(err)
			}
		}
		if err := publishTransferPath(from, target); err == nil {
			t.Fatal("replaced destination")
		}
		if _, err := os.Stat(from); err != nil {
			t.Fatal("lost staging on collision", err)
		}
		if err := publishTransferPath(from, filepath.Join(root, "available")); err != nil {
			t.Fatal(err)
		}
	}
}

func TestResumableRejectsChangedPrefixAndSource(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fixture runs on Linux/macOS")
	}
	for _, direction := range []string{"send", "receive"} {
		t.Run(direction, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			root := t.TempDir()
			local := filepath.Join(t.TempDir(), "file")
			remote := filepath.Join(t.TempDir(), "file")
			source := local
			if direction == "receive" {
				source = remote
			}
			if err := os.WriteFile(source, bytes.Repeat([]byte("x"), FileTransferChunkSize*2), 0600); err != nil {
				t.Fatal(err)
			}
			c, err := CreateTransferCheckpoint(ctx, root, direction, local, remote, "", FileTransferWireBase64)
			if err != nil {
				t.Fatal(err)
			}
			dir := filepath.Join(root, c.ID)
			p := resumeTestProtocol(t, ctx)
			if err := p.prepareCheckpoint(ctx, dir, &c); err != nil {
				t.Fatal(err)
			}
			err = p.streamCheckpoint(ctx, dir, &c, func(done, total int64) error {
				if done > 0 {
					return context.Canceled
				}
				return nil
			})
			if !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			changed := checkpointRemotePayload(c)
			f, err := os.OpenFile(changed, os.O_WRONLY, 0600)
			if err != nil {
				t.Fatal(err)
			}
			_, _ = f.Write([]byte("changed"))
			_ = f.Close()
			before, _ := os.ReadFile(changed)
			if err := resumeTestProtocol(t, ctx).streamCheckpoint(ctx, dir, &c, nil); err == nil {
				t.Fatal("accepted changed prefix/source")
			}
			after, _ := os.ReadFile(changed)
			if !bytes.Equal(before, after) {
				t.Fatal("modified file after failed validation")
			}
		})
	}
}

func TestResumableRemotePublicationRefusesRacingDestination(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fixture runs on Linux/macOS")
	}
	for _, directory := range []bool{false, true} {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		root := t.TempDir()
		source := filepath.Join(t.TempDir(), "source")
		target := filepath.Join(t.TempDir(), "target")
		if directory {
			if err := os.Mkdir(source, 0700); err != nil {
				t.Fatal(err)
			}
		} else if err := os.WriteFile(source, []byte("new"), 0600); err != nil {
			t.Fatal(err)
		}
		c, err := CreateTransferCheckpoint(ctx, root, "send", source, target, "", FileTransferWireBase64)
		if err != nil {
			t.Fatal(err)
		}
		dir := filepath.Join(root, c.ID)
		p := resumeTestProtocol(t, ctx)
		if err := p.prepareCheckpoint(ctx, dir, &c); err != nil {
			t.Fatal(err)
		}
		if err := p.checkCheckpointPublication(ctx, c); err != nil {
			t.Fatal(err)
		}
		if err := p.streamCheckpoint(ctx, dir, &c, nil); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(target, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(target, "keep"), []byte("old"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := p.publishCheckpoint(ctx, dir, &c); !errors.Is(err, ErrTransferPublicationUncertain) {
			t.Fatal(err)
		}
		entries, err := os.ReadDir(target)
		if err != nil || len(entries) != 1 || entries[0].Name() != "keep" {
			t.Fatalf("destination replaced or nested: %v %v", entries, err)
		}
		cancel()
	}
}
