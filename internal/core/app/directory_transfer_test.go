package app

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestDirectoryPlanStreamsNestedFilesIncludingDotfiles(t *testing.T) {
	root := t.TempDir()
	mustWriteDirectoryTestFile(t, filepath.Join(root, ".env"), []byte("MODE=test\n"))
	mustWriteDirectoryTestFile(t, filepath.Join(root, "nested", "file without extension"), []byte("payload"))
	plan, err := makeDirectoryPlan(root)
	if err != nil {
		t.Fatalf("makeDirectoryPlan() error = %v", err)
	}
	data, err := io.ReadAll(plan.stream())
	if err != nil {
		t.Fatalf("read tar stream: %v", err)
	}
	if int64(len(data)) != plan.size {
		t.Errorf("tar stream size = %d, want counted %d", len(data), plan.size)
	}
	entries := readDirectoryTestTar(t, data)
	if got := string(entries[".env"]); got != "MODE=test\n" {
		t.Errorf("dotfile = %q", got)
	}
	if got := string(entries["nested/file without extension"]); got != "payload" {
		t.Errorf("nested file = %q", got)
	}
}

func TestDirectoryPlanAllowsEmptyDirectory(t *testing.T) {
	plan, err := makeDirectoryPlan(t.TempDir())
	if err != nil {
		t.Fatalf("makeDirectoryPlan() error = %v", err)
	}
	if plan.size == 0 {
		t.Fatal("empty directory tar size is zero, want tar end blocks")
	}
	data, err := io.ReadAll(plan.stream())
	if err != nil {
		t.Fatal(err)
	}
	if len(readDirectoryTestTar(t, data)) != 0 {
		t.Error("empty directory tar has entries")
	}
}

func TestSendDirectoryStreamsTarInBoundedPayloads(t *testing.T) {
	source := t.TempDir()
	mustWriteDirectoryTestFile(t, filepath.Join(source, "nested", "large.bin"), bytes.Repeat([]byte("x"), FileTransferChunkSize*3+19))
	terminal := newFileTransferTestSession(nil)
	result, err := SendDirectory(context.Background(), terminal, source, "/tmp/release", nil)
	if err != nil {
		t.Fatalf("SendDirectory() error = %v", err)
	}
	if result.RemotePath != "/tmp/release" || result.Size != int64(len(terminal.received)) {
		t.Errorf("SendDirectory() result = %+v, received %d bytes", result, len(terminal.received))
	}
	if terminal.maxPayload > FileTransferChunkSize {
		t.Errorf("largest Session payload = %d, want <= %d", terminal.maxPayload, FileTransferChunkSize)
	}
	if got := string(readDirectoryTestTar(t, terminal.received)["nested/large.bin"]); len(got) != FileTransferChunkSize*3+19 {
		t.Errorf("nested file length = %d", len(got))
	}
}

func TestReceiveDirectoryExtractsTarStream(t *testing.T) {
	source := t.TempDir()
	mustWriteDirectoryTestFile(t, filepath.Join(source, "logs", "app log"), []byte("entry"))
	plan, err := makeDirectoryPlan(source)
	if err != nil {
		t.Fatal(err)
	}
	archive, err := io.ReadAll(plan.stream())
	if err != nil {
		t.Fatal(err)
	}
	terminal := newFileTransferTestSession(archive)
	destination := t.TempDir()
	result, err := ReceiveDirectory(context.Background(), terminal, "/var/log/myapp", destination, nil)
	if err != nil {
		t.Fatalf("ReceiveDirectory() error = %v", err)
	}
	if result.Size != int64(len(archive)) {
		t.Errorf("ReceiveDirectory() size = %d, want %d", result.Size, len(archive))
	}
	data, err := os.ReadFile(filepath.Join(destination, "logs", "app log"))
	if err != nil || string(data) != "entry" {
		t.Fatalf("received directory file = %q, %v", data, err)
	}
}

func TestSendDirectoryReportsUnavailableRemoteTar(t *testing.T) {
	source := t.TempDir()
	mustWriteDirectoryTestFile(t, filepath.Join(source, "file"), []byte("content"))
	terminal := newFileTransferTestSession(nil)
	terminal.failTar = true
	_, err := SendDirectory(context.Background(), terminal, source, "/tmp/release", nil)
	if !errors.Is(err, ErrDirectoryTarUnavailable) {
		t.Fatalf("SendDirectory() error = %v, want ErrDirectoryTarUnavailable", err)
	}
}

func TestDirectoryPlanRejectsSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating symlinks may require Windows developer privileges")
	}
	root := t.TempDir()
	if err := os.Symlink("missing", filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	_, err := PlanDirectory(root)
	if !errors.Is(err, ErrUnsupportedDirectoryEntry) {
		t.Fatalf("PlanDirectory() error = %v, want ErrUnsupportedDirectoryEntry", err)
	}
}

func TestExtractDirectoryTarRejectsEscapingAndSpecialEntries(t *testing.T) {
	tests := []struct {
		name string
		head tar.Header
		want error
	}{
		{name: "parent", head: tar.Header{Name: "../escaped", Mode: 0o644, Size: 1, Typeflag: tar.TypeReg}, want: ErrUnsafeTarPath},
		{name: "absolute", head: tar.Header{Name: "/escaped", Mode: 0o644, Size: 1, Typeflag: tar.TypeReg}, want: ErrUnsafeTarPath},
		{name: "symlink", head: tar.Header{Name: "link", Typeflag: tar.TypeSymlink, Linkname: "outside"}, want: ErrUnsupportedDirectoryEntry},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var archive bytes.Buffer
			writer := tar.NewWriter(&archive)
			if err := writer.WriteHeader(&tt.head); err != nil {
				t.Fatal(err)
			}
			if tt.head.Size > 0 {
				if _, err := writer.Write([]byte("x")); err != nil {
					t.Fatal(err)
				}
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			err := extractDirectoryTar(bytes.NewReader(archive.Bytes()), t.TempDir())
			if !errors.Is(err, tt.want) {
				t.Fatalf("extractDirectoryTar() error = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestExtractDirectoryTarWritesOnlyInsideStaging(t *testing.T) {
	var archive bytes.Buffer
	writer := tar.NewWriter(&archive)
	for _, header := range []tar.Header{
		{Name: "logs", Mode: 0o755, Typeflag: tar.TypeDir},
		{Name: "logs/app log", Mode: 0o640, Size: int64(len("ok")), Typeflag: tar.TypeReg},
	} {
		if err := writer.WriteHeader(&header); err != nil {
			t.Fatal(err)
		}
		if header.Size > 0 {
			if _, err := writer.Write([]byte("ok")); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	destination := t.TempDir()
	if err := extractDirectoryTar(bytes.NewReader(archive.Bytes()), destination); err != nil {
		t.Fatalf("extractDirectoryTar() error = %v", err)
	}
	data, err := os.ReadFile(filepath.Join(destination, "logs", "app log"))
	if err != nil || string(data) != "ok" {
		t.Fatalf("extracted file = %q, %v", data, err)
	}
}

func mustWriteDirectoryTestFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func readDirectoryTestTar(t *testing.T, data []byte) map[string][]byte {
	t.Helper()
	reader := tar.NewReader(bytes.NewReader(data))
	entries := map[string][]byte{}
	for {
		head, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return entries
		}
		if err != nil {
			t.Fatal(err)
		}
		if head.Typeflag == tar.TypeReg {
			body, err := io.ReadAll(reader)
			if err != nil {
				t.Fatal(err)
			}
			entries[head.Name] = body
		}
	}
}
