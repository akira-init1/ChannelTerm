package app

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pkg/sftp"
	gossh "golang.org/x/crypto/ssh"
)

// TestSFTPSafeReplacement proves that mid-copy failure preserves existing
// destinations, while a completed copy replaces them and removes staging files.
func TestSFTPSafeReplacement(t *testing.T) {
	config := fileServer(t, false)
	dir := t.TempDir()
	source, destination := filepath.Join(dir, "source"), filepath.Join(dir, "destination")
	old := []byte("original destination")
	payload := bytes.Repeat([]byte("replacement"), 10000)
	write := func(name string, data []byte) {
		t.Helper()
		if err := os.WriteFile(name, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(source, old)
	r := SSHFilesRequest{Config: config, Protocol: "sftp", Operation: "put", Local: source, Remote: "/target"}
	if _, err := RunSSHFiles(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	write(source, payload)
	r.Overwrite, r.SafeReplace = true, true
	interrupted := errors.New("fixture copy interrupted")
	r.Progress = func(_ string, n, _ int64) error {
		if n > 0 {
			return interrupted
		}
		return nil
	}
	if _, err := RunSSHFiles(context.Background(), r); !errors.Is(err, interrupted) {
		t.Fatalf("interrupted put: %v", err)
	}
	get := SSHFilesRequest{Config: config, Protocol: "sftp", Operation: "get", Remote: "/target", Local: destination}
	if _, err := RunSSHFiles(context.Background(), get); err != nil {
		t.Fatal(err)
	}
	actual, err := os.ReadFile(destination)
	if err != nil || !bytes.Equal(actual, old) {
		t.Fatalf("old remote file lost: %q %v", actual, err)
	}
	listed, err := RunSSHFiles(context.Background(), SSHFilesRequest{Config: config, Protocol: "sftp", Operation: "ls", Remote: "/"})
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range listed.Entries {
		if strings.HasPrefix(entry.Name, ".channelterm-") {
			t.Fatal("remote staging file leaked")
		}
	}
	r.Progress = nil
	if _, err := RunSSHFiles(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	get.Overwrite, get.SafeReplace = true, true
	get.Progress = func(_ string, n, _ int64) error {
		if n > 0 {
			return interrupted
		}
		return nil
	}
	if _, err := RunSSHFiles(context.Background(), get); !errors.Is(err, interrupted) {
		t.Fatalf("interrupted get: %v", err)
	}
	actual, err = os.ReadFile(destination)
	if err != nil || !bytes.Equal(actual, old) {
		t.Fatalf("old local file lost: %q %v", actual, err)
	}
	get.Progress = nil
	if _, err := RunSSHFiles(context.Background(), get); err != nil {
		t.Fatal(err)
	}
	actual, err = os.ReadFile(destination)
	if err != nil || !bytes.Equal(actual, payload) {
		t.Fatal("completed replacement mismatch", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".channelterm-") {
			t.Fatal("local staging file leaked")
		}
	}
}

type rejectReplacement struct{ sftp.FileCmder }

// PosixRename simulates a server that advertises the extension but refuses it.
func (rejectReplacement) PosixRename(*sftp.Request) error { return os.ErrPermission }

func TestSafeUploadFailurePreservesCommittedFile(t *testing.T) {
	for _, mode := range []string{"disconnect", "rename-refused"} {
		t.Run(mode, func(t *testing.T) {
			handlers := sftp.InMemHandler()
			if mode == "rename-refused" {
				handlers.FileCmd = rejectReplacement{handlers.FileCmd}
			}
			var mu sync.Mutex
			var disconnect func()
			config := fileServerWithHandler(t, false, func(ch gossh.Channel) {
				mu.Lock()
				disconnect = func() { _ = ch.Close() }
				mu.Unlock()
				server := sftp.NewRequestServer(ch, handlers)
				_ = server.Serve()
				_ = server.Close()
			})
			dir := t.TempDir()
			local := filepath.Join(dir, "source")
			old := []byte("original committed bytes")
			if err := os.WriteFile(local, old, 0600); err != nil {
				t.Fatal(err)
			}
			r := SSHFilesRequest{Config: config, Protocol: "sftp", Operation: "put", Local: local, Remote: "/target"}
			if _, err := RunSSHFiles(context.Background(), r); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(local, bytes.Repeat([]byte("new"), 50000), 0600); err != nil {
				t.Fatal(err)
			}
			r.Overwrite, r.SafeReplace = true, true
			if mode == "disconnect" {
				r.Progress = func(_ string, n, _ int64) error {
					if n > 0 {
						mu.Lock()
						stop := disconnect
						mu.Unlock()
						stop()
					}
					return nil
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_, err := RunSSHFiles(ctx, r)
			if err == nil || errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("failure=%v", err)
			}
			if mode == "disconnect" && !strings.Contains(err.Error(), "temporary remote file may remain") {
				t.Fatal("missing staging cleanup diagnostic", err)
			}
			destination := filepath.Join(dir, "download")
			if _, err := RunSSHFiles(context.Background(), SSHFilesRequest{Config: config, Protocol: "sftp", Operation: "get", Remote: "/target", Local: destination}); err != nil {
				t.Fatal(err)
			}
			if got, err := os.ReadFile(destination); err != nil || !bytes.Equal(got, old) {
				t.Fatalf("old target damaged: %q %v", got, err)
			}
			if mode == "rename-refused" {
				listed, err := RunSSHFiles(context.Background(), SSHFilesRequest{Config: config, Protocol: "sftp", Operation: "ls", Remote: "/"})
				if err != nil || len(listed.Entries) != 1 {
					t.Fatal("failed rename leaked staging file", err)
				}
			}
		})
	}
}

func TestSafeReplacementValidation(t *testing.T) {
	for _, r := range []SSHFilesRequest{
		{Protocol: "scp", Operation: "put", Overwrite: true, SafeReplace: true},
		{Protocol: "sftp", Operation: "put", SafeReplace: true},
		{Protocol: "sftp", Operation: "rm", Overwrite: true, SafeReplace: true},
	} {
		if _, err := RunSSHFiles(context.Background(), r); err == nil {
			t.Fatal("invalid replacement accepted")
		}
	}
}

func TestSafeUploadRejectsMissingRenameExtensionBeforeMutation(t *testing.T) {
	config := fileServerWithHandler(t, false, func(ch gossh.Channel) {
		var init [9]byte
		if _, err := io.ReadFull(ch, init[:]); err != nil {
			return
		}
		if binary.BigEndian.Uint32(init[:4]) != 5 || init[4] != 1 {
			t.Error("unexpected INIT")
			return
		}
		if _, err := ch.Write([]byte{0, 0, 0, 5, 2, 0, 0, 0, 3}); err != nil {
			return
		}
		if n, _ := io.Copy(io.Discard, ch); n != 0 {
			t.Errorf("sent %d bytes after unsupported extension", n)
		}
	})
	local := filepath.Join(t.TempDir(), "source")
	if err := os.WriteFile(local, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err := RunSSHFiles(ctx, SSHFilesRequest{Config: config, Protocol: "sftp", Operation: "put", Local: local, Remote: "/target", Overwrite: true, SafeReplace: true})
	if err == nil || !strings.Contains(err.Error(), "POSIX rename") {
		t.Fatalf("missing capability: %v", err)
	}
}

func TestSafeDownloadRenameFailurePreservesDestination(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "target"), 0700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(dir, "target", "marker")
	if err := os.WriteFile(marker, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	var result SSHFilesResult
	err = replaceSFTPDownload(context.Background(), root, strings.NewReader("new"), "target", "target", 3, SSHFilesRequest{}, &result)
	if err == nil {
		t.Fatal("replaced directory with file")
	}
	if got, err := os.ReadFile(marker); err != nil || string(got) != "keep" {
		t.Fatal("destination changed", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatal("staging file leaked", err)
	}
	if result.Files != 0 {
		t.Fatal("failed rename counted as committed")
	}
}

// TestSafeReplacementCancellation covers real context cancellation, not only
// a progress error. A cancelled upload may retain its uniquely named staging
// file, but neither direction may truncate the previously committed target.
func TestSafeReplacementCancellation(t *testing.T) {
	for _, direction := range []string{"put", "get"} {
		t.Run(direction, func(t *testing.T) {
			config := fileServer(t, false)
			dir := t.TempDir()
			local := filepath.Join(dir, "source")
			old := []byte("original")
			if err := os.WriteFile(local, old, 0600); err != nil {
				t.Fatal(err)
			}
			seed := SSHFilesRequest{Config: config, Protocol: "sftp", Operation: "put", Local: local, Remote: "/target"}
			if _, err := RunSSHFiles(context.Background(), seed); err != nil {
				t.Fatal(err)
			}
			payload := bytes.Repeat([]byte("new"), 50000)
			if err := os.WriteFile(local, payload, 0600); err != nil {
				t.Fatal(err)
			}
			r := seed
			r.Overwrite, r.SafeReplace = true, true
			destination := filepath.Join(dir, "download")
			if direction == "get" {
				seed.Overwrite = true
				if _, err := RunSSHFiles(context.Background(), seed); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(destination, old, 0600); err != nil {
					t.Fatal(err)
				}
				r.Operation, r.Local = "get", destination
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			r.Progress = func(_ string, n, _ int64) error {
				if n > 0 {
					cancel()
				}
				return nil
			}
			if _, err := RunSSHFiles(ctx, r); !errors.Is(err, context.Canceled) {
				t.Fatalf("cancelled copy=%v", err)
			}
			if direction == "put" {
				if _, err := RunSSHFiles(context.Background(), SSHFilesRequest{Config: config, Protocol: "sftp", Operation: "get", Local: destination, Remote: "/target"}); err != nil {
					t.Fatal(err)
				}
			}
			if got, err := os.ReadFile(destination); err != nil || !bytes.Equal(got, old) {
				t.Fatalf("committed destination changed: %q %v", got, err)
			}
			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			for _, entry := range entries {
				if strings.HasPrefix(entry.Name(), ".channelterm-") {
					t.Fatal("local staging file leaked")
				}
			}
		})
	}
}
