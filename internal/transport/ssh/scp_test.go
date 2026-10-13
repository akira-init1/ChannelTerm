package ssh

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	gossh "golang.org/x/crypto/ssh"
)

// TestReceiveSCPRejectsUntrustedRecords protects paths, framing and parser bounds.
func TestReceiveSCPRejectsUntrustedRecords(t *testing.T) {
	for _, input := range []string{
		"C0644 0 ../escape\n", "C0644 0 /absolute\n", "C0644 0 C:drive\n", "C0644 0 other\n",
		"C0644 -1 file\n", "C9999 0 file\n", "C0644 9223372036854775808 file\n", "D0755 0 file\n",
		"C0644 3 file\nab", "E\n", "Tbad\n", "C0644 0 " + strings.Repeat("x", 5000) + "\n", "\x01failure\n",
	} {
		_, err := receiveSCP(bufio.NewReaderSize(strings.NewReader(input), 4096), io.Discard, "file", false, func(entry SCPEntry, r io.Reader) error {
			if entry.Path != "file" {
				t.Fatalf("unsafe name reached receiver: %q", entry.Path)
			}
			if r != nil {
				_, err := io.Copy(io.Discard, r)
				return err
			}
			return nil
		}, nil)
		if err == nil {
			t.Errorf("accepted malformed record %q", input[:min(len(input), 100)])
		}
	}
}

// TestReceiveSCPVerificationCompletion requires acknowledgements before calling
// verification, and counts only the files whose verification also succeeded.
func TestReceiveSCPVerificationCompletion(t *testing.T) {
	for _, status := range []string{"\x00", "\x01source failed\n"} {
		failure := errors.New("checksum mismatch")
		calls := 0
		wire := "D0700 0 tree\nC0600 0 first\n\x00C0600 0 second\n" + status + "E\n"
		files, err := receiveSCP(bufio.NewReader(strings.NewReader(wire)), io.Discard, "tree", true, func(_ SCPEntry, reader io.Reader) error {
			if reader != nil {
				_, err := io.Copy(io.Discard, reader)
				return err
			}
			return nil
		}, func(entry SCPEntry) error {
			calls++
			if entry.Path == "tree/second" {
				return failure
			}
			return nil
		})
		if files != 1 || err == nil {
			t.Fatalf("files=%d error=%v", files, err)
		}
		if status == "\x00" {
			if calls != 2 || !errors.Is(err, failure) {
				t.Fatalf("calls=%d error=%v", calls, err)
			}
		} else if calls != 1 {
			t.Fatal("verification ran before acknowledgement")
		}
	}
}

func runSCPFixture(t *testing.T, ch gossh.Channel, behavior string) {
	t.Helper()
	defer ch.Close()
	reader := bufio.NewReader(ch)
	ack := func() bool {
		b, err := reader.ReadByte()
		if err != nil || b != 0 {
			t.Errorf("fixture acknowledgement=%d %v", b, err)
			return false
		}
		return true
	}
	if behavior == "scp-download" {
		if !ack() {
			return
		}
		_, _ = io.WriteString(ch, "C0644 3 fixture.bin\n")
		if !ack() {
			return
		}
		_, _ = ch.Write([]byte{'A', 0, 'B', 0})
		if !ack() {
			return
		}
	} else if behavior == "scp-upload" || behavior == "scp-upload-empty" {
		want := []byte{'A', 0, 'B'}
		if behavior == "scp-upload-empty" {
			want = nil
		}
		_, _ = ch.Write([]byte{0})
		line, err := reader.ReadString('\n')
		if err != nil || line != fmt.Sprintf("C0644 %d fixture.bin\n", len(want)) {
			t.Errorf("fixture header=%q %v", line, err)
			return
		}
		_, _ = ch.Write([]byte{0})
		payload := make([]byte, len(want)+1)
		if _, err := io.ReadFull(reader, payload); err != nil || !bytes.Equal(payload, append(want, 0)) {
			t.Errorf("fixture payload=%v %v", payload, err)
			return
		}
		_, _ = ch.Write([]byte{0})
		_, _ = io.Copy(io.Discard, reader)
	} else {
		// A peer that never starts SCP must still be released by cancellation.
		_, _ = io.Copy(io.Discard, reader)
		return
	}
	_, _ = ch.SendRequest("exit-status", false, gossh.Marshal(struct{ Status uint32 }{0}))
}

// TestSCPConnectionCompletion protects binary transfers and normal peer-initiated
// EOF after the final download acknowledgement, which must not report failure.
func TestSCPConnectionCompletion(t *testing.T) {
	for _, mode := range []string{"scp-upload", "scp-upload-empty", "scp-download"} {
		t.Run(mode, func(t *testing.T) {
			server := startTestServer(t, mode)
			transport, err := New(server.config)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			client, err := transport.OpenFiles(ctx, "scp")
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			if strings.HasPrefix(mode, "scp-upload") {
				data := []byte{'A', 0, 'B'}
				if mode == "scp-upload-empty" {
					data = nil
				}
				source := fstest.MapFS{"fixture.bin": &fstest.MapFile{Data: data, Mode: 0644}}
				var reports []int64
				files, n, err := client.UploadSCP(source, "fixture.bin", "/tmp/fixture.bin", false, false, func(name string, n, total int64) error {
					if name != "fixture.bin" || total != int64(len(data)) {
						t.Error("incorrect progress metadata")
					}
					reports = append(reports, n)
					return nil
				}, nil)
				if err != nil || files != 1 || n != int64(len(data)) {
					t.Fatalf("upload=%d %d %v", files, n, err)
				}
				if len(reports) == 0 || reports[0] != 0 || reports[len(reports)-1] != int64(len(data)) {
					t.Fatalf("progress=%v", reports)
				}
			} else {
				var output bytes.Buffer
				files, err := client.DownloadSCP("/tmp/fixture.bin", false, func(entry SCPEntry, r io.Reader) error { _, err := io.Copy(&output, r); return err }, nil)
				if err != nil || files != 1 || !bytes.Equal(output.Bytes(), []byte{'A', 0, 'B'}) {
					t.Fatalf("download=%d %v %v", files, output.Bytes(), err)
				}
			}
			select {
			case req := <-server.requests:
				if req.Type != "exec" {
					t.Fatal("file operation opened a PTY")
				}
			default:
				t.Fatal("missing exec request")
			}
		})
	}
}

// TestSCPBlockedTransferCancellation releases a peer that accepts exec but
// never acknowledges SCP. No socket or protocol worker may retain the caller.
func TestSCPBlockedTransferCancellation(t *testing.T) {
	server := startTestServer(t, "scp-stall")
	transport, err := New(server.config)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client, err := transport.OpenFiles(ctx, "scp")
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	done := make(chan error, 1)
	go func() {
		_, err := client.DownloadSCP("fixture.bin", false, func(SCPEntry, io.Reader) error { return nil }, nil)
		done <- err
	}()
	select {
	case <-server.requests:
	case <-time.After(time.Second):
		t.Fatal("no SCP request")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancel did not release SCP")
	}
}

// TestReceiveSCPTree verifies binary framing and directory acknowledgements
// independently of the upload encoder, including duplicate-entry rejection.
func TestReceiveSCPTree(t *testing.T) {
	wire := "T1 0 2 0\nD0755 0 tree\nC0644 3 data.bin\nA\x00B\x00D0700 0 empty\nE\nE\n"
	var acks bytes.Buffer
	var paths []string
	files, err := receiveSCP(bufio.NewReader(strings.NewReader(wire)), &acks, "tree", true, func(entry SCPEntry, r io.Reader) error {
		paths = append(paths, entry.Path)
		if entry.Mode.IsRegular() {
			data, err := io.ReadAll(r)
			if err != nil {
				return err
			}
			if !bytes.Equal(data, []byte{'A', 0, 'B'}) {
				t.Fatalf("payload=%q", data)
			}
		}
		return nil
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(paths, ",") != "tree,tree/data.bin,tree/empty" || acks.Len() != 8 || files != 1 {
		t.Fatalf("paths=%v acks=%d files=%d", paths, acks.Len(), files)
	}
	duplicate := "D0755 0 tree\nC0644 0 x\n\x00C0644 0 x\n\x00E\n"
	if _, err := receiveSCP(bufio.NewReader(strings.NewReader(duplicate)), io.Discard, "tree", true, func(SCPEntry, io.Reader) error { return nil }, nil); err == nil {
		t.Fatal("duplicate accepted")
	}
}

// TestReceiveSCPCountsOnlyAcknowledgedFiles covers sender rejection and failure
// to send the receiver's final acknowledgement, including empty payloads.
func TestReceiveSCPCountsOnlyAcknowledgedFiles(t *testing.T) {
	for _, tc := range []struct {
		name     string
		status   string
		failAck  int
		wantFile int64
	}{
		{"success", "\x00", 0, 1},
		{"sender-error", "\x01read failed\n", 0, 0},
		{"missing-status", "", 0, 0},
		{"invalid-status", "\x03", 0, 0},
		{"receiver-error", "\x00", 3, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			writer := &failingSCPAckWriter{failAt: tc.failAck}
			files, err := receiveSCP(bufio.NewReader(strings.NewReader("C0600 0 file\n"+tc.status)), writer, "file", false, func(_ SCPEntry, reader io.Reader) error {
				_, err := io.Copy(io.Discard, reader)
				return err
			}, nil)
			if files != tc.wantFile || (err == nil) != (tc.wantFile == 1) {
				t.Fatalf("completed files = %d, error = %v", files, err)
			}
			if tc.failAck != 0 && !errors.Is(err, io.ErrClosedPipe) {
				t.Fatalf("lost acknowledgement write error: %v", err)
			}
		})
	}
}

type failingSCPAckWriter struct{ calls, failAt int }

// Write fails at a selected acknowledgement without involving a network peer.
func (w *failingSCPAckWriter) Write(data []byte) (int, error) {
	w.calls++
	if w.calls == w.failAt {
		return 0, io.ErrClosedPipe
	}
	return len(data), nil
}

// TestSCPNamesAndQuoting rejects local aliases and quotes remote shell metacharacters.
func TestSCPNamesAndQuoting(t *testing.T) {
	for _, name := range []string{"..", "a/b", "a\\b", "C:x", "NUL", "con.txt", "COM1", "COM¹", "com².txt", "COM³.bin", "LPT¹", "lpt².log", "LPT³", "x.", "x ", "x\n", "a*b"} {
		if ValidFileName(name) {
			t.Errorf("accepted %q", name)
		}
	}
	quoted, err := scpPath("a b'$(touch nope)")
	if err != nil || quoted != "'./a b'\"'\"'$(touch nope)'" {
		t.Fatalf("quote=%q err=%v", quoted, err)
	}
	if _, err := scpPath("bad\npath"); err == nil {
		t.Fatal("newline accepted")
	}
	if !ValidFileName("file name.bin") {
		t.Fatal("ordinary filename rejected")
	}
}

// TestSCPDiagnosticBound ensures stderr is drained without unbounded memory.
func TestSCPDiagnosticBound(t *testing.T) {
	d := &scpDiagnostic{}
	data := bytes.Repeat([]byte("x"), 8192)
	for i := 0; i < 2; i++ {
		if n, err := d.Write(data); err != nil || n != len(data) {
			t.Fatalf("write=%d %v", n, err)
		}
	}
	if len(d.data) != 4096 {
		t.Fatalf("diagnostic size=%d", len(d.data))
	}
}
