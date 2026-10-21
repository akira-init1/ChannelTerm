package command

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/akira-init1/ChannelTerm/internal/core/app"
)

func TestSaveDiagnosticVerifiesDownloadAndPreservesExistingFiles(t *testing.T) {
	data := []byte("fake diagnostic ZIP for download integrity test")
	sum := sha256.Sum256(data)
	meta := app.DiagnosticArchive{ID: "DIAG-fake", Size: len(data), SHA256: hex.EncodeToString(sum[:])}
	read := func(offset int) (diagnosticDownloadChunk, error) {
		end := min(offset+7, len(data))
		return diagnosticDownloadChunk{Data: base64.StdEncoding.EncodeToString(data[offset:end]), Encoding: "base64", Next: end, BytesRead: end - offset, EOF: end == len(data), Size: len(data), SHA256: meta.SHA256}, nil
	}
	path := filepath.Join(t.TempDir(), "capture.zip")
	if err := saveDiagnostic(context.Background(), path, app.DiagnosticArchive{ID: meta.ID}, read); err != nil {
		t.Fatal(err)
	}
	saved, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(saved, data) {
		t.Fatalf("saved=%q err=%v", saved, err)
	}
	if err := saveDiagnostic(context.Background(), path, meta, read); !errors.Is(err, os.ErrExist) {
		t.Fatalf("overwrite error=%v", err)
	}
}

func TestSaveDiagnosticRemovesOnlyIncompleteNewFile(t *testing.T) {
	for _, corrupt := range []bool{false, true} {
		path := filepath.Join(t.TempDir(), "capture.zip")
		err := saveDiagnostic(context.Background(), path, app.DiagnosticArchive{}, func(int) (diagnosticDownloadChunk, error) {
			if !corrupt {
				return diagnosticDownloadChunk{}, errors.New("expired")
			}
			return diagnosticDownloadChunk{Data: "YWJj", Encoding: "base64", Next: 3, BytesRead: 3, EOF: true, Size: 3, SHA256: "incorrect"}, nil
		})
		if err == nil {
			t.Fatal("accepted failed download")
		}
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("partial file retained: %v", err)
		}
	}
}

func TestDiagnosticsHelpAndValidation(t *testing.T) {
	var output bytes.Buffer
	if err := runDiagnostics(context.Background(), []string{"--help"}, &output); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(output.Bytes(), []byte("--include-raw")) {
		t.Fatal("missing raw-content opt-in help")
	}
	if err := runDiagnostics(context.Background(), []string{"--session", "SER-1"}, &output); err == nil {
		t.Fatal("accepted missing output")
	}
}
