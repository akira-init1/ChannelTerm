package app

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pkg/sftp"
	gossh "golang.org/x/crypto/ssh"
)

// TestSFTPVerifiedTransfers exercises stored-content verification without any
// remote shell, including empty files, binary data, recursive copies and staging.
func TestSFTPVerifiedTransfers(t *testing.T) {
	for _, safe := range []bool{false, true} {
		t.Run(fmt.Sprint(safe), func(t *testing.T) {
			config := fileServer(t, false)
			dir := t.TempDir()
			tree := filepath.Join(dir, "tree")
			if err := os.Mkdir(tree, 0700); err != nil {
				t.Fatal(err)
			}
			contents := map[string][]byte{"empty": {}, "二进制 'file.bin": bytes.Repeat([]byte{0, 255, 42}, 30000)}
			for name, data := range contents {
				if err := os.WriteFile(filepath.Join(tree, name), data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			for _, operation := range []string{"put", "get"} {
				local := tree
				if operation == "get" {
					local = filepath.Join(dir, "received")
				}
				var events []SSHFileVerification
				r := SSHFilesRequest{Config: config, Protocol: "sftp", Operation: operation, Local: local, Remote: "/tree", Recursive: true, Overwrite: true, SafeReplace: safe, Verify: "sha256"}
				r.Verification = func(event SSHFileVerification) error { events = append(events, event); return nil }
				result, err := RunSSHFiles(context.Background(), r)
				if err != nil || result.Files != 2 || result.VerifiedFiles != 2 || len(events) != 4 {
					t.Fatalf("%s result=%+v events=%v error=%v", operation, result, events, err)
				}
				for i := 0; i < len(events); i += 2 {
					start, end := events[i], events[i+1]
					expected := fmt.Sprintf("%x", sha256.Sum256(contents[filepath.Base(end.Name)]))
					if start.Name != end.Name || start.SourceSHA256 != "" || end.SourceSHA256 != expected || end.DestinationSHA256 != expected {
						t.Fatalf("incorrect verification events: %+v %+v", start, end)
					}
				}
			}
			for name, want := range contents {
				got, err := os.ReadFile(filepath.Join(dir, "received", name))
				if err != nil || !bytes.Equal(got, want) {
					t.Fatalf("download %q: %v", name, err)
				}
			}
		})
	}
}

type corruptSFTPReader struct {
	sftp.FileReader
	corrupt atomic.Bool
}

// Fileread simulates a destination returning altered bytes after a valid write.
func (r *corruptSFTPReader) Fileread(request *sftp.Request) (io.ReaderAt, error) {
	reader, err := r.FileReader.Fileread(request)
	if err != nil || !r.corrupt.Load() {
		return reader, err
	}
	return flippedByteReader{reader}, nil
}

type flippedByteReader struct{ io.ReaderAt }

// ReadAt changes one byte without changing the reported size.
func (r flippedByteReader) ReadAt(p []byte, offset int64) (int, error) {
	n, err := r.ReaderAt.ReadAt(p, offset)
	if offset == 0 && n > 0 {
		p[0] ^= 1
	}
	return n, err
}

// TestSFTPVerificationFailure checks completion counts and old-file preservation
// when verification detects corruption or is cancelled before safe replacement.
func TestSFTPVerificationFailure(t *testing.T) {
	for _, operation := range []string{"put", "get"} {
		for _, safe := range []bool{false, true} {
			for _, failure := range []string{"mismatch", "cancel"} {
				t.Run(fmt.Sprintf("%s/safe=%t/%s", operation, safe, failure), func(t *testing.T) {
					handlers := sftp.InMemHandler()
					reader := &corruptSFTPReader{FileReader: handlers.FileGet}
					handlers.FileGet = reader
					config := fileServerWithHandler(t, false, func(ch gossh.Channel) {
						server := sftp.NewRequestServer(ch, handlers)
						_ = server.Serve()
						_ = server.Close()
					})
					dir := t.TempDir()
					local := filepath.Join(dir, "source")
					old, replacement := []byte("old bytes"), []byte("new bytes")
					if err := os.WriteFile(local, old, 0600); err != nil {
						t.Fatal(err)
					}
					r := SSHFilesRequest{Config: config, Protocol: "sftp", Operation: "put", Local: local, Remote: "/target"}
					if _, err := RunSSHFiles(context.Background(), r); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(local, replacement, 0600); err != nil {
						t.Fatal(err)
					}
					r.Operation, r.Overwrite, r.SafeReplace, r.Verify = operation, true, safe, "sha256"
					ctx, cancel := context.WithCancel(context.Background())
					defer cancel()
					r.Verification = func(event SSHFileVerification) error {
						if event.SourceSHA256 != "" {
							t.Error("failed file reported matched")
							return nil
						}
						if failure == "cancel" {
							cancel()
							return nil
						}
						if operation == "put" {
							reader.corrupt.Store(true)
							return nil
						}
						target := local
						if safe {
							matches, err := filepath.Glob(filepath.Join(dir, ".channelterm-*.part"))
							if err != nil || len(matches) != 1 {
								return fmt.Errorf("staging files: %v %v", matches, err)
							}
							target = matches[0]
						}
						return os.WriteFile(target, replacement, 0600)
					}
					result, err := RunSSHFiles(ctx, r)
					if err == nil || result.Files != 0 || result.VerifiedFiles != 0 {
						t.Fatalf("result=%+v error=%v", result, err)
					}
					if failure == "mismatch" && !strings.Contains(err.Error(), "SHA-256 mismatch") {
						t.Fatal(err)
					}
					if failure == "cancel" && !errors.Is(err, context.Canceled) {
						t.Fatal(err)
					}
					reader.corrupt.Store(false)
					if safe {
						if operation == "put" {
							local = filepath.Join(dir, "check")
							_, err = RunSSHFiles(context.Background(), SSHFilesRequest{Config: config, Protocol: "sftp", Operation: "get", Remote: "/target", Local: local})
							if err != nil {
								t.Fatal(err)
							}
						} else {
							old = replacement
						}
						got, err := os.ReadFile(local)
						if err != nil || !bytes.Equal(got, old) {
							t.Fatalf("old target lost: %q %v", got, err)
						}
						if operation == "get" {
							matches, _ := filepath.Glob(filepath.Join(dir, ".channelterm-*.part"))
							if len(matches) != 0 {
								t.Fatalf("staging files leaked: %v", matches)
							}
						} else if failure != "cancel" {
							listing, err := RunSSHFiles(context.Background(), SSHFilesRequest{Config: config, Protocol: "sftp", Operation: "ls", Remote: "/"})
							if err != nil || len(listing.Entries) != 1 {
								t.Fatalf("remote staging leak: %+v %v", listing, err)
							}
						}
					}
				})
			}
		}
	}
}

// TestSCPVerifiedTransfers exercises separate exec channels on the same SSH
// connection, quoted paths, both directions, and verification failure boundaries.
func TestSCPVerifiedTransfers(t *testing.T) {
	for _, operation := range []string{"put", "get"} {
		for _, behavior := range []string{"binary", "empty", "mismatch", "unavailable", "malformed", "oversized", "command-error", "cancel", "negative-ack"} {
			t.Run(operation+"/"+behavior, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				data := []byte{'A', 0, 255, 'B'}
				if behavior == "empty" {
					data = nil
				}
				name := "空 'file.bin"
				remote := "/link/../dir's"
				if operation == "get" {
					remote += "/" + name
				}
				var transferred, hashed atomic.Bool
				config := fileServerWithRequests(t, false, func(ch gossh.Channel, request *gossh.Request) {
					var exec struct{ Command string }
					if request.Type != "exec" || gossh.Unmarshal(request.Payload, &exec) != nil {
						t.Error("expected exec")
						return
					}
					exit := func(status uint32) {
						_, _ = ch.SendRequest("exit-status", false, gossh.Marshal(struct{ Status uint32 }{status}))
					}
					switch {
					case exec.Command == "command -v sha256sum >/dev/null":
						if behavior == "unavailable" {
							exit(127)
						} else {
							exit(0)
						}
					case strings.HasPrefix(exec.Command, "if test -d "):
						if exec.Command != "if test -d '/link/../dir'\"'\"'s'; then printf d; else printf f; fi" {
							t.Errorf("directory command: %q", exec.Command)
						}
						_, _ = io.WriteString(ch, "d")
						exit(0)
					case strings.HasPrefix(exec.Command, "sha256sum < "):
						hashed.Store(true)
						if exec.Command != "sha256sum < '/link/../dir'\"'\"'s/空 '\"'\"'file.bin'" {
							t.Errorf("hash command: %q", exec.Command)
						}
						if behavior == "cancel" {
							cancel()
							_, _ = io.Copy(io.Discard, ch)
							return
						}
						digest := fmt.Sprintf("%x", sha256.Sum256(data))
						if behavior == "mismatch" {
							digest = strings.Repeat("0", 64)
						}
						if behavior == "malformed" {
							digest = strings.Repeat("z", 64)
						}
						if behavior == "oversized" {
							digest = strings.Repeat("x", 5000)
						}
						_, _ = fmt.Fprintf(ch, "%s  -\n", digest)
						if behavior == "command-error" {
							exit(1)
						} else {
							exit(0)
						}
					default:
						if !strings.Contains(exec.Command, "scp ") {
							t.Errorf("unexpected command: %q", exec.Command)
							exit(1)
							return
						}
						transferred.Store(true)
						reader := bufio.NewReader(ch)
						ack := func() bool { value, err := reader.ReadByte(); return err == nil && value == 0 }
						if operation == "put" {
							_, _ = ch.Write([]byte{0})
							line, err := reader.ReadString('\n')
							if err != nil || !strings.HasSuffix(line, fmt.Sprintf(" %d %s\n", len(data), name)) {
								t.Errorf("header=%q %v", line, err)
								return
							}
							_, _ = ch.Write([]byte{0})
							got := make([]byte, len(data)+1)
							if _, err := io.ReadFull(reader, got); err != nil || !bytes.Equal(got, append(append([]byte{}, data...), 0)) {
								t.Errorf("payload=%q %v", got, err)
								return
							}
							if behavior == "negative-ack" {
								_, _ = io.WriteString(ch, "\x01disk full\n")
								return
							}
							_, _ = ch.Write([]byte{0})
							_, _ = io.Copy(io.Discard, reader)
						} else {
							if !ack() {
								return
							}
							_, _ = fmt.Fprintf(ch, "C0600 %d %s\n", len(data), name)
							if !ack() {
								return
							}
							_, _ = ch.Write(data)
							if behavior == "negative-ack" {
								_, _ = io.WriteString(ch, "\x01read error\n")
								return
							}
							_, _ = ch.Write([]byte{0})
							if !ack() {
								return
							}
						}
						exit(0)
					}
				})
				local := filepath.Join(t.TempDir(), name)
				if err := os.WriteFile(local, data, 0600); err != nil {
					t.Fatal(err)
				}
				r := SSHFilesRequest{Config: config, Protocol: "scp", Operation: operation, Local: local, Remote: remote, Overwrite: true, Verify: "sha256"}
				result, err := RunSSHFiles(ctx, r)
				if behavior == "binary" || behavior == "empty" {
					if err != nil || result.Files != 1 || result.VerifiedFiles != 1 || result.Bytes != int64(len(data)) {
						t.Fatalf("result=%+v %v", result, err)
					}
				} else {
					if err == nil || result.Files != 0 || result.VerifiedFiles != 0 {
						t.Fatalf("result=%+v %v", result, err)
					}
					if behavior == "cancel" && !errors.Is(err, context.Canceled) {
						t.Fatal(err)
					}
					if behavior == "mismatch" && !strings.Contains(err.Error(), "SHA-256 mismatch") {
						t.Fatal(err)
					}
					if behavior == "unavailable" && (transferred.Load() || !strings.Contains(err.Error(), "requires remote sha256sum")) {
						t.Fatal("missing preflight", err)
					}
				}
				if hashed.Load() != (behavior != "unavailable" && behavior != "negative-ack") {
					t.Fatalf("unexpected hash execution: %t %v", hashed.Load(), err)
				}
			})
		}
	}
}

// TestSSHVerificationValidation rejects unsupported verification before I/O.
func TestSSHVerificationValidation(t *testing.T) {
	for _, r := range []SSHFilesRequest{
		{Protocol: "sftp", Operation: "ls", Remote: "/", Verify: "sha256"},
		{Protocol: "sftp", Operation: "put", Remote: "/", Local: "file", Verify: "md5"},
	} {
		if err := validateSSHFiles(r); err == nil {
			t.Fatalf("accepted invalid verification: %+v", r)
		}
	}
}

// TestSSHHashReadFailures preserves read/close errors and cancellation while
// bounding verification reads when a destination shrinks or grows after copying.
func TestSSHHashReadFailures(t *testing.T) {
	for _, mode := range []string{"short", "long", "read", "close", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			failure := errors.New("fixture I/O failure")
			reader := &hashFailureReader{Reader: strings.NewReader("payload")}
			size := int64(7)
			switch mode {
			case "short":
				size++
			case "long":
				size--
			case "read":
				reader.readErr = failure
			case "close":
				reader.closeErr = failure
			case "cancel":
				reader.afterRead = cancel
			}
			_, err := hashSSHFile(ctx, size, func() (io.ReadCloser, error) { return reader, nil })
			if err == nil || !reader.closed {
				t.Fatalf("error=%v closed=%t", err, reader.closed)
			}
			if (mode == "read" || mode == "close") && !errors.Is(err, failure) {
				t.Fatal(err)
			}
			if mode == "cancel" && !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
		})
	}
}

type hashFailureReader struct {
	io.Reader
	readErr, closeErr error
	afterRead         func()
	closed            bool
}

// Read injects failure or cancellation during hashing, after the file is opened.
func (r *hashFailureReader) Read(p []byte) (int, error) {
	if r.readErr != nil {
		return 0, r.readErr
	}
	n, err := r.Reader.Read(p)
	if r.afterRead != nil {
		r.afterRead()
	}
	return n, err
}

// Close records resource release even after a failed verification read.
func (r *hashFailureReader) Close() error { r.closed = true; return r.closeErr }

// TestVerificationCallbackFailurePreservesTarget ensures an adapter error after
// hash matching still aborts staged replacement and retains the original file.
func TestVerificationCallbackFailurePreservesTarget(t *testing.T) {
	root, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := root.WriteFile("target", []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	failure := errors.New("cannot display verification")
	r := SSHFilesRequest{Verify: "sha256", Verification: func(event SSHFileVerification) error {
		if event.SourceSHA256 != "" {
			return failure
		}
		return nil
	}}
	var result SSHFilesResult
	err = replaceSFTPDownload(context.Background(), root, strings.NewReader("new"), "target", "file", 3, r, &result)
	if !errors.Is(err, failure) || result.Files != 0 || result.VerifiedFiles != 0 {
		t.Fatalf("result=%+v %v", result, err)
	}
	got, err := root.ReadFile("target")
	if err != nil || string(got) != "old" {
		t.Fatalf("target=%q %v", got, err)
	}
	entries, err := os.ReadDir(root.Name())
	if err != nil || len(entries) != 1 {
		t.Fatalf("staging files leaked: %v %v", entries, err)
	}
}
