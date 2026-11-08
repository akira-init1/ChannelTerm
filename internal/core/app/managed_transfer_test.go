package app

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/core/config"
	"github.com/akira-init1/ChannelTerm/internal/core/session"
	sshtransport "github.com/akira-init1/ChannelTerm/internal/transport/ssh"
	"github.com/pkg/sftp"
	gossh "golang.org/x/crypto/ssh"
)

type managedStorageLister struct {
	sftp.FileCmder
	blocks  uint64
	unknown bool
}

func (l managedStorageLister) StatVFS(*sftp.Request) (*sftp.StatVFS, error) {
	if l.unknown {
		return nil, errors.New("no capacity extension")
	}
	return &sftp.StatVFS{Bavail: l.blocks, Frsize: 4096, Files: 100000, Favail: 99999}, nil
}

func managedFileServer(t *testing.T, blocks uint64, unknown bool) sshtransport.Config {
	t.Helper()
	h := sftp.InMemHandler()
	h.FileCmd = managedStorageLister{h.FileCmd, blocks, unknown}
	return fileServerWithHandler(t, false, func(ch gossh.Channel) {
		server := sftp.NewRequestServer(ch, h)
		_ = server.Serve()
		_ = server.Close()
	})
}

func managedTestApplication(t *testing.T, root string, ssh sshtransport.Config) *Application {
	t.Helper()
	manager := session.NewManager()
	a, err := New(Dependencies{Manager: manager, FileTransfer: config.FileTransfer{Root: root, SSH: map[string]config.FileTransferSSH{"board": {User: ssh.User, Host: ssh.Host, Port: ssh.Port, PrivateKey: ssh.PrivateKeyPath, KnownHosts: ssh.KnownHostsPath}}}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := a.CloseFileTransfers(ctx); err != nil {
			t.Error(err)
		}
		_ = manager.Close()
	})
	return a
}

func managedWait(t *testing.T, a *Application, ctx context.Context, q TransferRequest) TransferStatus {
	t.Helper()
	s, err := a.StartFileTransfer(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	s, err = a.FileTransferStatus(ctx, s.ID, false, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if s.State == "running" {
		t.Fatalf("task did not finish: %+v", s)
	}
	return s
}

func TestManagedTransferSFTPRoundTripSelectionAndArchives(t *testing.T) {
	for _, mode := range []string{"auto", "files", "tar", "tar.gz"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			mustWriteDirectoryTestFile(t, filepath.Join(root, "one.txt"), []byte("one"))
			mustWriteDirectoryTestFile(t, filepath.Join(root, "a folder", "two.txt"), []byte("two"))
			if err := os.Mkdir(filepath.Join(root, "a folder", "empty"), 0700); err != nil {
				t.Fatal(err)
			}
			a := managedTestApplication(t, root, managedFileServer(t, 1<<30, false))
			ctx := context.Background()
			q := TransferRequest{RequestID: "send", Direction: "send", Transport: "sftp", SSHProfile: "board", Sources: []string{"one.txt", "a folder"}, Destination: "/output", Mode: mode}
			sent := managedWait(t, a, ctx, q)
			if sent.State != "completed" || sent.SHA256 == "" {
				t.Fatalf("send: %+v", sent)
			}
			q = TransferRequest{RequestID: "receive", Direction: "receive", Transport: "sftp", SSHProfile: "board", Sources: []string{"/output"}, Destination: "received", Mode: "auto"}
			received := managedWait(t, a, ctx, q)
			if received.State != "completed" {
				t.Fatalf("receive: %+v", received)
			}
			if mode == "tar" || mode == "tar.gz" {
				if sent.ChecksumScope != "archive" || sent.FilesVerified != 1 {
					t.Fatalf("archive status: %+v", sent)
				}
				f, err := os.Open(filepath.Join(root, "received"))
				if err != nil {
					t.Fatal(err)
				}
				defer f.Close()
				var reader io.Reader = f
				if mode == "tar.gz" {
					gz, err := gzip.NewReader(f)
					if err != nil {
						t.Fatal(err)
					}
					defer gz.Close()
					reader = gz
				}
				archive := tar.NewReader(reader)
				files := map[string]string{}
				for {
					h, err := archive.Next()
					if err == io.EOF {
						break
					}
					if err != nil {
						t.Fatal(err)
					}
					b, err := io.ReadAll(archive)
					if err != nil {
						t.Fatal(err)
					}
					files[h.Name] = string(b)
				}
				if files["one.txt"] != "one" || files["a folder/two.txt"] != "two" {
					t.Fatalf("archive: %+v", files)
				}
			} else {
				for name, want := range map[string]string{"one.txt": "one", "a folder/two.txt": "two"} {
					b, err := os.ReadFile(filepath.Join(root, "received", name))
					if err != nil || string(b) != want {
						t.Fatalf("%s: %s %v", name, b, err)
					}
				}
				if _, err := os.Stat(filepath.Join(root, "received", "a folder", "empty")); err != nil {
					t.Fatal(err)
				}
				if received.FilesVerified != 2 || received.ChecksumScope != "manifest" {
					t.Fatalf("tree: %+v", received)
				}
			}
			// An already existing archive in auto is a file, never implicitly unpacked.
			if sent.SHA256 != received.SHA256 {
				t.Fatalf("round-trip hashes: %s %s", sent.SHA256, received.SHA256)
			}
		})
	}
}

func TestManagedTransferReceiveMultipleAsCompressedArchive(t *testing.T) {
	root := t.TempDir()
	mustWriteDirectoryTestFile(t, filepath.Join(root, "source"), []byte("payload"))
	a := managedTestApplication(t, root, managedFileServer(t, 1<<30, false))
	for _, name := range []string{"a", "b"} {
		s := managedWait(t, a, context.Background(), TransferRequest{RequestID: name, Direction: "send", Transport: "sftp", SSHProfile: "board", Sources: []string{"source"}, Destination: "/" + name})
		if s.State != "completed" {
			t.Fatal(s)
		}
	}
	s := managedWait(t, a, context.Background(), TransferRequest{RequestID: "bundle", Direction: "receive", Transport: "sftp", SSHProfile: "board", Sources: []string{"/a", "/b"}, Destination: "out.tar.gz", Mode: "tar.gz"})
	if s.State != "completed" || s.ChecksumScope != "archive" {
		t.Fatal(s)
	}
}

func TestManagedTransferRejectsUnknownOrInsufficientCapacityBeforeUpload(t *testing.T) {
	for _, unknown := range []bool{false, true} {
		t.Run(fmt.Sprint(unknown), func(t *testing.T) {
			root := t.TempDir()
			mustWriteDirectoryTestFile(t, filepath.Join(root, "source"), []byte("payload"))
			cfg := managedFileServer(t, 0, unknown)
			a := managedTestApplication(t, root, cfg)
			s := managedWait(t, a, context.Background(), TransferRequest{RequestID: "space", Direction: "send", Transport: "sftp", SSHProfile: "board", Sources: []string{"source"}, Destination: "/target"})
			if s.State != "failed" || !strings.Contains(s.Error, "storage space") {
				t.Fatal(s)
			}
			_, err := RunSSHFiles(context.Background(), SSHFilesRequest{Config: cfg, Protocol: "sftp", Operation: "stat", Remote: "/target"})
			if err == nil {
				t.Fatal("capacity refusal created destination")
			}
		})
	}
}

func TestManagedTransferRetryOwnershipCancellationAndShutdown(t *testing.T) {
	root := t.TempDir()
	mustWriteDirectoryTestFile(t, filepath.Join(root, "source"), []byte("payload"))
	a := managedTestApplication(t, root, fileServer(t, true))
	ctx := WithClientAccess(WithClientIdentity(context.Background(), session.ClientIdentity{ID: "client-a"}), ClientAccess{Write: true})
	q := TransferRequest{RequestID: "retry", Direction: "send", Transport: "sftp", SSHProfile: "board", Sources: []string{"source"}, Destination: "/target"}
	var wg sync.WaitGroup
	ids := make(chan string, 12)
	for range 12 {
		wg.Go(func() {
			s, err := a.StartFileTransfer(ctx, q)
			if err != nil {
				t.Error(err)
			}
			ids <- s.ID
		})
	}
	wg.Wait()
	close(ids)
	id := ""
	for got := range ids {
		if id != "" && id != got {
			t.Fatal("duplicate task")
		}
		id = got
	}
	q.Destination = "/different"
	if _, err := a.StartFileTransfer(ctx, q); err == nil {
		t.Fatal("conflicting retry accepted")
	}
	other := WithClientIdentity(ctx, session.ClientIdentity{ID: "client-b"})
	if _, err := a.FileTransferStatus(other, id, true, 0); err == nil {
		t.Fatal("cross-client cancellation")
	}
	readOnly := WithClientAccess(ctx, ClientAccess{})
	if _, err := a.FileTransferStatus(readOnly, id, false, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := a.FileTransferStatus(readOnly, id, true, 0); !errors.Is(err, ErrClientPermissionDenied) {
		t.Fatal(err)
	}
	s, err := a.FileTransferStatus(ctx, id, true, 10*time.Second)
	if err != nil || s.State != "cancelled" {
		t.Fatalf("cancel: %+v %v", s, err)
	}
	for range 2 {
		if err := a.CloseFileTransfers(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := a.StartFileTransfer(ctx, q); err == nil {
		t.Fatal("admitted after shutdown")
	}
}

func TestManagedTransferDisabledAndRootContainment(t *testing.T) {
	q := TransferRequest{RequestID: "safety", Direction: "send", Transport: "sftp", SSHProfile: "board", Sources: []string{"file"}, Destination: "/target"}
	a := managedTestApplication(t, "", sshtransport.Config{})
	if _, err := a.StartFileTransfer(context.Background(), q); err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatal(err)
	}
	for _, source := range []string{"../outside", filepath.Join(t.TempDir(), "absolute")} {
		q.Sources = []string{source}
		if err := validateTransferRequest(q); err == nil {
			t.Fatalf("accepted %s", source)
		}
	}
	root := t.TempDir()
	outside := t.TempDir()
	mustWriteDirectoryTestFile(t, filepath.Join(outside, "secret"), []byte("secret"))
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	handle, err := os.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Close()
	if _, _, err := scanManagedSources(context.Background(), handle, []string{"link"}); err == nil {
		t.Fatal("accepted symlink")
	}
	if _, _, err := scanManagedSources(context.Background(), handle, []string{"link/secret"}); err == nil {
		t.Fatal("escaped root")
	}
}

func TestManagedTransferSerialAutoBundleAndPerFileVerification(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux POSIX shell fixture")
	}
	for _, mode := range []string{"auto", "files"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			source := t.TempDir()
			parent := t.TempDir()
			for i := range 17 {
				mustWriteDirectoryTestFile(t, filepath.Join(source, fmt.Sprintf("file %02d", i)), []byte(fmt.Sprint(i)))
			}
			r := &transferRun{request: TransferRequest{Mode: mode, Destination: filepath.Join(parent, "out")}}
			if err := r.sendSerial(ctx, directoryFilesShell(t, ctx), source, false); err != nil {
				t.Fatal(err)
			}
			s := r.snapshot()
			want := "files"
			if mode == "auto" {
				want = "tar"
			}
			if s.WireMode != want || s.FilesVerified != 17 || s.ChecksumScope != "manifest" {
				t.Fatal(s)
			}
			a, _ := localDirectoryManifest(ctx, source)
			b, err := localDirectoryManifest(ctx, s.ResolvedPath)
			if err != nil {
				t.Fatal(err)
			}
			if err := compareDirectoryManifests(a, b); err != nil {
				t.Fatal(err)
			}
			entries, err := os.ReadDir(parent)
			if err != nil || len(entries) != 1 {
				t.Fatalf("staging leaked: %v %v", entries, err)
			}
		})
	}
}

// managedSerialFixture keeps the real application lease/event lifecycle while
// replacing only the POSIX peer. It never opens a physical serial endpoint.
type managedSerialFixture struct {
	*fakeConnectedSession
	peer *fileTransferTestSession
}

func (s managedSerialFixture) HasPendingInput() bool { return false }
func (s managedSerialFixture) ReadRecent(limit int) (session.OutputChunk, error) {
	return s.peer.ReadRecent(context.Background(), limit)
}
func (s managedSerialFixture) ReadOutput(ctx context.Context, cursor session.OutputCursor, limit int) (session.OutputChunk, error) {
	return s.peer.ReadOutput(ctx, cursor, limit)
}
func (s managedSerialFixture) Write(q session.WriteRequest) (int, error) { return s.peer.Write(q) }

func TestManagedTransferSerialCompletionReleasesLeaseAndRestoresShell(t *testing.T) {
	for _, failInit := range []bool{false, true} {
		t.Run(fmt.Sprint(failInit), func(t *testing.T) {
			root := t.TempDir()
			mustWriteDirectoryTestFile(t, filepath.Join(root, "source"), []byte("payload"))
			a := managedTestApplication(t, root, sshtransport.Config{})
			base := newFakeConnectedSession("board")
			_ = base.Connect(context.Background())
			peer := newFileTransferTestSession(nil)
			peer.wire = FileTransferWireBase64
			peer.failInitialization = failInit
			terminal := managedSerialFixture{base, peer}
			if err := a.manager.RegisterWithMetadata(terminal, session.SessionMetadata{Transport: "serial", Endpoint: "fake"}); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			s, err := a.StartFileTransfer(ctx, TransferRequest{RequestID: "serial", Direction: "send", Transport: "serial", SessionID: "board", Sources: []string{"source"}, Destination: "/out"})
			cancel() // Accepted work survives loss of its initiating MCP request.
			if err != nil {
				t.Fatal(err)
			}
			s, err = a.FileTransferStatus(context.Background(), s.ID, false, 10*time.Second)
			want := "completed"
			if failInit {
				want = "failed"
			}
			if err != nil || s.State != want {
				t.Fatalf("%+v %v", s, err)
			}
			if _, active, err := a.LeaseStatus("board"); err != nil || active {
				t.Fatalf("lease active=%v %v", active, err)
			}
			if peer.shellActive {
				t.Fatal("child shell remains active")
			}
			chunk, err := base.ReadRecentEvents(100)
			if err != nil {
				t.Fatal(err)
			}
			if len(chunk.Events) < 3 || chunk.Events[len(chunk.Events)-1].Type != session.EventLeaseReleased {
				t.Fatalf("events=%+v", chunk.Events)
			}
			if s.SessionTransferID == "" {
				t.Fatal("missing CLI-compatible correlation")
			}
		})
	}
}

func TestManagedTransferSCPUsesDedicatedPayloadAndSFTPReadback(t *testing.T) {
	root := t.TempDir()
	payload := []byte("fixture payload")
	local := filepath.Join(root, "source")
	mustWriteDirectoryTestFile(t, local, payload)
	h := sftp.InMemHandler()
	h.FileCmd = managedStorageLister{FileCmder: h.FileCmd, blocks: 1 << 30}
	var cfg sshtransport.Config
	ready := make(chan struct{})
	cfg = fileServerWithRequests(t, false, func(ch gossh.Channel, request *gossh.Request) {
		if request.Type == "subsystem" {
			server := sftp.NewRequestServer(ch, h)
			_ = server.Serve()
			_ = server.Close()
			return
		}
		<-ready
		var command struct{ Command string }
		if err := gossh.Unmarshal(request.Payload, &command); err != nil {
			t.Error(err)
			return
		}
		defer func() { _, _ = ch.SendRequest("exit-status", false, gossh.Marshal(struct{ Status uint32 }{0})) }()
		switch {
		case command.Command == "command -v sha256sum >/dev/null":
			return
		case strings.HasPrefix(command.Command, "if test -d "):
			_, _ = io.WriteString(ch, "f")
		case strings.HasPrefix(command.Command, "sha256sum < "):
			_, _ = fmt.Fprintf(ch, "%x  -\n", sha256.Sum256(payload))
		case strings.Contains(command.Command, "scp -t "):
			reader := bufio.NewReader(ch)
			_, _ = ch.Write([]byte{0})
			if _, err := reader.ReadString('\n'); err != nil {
				t.Error(err)
				return
			}
			_, _ = ch.Write([]byte{0})
			data := make([]byte, len(payload)+1)
			if _, err := io.ReadFull(reader, data); err != nil || string(data[:len(payload)]) != string(payload) {
				t.Errorf("SCP payload: %q %v", data, err)
				return
			}
			// The fixture sink stores bytes in the same SFTP filesystem, allowing
			// the managed coordinator's final readback to verify the actual copy.
			if _, err := RunSSHFiles(context.Background(), SSHFilesRequest{Config: cfg, Protocol: "sftp", Operation: "put", Local: local, Remote: "/target"}); err != nil {
				t.Error(err)
				return
			}
			_, _ = ch.Write([]byte{0})
			_, _ = io.Copy(io.Discard, reader)
		case strings.Contains(command.Command, "scp -f "):
			reader := bufio.NewReader(ch)
			if _, err := reader.ReadByte(); err != nil {
				return
			}
			_, _ = fmt.Fprintf(ch, "C0600 %d target\n", len(payload))
			if _, err := reader.ReadByte(); err != nil {
				return
			}
			_, _ = ch.Write(payload)
			_, _ = ch.Write([]byte{0})
			_, _ = reader.ReadByte()
		default:
			t.Errorf("unexpected SCP command: %s", command.Command)
		}
	})
	close(ready)
	a := managedTestApplication(t, root, cfg)
	sent := managedWait(t, a, context.Background(), TransferRequest{RequestID: "scp-send", Direction: "send", Transport: "scp", SSHProfile: "board", Sources: []string{"source"}, Destination: "/target"})
	if sent.State != "completed" || sent.FilesVerified != 1 {
		t.Fatal(sent)
	}
	got := managedWait(t, a, context.Background(), TransferRequest{RequestID: "scp-receive", Direction: "receive", Transport: "scp", SSHProfile: "board", Sources: []string{"/target"}, Destination: "received"})
	if got.State != "completed" || got.SHA256 != sent.SHA256 {
		t.Fatal(got)
	}
}

func TestManagedTransferArchiveSizingHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	count := &countingWriter{}
	p := directoryPlan{entries: []directoryEntry{{head: tar.Header{Name: "large", Typeflag: tar.TypeReg, Mode: 0600, Size: 1 << 40}}}}
	done := make(chan error, 1)
	go func() { done <- p.writeToContext(ctx, count) }()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("archive sizing ignored cancellation")
	}
}

func TestManagedTransferSSHCapacityWithoutInodeAccounting(t *testing.T) {
	v := sshtransport.FileStorage{AvailableBlocks: 10000, BlockSize: 4096, AvailableFiles: ^uint64(0)}
	if err := checkManagedSSHStorage("/out", transferFootprint{bytes: 1024, entries: 2}, v); err != nil {
		t.Fatal(err)
	}
	v.TotalFiles = 100
	if err := checkManagedSSHStorage("/out", transferFootprint{}, v); !errors.Is(err, ErrTransferSpaceUnavailable) {
		t.Fatal(err)
	}
	v.TotalFiles = 0
	v.AvailableBlocks = 0
	if err := checkManagedSSHStorage("/out", transferFootprint{}, v); !errors.Is(err, ErrTransferSpaceInsufficient) {
		t.Fatal(err)
	}
}
