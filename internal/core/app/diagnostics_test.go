package app

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/core/session"
)

func diagnosticFiles(t *testing.T, a *Application, archive DiagnosticArchive) map[string][]byte {
	t.Helper()
	var buf []byte
	for offset := 0; ; {
		data, next, eof, err := a.ReadDiagnostic(context.Background(), archive.ID, offset, 37)
		if err != nil {
			t.Fatal(err)
		}
		buf = append(buf, data...)
		offset = next
		if eof {
			break
		}
	}
	sum := sha256.Sum256(buf)
	if len(buf) != archive.Size || hex.EncodeToString(sum[:]) != archive.SHA256 {
		t.Fatal("archive integrity mismatch")
	}
	zr, err := zip.NewReader(bytes.NewReader(buf), int64(len(buf)))
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{}
	for _, f := range zr.File {
		r, e := f.Open()
		if e != nil {
			t.Fatal(e)
		}
		files[f.Name], e = io.ReadAll(r)
		_ = r.Close()
		if e != nil {
			t.Fatal(e)
		}
	}
	return files
}

func TestDiagnosticSnapshotLosslessAndMetadataOptIn(t *testing.T) {
	a, s := newSerialTestApplication(t)
	a.build = BuildProvenance{Version: "test-version", Commit: "fake-commit", Time: "test-time"}
	raw := []byte("output\xff\xd3\x1b[6n\r\n")
	s.emitOutput(raw)
	_, _ = s.Write(session.WriteRequest{Actor: session.ActorUser, Data: []byte("secret-input")})
	s.PublishEvent(session.Event{Type: session.EventTerminalCommandStarted, Metadata: map[string]any{"command": "secret-command", "owner": "secret-owner", "command_id": "CMD-fake"}})
	for _, includeRaw := range []bool{false, true} {
		archive, err := a.ExportDiagnostics(context.Background(), "SER-1", includeRaw)
		if err != nil {
			t.Fatal(err)
		}
		files := diagnosticFiles(t, a, archive)
		if !includeRaw {
			if _, ok := files["terminal.bin"]; ok {
				t.Fatal("raw bytes included by default")
			}
			if bytes.Contains(files["activity.jsonl"], []byte("data_base64")) {
				t.Fatal("input payload included by default")
			}
		} else {
			decoded, e := hex.DecodeString(strings.TrimSpace(string(files["terminal.hex"])))
			if e != nil || !bytes.Equal(decoded, raw) || !bytes.Equal(files["terminal.bin"], raw) {
				t.Fatal("raw evidence changed")
			}
			if !bytes.Contains(files["terminal.log"], []byte("<0xFF@6>")) {
				t.Fatalf("derived view: %s", files["terminal.log"])
			}
		}
		if bytes.Contains(files["events.jsonl"], []byte("secret-")) {
			t.Fatal("private metadata leaked")
		}
		var manifest map[string]any
		if e := json.Unmarshal(files["manifest.json"], &manifest); e != nil {
			t.Fatal(e)
		}
		if manifest["version"] != "test-version" || manifest["commit"] != "fake-commit" {
			t.Fatalf("provenance: %v", manifest)
		}
		if _, ok := files["rx-index.jsonl"]; ok {
			t.Fatal("snapshot invented observation times")
		}
	}
}

func TestDiagnosticsExpiryEvictionBoundsAndCancellation(t *testing.T) {
	a, _ := newSerialTestApplication(t)
	first, err := a.ExportDiagnostics(context.Background(), "SER-1", false)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < diagnosticCountLimit; i++ {
		if _, err := a.ExportDiagnostics(context.Background(), "SER-1", false); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := a.DiagnosticInfo(first.ID); !errors.Is(err, ErrDiagnosticNotFound) {
		t.Fatalf("eviction error=%v", err)
	}
	latest := a.diagnostics.entries[len(a.diagnostics.entries)-1].info
	for _, tc := range [][2]int{{-1, 1}, {0, 0}, {0, 65537}, {latest.Size + 1, 1}} {
		if _, _, _, err := a.ReadDiagnostic(context.Background(), latest.ID, tc[0], tc[1]); err == nil {
			t.Fatalf("accepted invalid range %v", tc)
		}
	}
	a.diagnostics.entries[len(a.diagnostics.entries)-1].info.ExpiresAt = time.Now().Add(-time.Second)
	if _, err := a.DiagnosticInfo(latest.ID); !errors.Is(err, ErrDiagnosticNotFound) {
		t.Fatal("expired archive readable")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := a.ExportDiagnostics(ctx, "SER-1", true); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel error=%v", err)
	}
	if _, err := a.DiagnosticInfo("../../secret"); !errors.Is(err, ErrDiagnosticNotFound) {
		t.Fatal("path treated as archive")
	}
}

func TestDiagnosticCaptureBoundsAndGapMapping(t *testing.T) {
	c := &diagnosticCapture{}
	c.append(session.OutputChunk{Data: []byte("abc"), Next: 103})
	c.append(session.OutputChunk{Data: []byte("def"), Next: 203, Dropped: true})
	if len(c.segments) != 2 || c.segments[1].Start != 200 || c.segments[1].Offset != 3 || !c.segments[1].Dropped {
		t.Fatalf("segments=%+v", c.segments)
	}
	c.append(session.OutputChunk{Data: bytes.Repeat([]byte{0xff}, diagnosticRawLimit), Next: session.OutputCursor(diagnosticRawLimit + 203)})
	if len(c.data) != diagnosticRawLimit || !c.truncated {
		t.Fatal("capture unbounded or silent truncation")
	}
}
