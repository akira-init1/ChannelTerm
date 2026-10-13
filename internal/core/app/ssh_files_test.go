package app

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	sshtransport "github.com/akira-init1/ChannelTerm/internal/transport/ssh"
	"github.com/pkg/sftp"
	gossh "golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

func fileServer(t *testing.T, stall bool) sshtransport.Config {
	return fileServerWithHandler(t, stall, nil)
}

// fileServerWithHandler allows SFTP or SCP wire-level fixtures while
// retaining the same generated credentials and loopback-only SSH lifecycle.
func fileServerWithHandler(t *testing.T, stall bool, serve func(gossh.Channel)) sshtransport.Config {
	t.Helper()
	var handler func(gossh.Channel, *gossh.Request)
	if serve != nil {
		handler = func(ch gossh.Channel, _ *gossh.Request) { serve(ch) }
	}
	return fileServerWithRequests(t, stall, handler)
}

// fileServerWithRequests accepts concurrent channels so a file transfer may
// request a checksum on the same SSH connection while SCP awaits the next file.
func fileServerWithRequests(t *testing.T, stall bool, serve func(gossh.Channel, *gossh.Request)) sshtransport.Config {
	t.Helper()
	config := sshTestConfig(t)
	key, err := os.ReadFile(config.PrivateKeyPath)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := gossh.ParsePrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	config.Host, _, _ = net.SplitHostPort(listener.Addr().String())
	_, port, _ := net.SplitHostPort(listener.Addr().String())
	config.Port, _ = strconv.Atoi(port)
	if err := os.WriteFile(config.KnownHostsPath, []byte(knownhosts.Line([]string{listener.Addr().String()}, signer.PublicKey())+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	serverConfig := &gossh.ServerConfig{PublicKeyCallback: func(meta gossh.ConnMetadata, key gossh.PublicKey) (*gossh.Permissions, error) {
		if !bytes.Equal(key.Marshal(), signer.PublicKey().Marshal()) {
			return nil, errors.New("wrong fixture key")
		}
		return nil, nil
	}}
	serverConfig.AddHostKey(signer)
	handlers := sftp.InMemHandler()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	var workers sync.WaitGroup
	go func() {
		defer close(done)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			workers.Add(1)
			go func() {
				defer workers.Done()
				defer conn.Close()
				stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
				defer stop()
				server, channels, requests, err := gossh.NewServerConn(conn, serverConfig)
				if err != nil {
					return
				}
				defer server.Close()
				go gossh.DiscardRequests(requests)
				for request := range channels {
					ch, requests, err := request.Accept()
					if err != nil {
						return
					}
					workers.Add(1)
					go func() {
						defer workers.Done()
						defer ch.Close()
						for req := range requests {
							if req.Type != "subsystem" && !(req.Type == "exec" && serve != nil) {
								_ = req.Reply(false, nil)
								continue
							}
							if stall {
								_ = server.Wait()
								return
							}
							_ = req.Reply(true, nil)
							if serve != nil {
								serve(ch, req)
								_ = ch.Close()
								break
							}
							sftpServer := sftp.NewRequestServer(ch, handlers)
							_ = sftpServer.Serve()
							_ = sftpServer.Close()
							_ = ch.Close()
							break
						}
					}()
				}
			}()
		}
	}()
	t.Cleanup(func() { cancel(); _ = listener.Close(); <-done; workers.Wait() })
	return config
}

// TestSCPDownloadCompletionCounts requires both local copying and the remote
// acknowledgement, retaining earlier completed files when a later file fails.
func TestSCPDownloadCompletionCounts(t *testing.T) {
	for _, mode := range []string{"success", "negative-ack", "missing-ack", "partial-tree", "cancelled-ack"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			config := fileServerWithHandler(t, false, func(ch gossh.Channel) {
				ack := func() bool {
					var b [1]byte
					_, err := io.ReadFull(ch, b[:])
					return err == nil && b[0] == 0
				}
				if !ack() {
					return
				}
				if mode == "partial-tree" {
					_, _ = io.WriteString(ch, "D0700 0 fixture.bin\n")
					if !ack() {
						return
					}
					_, _ = io.WriteString(ch, "C0600 0 completed.bin\n")
					if !ack() {
						return
					}
					_, _ = ch.Write([]byte{0})
					if !ack() {
						return
					}
				}
				_, _ = io.WriteString(ch, "C0600 3 fixture.bin\n")
				if !ack() {
					return
				}
				_, _ = io.WriteString(ch, "ABC")
				switch mode {
				case "success":
					_, _ = ch.Write([]byte{0})
					if ack() {
						_, _ = ch.SendRequest("exit-status", false, gossh.Marshal(struct{ Status uint32 }{0}))
					}
				case "negative-ack", "partial-tree":
					_, _ = io.WriteString(ch, "\x01fixture source read failed\n")
					_, _ = io.Copy(io.Discard, ch)
				case "cancelled-ack":
					_, _ = io.Copy(io.Discard, ch)
				}
			})
			request := SSHFilesRequest{
				Config: config, Protocol: "scp", Operation: "get", Remote: "/fixture.bin",
				Local: filepath.Join(t.TempDir(), "download"), Recursive: mode == "partial-tree",
			}
			if mode == "cancelled-ack" {
				request.Progress = func(_ string, n, total int64) error {
					if n == total {
						cancel()
					}
					return nil
				}
			}
			result, err := RunSSHFiles(ctx, request)
			if (err == nil) != (mode == "success") {
				t.Fatalf("download error = %v", err)
			}
			if (mode == "negative-ack" || mode == "partial-tree") && !strings.Contains(err.Error(), "fixture source read failed") {
				t.Fatalf("lost sender error: %v", err)
			}
			if mode == "cancelled-ack" && !errors.Is(err, context.Canceled) {
				t.Fatalf("lost cancellation: %v", err)
			}
			wantFiles := int64(0)
			if mode == "success" || mode == "partial-tree" {
				wantFiles = 1
			}
			if result.Files != wantFiles || result.Bytes != 3 {
				t.Fatalf("counts = %+v, want %d completed files and 3 bytes; error = %v", result, wantFiles, err)
			}
		})
	}
}

// TestSFTPFileOperations exercises real SFTP packets through SSH, tree copying,
// management operations, overwrite protection, and binary/empty-file fidelity.
func TestSFTPFileOperations(t *testing.T) {
	config := fileServer(t, false)
	local := t.TempDir()
	tree := filepath.Join(local, "tree")
	if err := os.MkdirAll(filepath.Join(tree, "empty"), 0700); err != nil {
		t.Fatal(err)
	}
	payload := bytes.Repeat([]byte{0, 1, 2, 255, '\n'}, 10000)
	if err := os.WriteFile(filepath.Join(tree, "file name.bin"), payload, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tree, "zero"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	call := func(op, remote, local, target string, recursive, overwrite bool) (SSHFilesResult, error) {
		return RunSSHFiles(context.Background(), SSHFilesRequest{Config: config, Protocol: "sftp", Operation: op, Remote: remote, Local: local, Target: target, Recursive: recursive, Overwrite: overwrite})
	}
	if result, err := call("put", "/uploaded", tree, "", true, false); err != nil || result.Files != 2 || result.Bytes != int64(len(payload)) {
		t.Fatalf("put=%+v %v", result, err)
	}
	destination := filepath.Join(local, "download")
	if _, err := call("get", "/uploaded", destination, "", true, false); err != nil {
		t.Fatal(err)
	}
	actual, err := os.ReadFile(filepath.Join(destination, "file name.bin"))
	if err != nil || !bytes.Equal(actual, payload) {
		t.Fatal("round trip mismatch", err)
	}
	if _, err := os.Stat(filepath.Join(destination, "empty")); err != nil {
		t.Fatal(err)
	}
	if _, err := call("put", "/uploaded/zero", filepath.Join(tree, "zero"), "", false, false); err == nil {
		t.Fatal("overwrote without opt-in")
	}
	if _, err := call("put", "/uploaded/zero", filepath.Join(tree, "zero"), "", false, true); err != nil {
		t.Fatal(err)
	}
	if result, err := call("ls", "/uploaded", "", "", false, false); err != nil || len(result.Entries) != 3 {
		t.Fatalf("ls=%+v %v", result, err)
	}
	if _, err := call("stat", "/missing", "", "", false, false); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing=%v", err)
	}
	if _, err := call("rename", "/uploaded/zero", "", "/uploaded/renamed", false, false); err != nil {
		t.Fatal(err)
	}
	if _, err := call("rm", "/uploaded/renamed", "", "", false, false); err != nil {
		t.Fatal(err)
	}
	if _, err := call("mkdir", "/newdir", "", "", false, false); err != nil {
		t.Fatal(err)
	}
	if _, err := call("rm", "/newdir", "", "", false, false); err == nil {
		t.Fatal("rm removed a directory")
	}
	if _, err := call("rmdir", "/newdir", "", "", false, false); err != nil {
		t.Fatal(err)
	}
	if _, err := call("get", "/uploaded", filepath.Join(local, "no-recursion"), "", false, false); err == nil {
		t.Fatal("directory accepted without recursion")
	}
}

// TestFileConnectionCancellationAndClose checks setup cancellation, blocked
// subsystem requests, caller lifetime after setup, and repeatable Close.
func TestFileConnectionCancellationAndClose(t *testing.T) {
	for _, stall := range []bool{false, true} {
		config := fileServer(t, stall)
		config.Timeout = 150 * time.Millisecond
		transport, err := sshtransport.New(config)
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		client, err := transport.OpenFiles(ctx, "sftp")
		if stall {
			cancel()
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("stalled subsystem=%v", err)
			}
			continue
		}
		if err != nil {
			cancel()
			t.Fatal(err)
		}
		cancel()
		if _, err := client.List("/"); !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled files=%v", err)
		}
		if err := client.Close(); err != nil {
			t.Fatal(err)
		}
		if err := client.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

// TestSSHFileCopyCancellationPreservesError checks progress aborts and changing
// sizes without writing beyond the declared source size.
func TestSSHFileCopyCancellationPreservesError(t *testing.T) {
	var output bytes.Buffer
	r := SSHFilesRequest{Progress: func(_ string, n, _ int64) error {
		if n > 0 {
			return context.Canceled
		}
		return nil
	}}
	if _, err := copySSHFile(context.Background(), &output, bytes.NewReader([]byte("abc")), "fixture", 3, r); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	output.Reset()
	if _, err := copySSHFile(context.Background(), &output, bytes.NewReader([]byte("abcd")), "fixture", 3, SSHFilesRequest{}); err == nil || output.Len() > 3 {
		t.Fatal("growing file not rejected")
	}
	if _, err := copySSHFile(context.Background(), io.Discard, bytes.NewReader(nil), "fixture", 1, SSHFilesRequest{}); err == nil {
		t.Fatal("truncated file not rejected")
	}
}

// TestSSHFileProgressStartsBeforePayload covers empty files and cancellation
// from the initial callback before any payload is written.
func TestSSHFileProgressStartsBeforePayload(t *testing.T) {
	for _, payload := range []string{"", "abc"} {
		var output bytes.Buffer
		var reports []int64
		r := SSHFilesRequest{Progress: func(name string, n, total int64) error {
			if name != "fixture" || total != int64(len(payload)) {
				t.Fatal("incorrect file progress metadata")
			}
			reports = append(reports, n)
			return nil
		}}
		if _, err := copySSHFile(context.Background(), &output, strings.NewReader(payload), "fixture", int64(len(payload)), r); err != nil {
			t.Fatal(err)
		}
		if len(reports) == 0 || reports[0] != 0 || reports[len(reports)-1] != int64(len(payload)) {
			t.Fatalf("progress=%v", reports)
		}
	}
	var output bytes.Buffer
	_, err := copySSHFile(context.Background(), &output, strings.NewReader("abc"), "fixture", 3, SSHFilesRequest{Progress: func(string, int64, int64) error { return context.Canceled }})
	if !errors.Is(err, context.Canceled) || output.Len() != 0 {
		t.Fatal("initial progress cancellation wrote payload", err)
	}
}

// TestUploadCurrentDirectoryValidation resolves dot to a real basename before
// portable-name validation and still rejects cancellation before traversal.
func TestUploadCurrentDirectoryValidation(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := validateUploadTree(context.Background(), ".", true); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := validateUploadTree(ctx, ".", true); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
