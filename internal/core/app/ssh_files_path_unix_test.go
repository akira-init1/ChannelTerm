//go:build linux || darwin

package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pkg/sftp"
	gossh "golang.org/x/crypto/ssh"
)

// TestSFTPTransfersPreserveLiteralRemotePaths uses actual symlink resolution
// behind a loopback SFTP server. Lexical cleaning must not redirect either
// an upload or a recursive download to a different directory.
func TestSFTPTransfersPreserveLiteralRemotePaths(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{"alias/destination", "real/child", "real/destination"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(root, "real/child"), filepath.Join(root, "alias/link")); err != nil {
		t.Fatal(err)
	}
	config := fileServerWithHandler(t, false, func(ch gossh.Channel) {
		server, err := sftp.NewServer(ch)
		if err != nil {
			t.Error(err)
			return
		}
		_ = server.Serve()
		_ = server.Close()
	})
	remote := root + "/alias/link/../destination"
	t.Run("put", func(t *testing.T) {
		local := filepath.Join(t.TempDir(), "payload.bin")
		if err := os.WriteFile(local, []byte("intended upload"), 0600); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, err := RunSSHFiles(ctx, SSHFilesRequest{Config: config, Protocol: "sftp", Operation: "put", Local: local, Remote: remote})
		if err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(filepath.Join(root, "real/destination/payload.bin"))
		if err != nil || string(data) != "intended upload" {
			t.Fatalf("wrong upload destination: data=%q err=%v", data, err)
		}
		if _, err := os.Stat(filepath.Join(root, "alias/destination/payload.bin")); !os.IsNotExist(err) {
			t.Fatal("upload reached the lexically cleaned path")
		}
	})
	t.Run("get", func(t *testing.T) {
		for file, data := range map[string]string{"real/destination/download.bin": "intended download", "alias/destination/download.bin": "wrong directory"} {
			if err := os.WriteFile(filepath.Join(root, file), []byte(data), 0600); err != nil {
				t.Fatal(err)
			}
		}
		local := filepath.Join(t.TempDir(), "download")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, err := RunSSHFiles(ctx, SSHFilesRequest{Config: config, Protocol: "sftp", Operation: "get", Remote: remote, Local: local, Recursive: true})
		if err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(filepath.Join(local, "download.bin"))
		if err != nil || string(data) != "intended download" {
			t.Fatalf("wrong download source: data=%q err=%v", data, err)
		}
	})
}
