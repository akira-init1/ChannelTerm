package ssh

import (
	"bufio"
	"bytes"
	"context"
	"errors"
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
		err := receiveSCP(bufio.NewReaderSize(strings.NewReader(input), 4096), io.Discard, "file", false, func(entry SCPEntry, r io.Reader) error {
			if entry.Path != "file" {
				t.Fatalf("unsafe name reached receiver: %q", entry.Path)
			}
			if r != nil {
				_, err := io.Copy(io.Discard, r)
				return err
			}
			return nil
		})
		if err == nil {
			t.Errorf("accepted malformed record %q", input[:min(len(input), 100)])
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
	} else if behavior == "scp-upload" {
		_, _ = ch.Write([]byte{0})
		line, err := reader.ReadString('\n')
		if err != nil || line != "C0644 3 fixture.bin\n" {
			t.Errorf("fixture header=%q %v", line, err)
			return
		}
		_, _ = ch.Write([]byte{0})
		payload := make([]byte, 4)
		if _, err := io.ReadFull(reader, payload); err != nil || !bytes.Equal(payload, []byte{'A', 0, 'B', 0}) {
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
	for _, mode := range []string{"scp-upload", "scp-download"} {
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
			if mode == "scp-upload" {
				source := fstest.MapFS{"fixture.bin": &fstest.MapFile{Data: []byte{'A', 0, 'B'}, Mode: 0644}}
				files, n, err := client.UploadSCP(source, "fixture.bin", "/tmp/fixture.bin", false, false, nil)
				if err != nil || files != 1 || n != 3 {
					t.Fatalf("upload=%d %d %v", files, n, err)
				}
			} else {
				var output bytes.Buffer
				err := client.DownloadSCP("/tmp/fixture.bin", false, func(entry SCPEntry, r io.Reader) error { _, err := io.Copy(&output, r); return err })
				if err != nil || !bytes.Equal(output.Bytes(), []byte{'A', 0, 'B'}) {
					t.Fatalf("download=%v %v", output.Bytes(), err)
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
		done <- client.DownloadSCP("fixture.bin", false, func(SCPEntry, io.Reader) error { return nil })
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
	err := receiveSCP(bufio.NewReader(strings.NewReader(wire)), &acks, "tree", true, func(entry SCPEntry, r io.Reader) error {
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
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(paths, ",") != "tree,tree/data.bin,tree/empty" || acks.Len() != 8 {
		t.Fatalf("paths=%v acks=%d", paths, acks.Len())
	}
	duplicate := "D0755 0 tree\nC0644 0 x\n\x00C0644 0 x\n\x00E\n"
	if err := receiveSCP(bufio.NewReader(strings.NewReader(duplicate)), io.Discard, "tree", true, func(SCPEntry, io.Reader) error { return nil }); err == nil {
		t.Fatal("duplicate accepted")
	}
}

// TestSCPNamesAndQuoting rejects local aliases and quotes remote shell metacharacters.
func TestSCPNamesAndQuoting(t *testing.T) {
	for _, name := range []string{"..", "a/b", "a\\b", "C:x", "NUL", "con.txt", "COM1", "x.", "x ", "x\n", "a*b"} {
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
