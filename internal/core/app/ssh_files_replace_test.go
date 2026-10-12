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
	"testing"
	"time"

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
	r.Progress = func(string, int64, int64) error { return interrupted }
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
	get.Progress = func(string, int64, int64) error { return interrupted }
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
