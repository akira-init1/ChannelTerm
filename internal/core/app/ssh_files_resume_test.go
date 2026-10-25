package app

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSFTPResumeAfterInterruptedPutAndGet(t *testing.T) {
	for _, operation := range []string{"put", "get"} {
		t.Run(operation, func(t *testing.T) {
			config := fileServer(t, false)
			payload := bytes.Repeat([]byte{0, 255, 3, 7}, 40000)
			local := filepath.Join(t.TempDir(), "source.bin")
			if err := os.WriteFile(local, payload, 0600); err != nil {
				t.Fatal(err)
			}
			r := SSHFilesRequest{Config: config, Protocol: "sftp", Operation: "put", Local: local, Remote: "/data.bin"}
			if operation == "get" {
				if _, err := RunSSHFiles(context.Background(), r); err != nil {
					t.Fatal(err)
				}
				r.Operation, r.Local = "get", filepath.Join(t.TempDir(), "received.bin")
			}
			r.Resume = true
			var offset int64
			r.Progress = func(_ string, n, _ int64) error {
				if n > 0 {
					offset = n
					return context.Canceled
				}
				return nil
			}
			partial, err := RunSSHFiles(context.Background(), r)
			if !errors.Is(err, context.Canceled) || partial.Files != 0 || offset <= 0 || offset >= int64(len(payload)) {
				t.Fatalf("interrupted result=%+v offset=%d err=%v", partial, offset, err)
			}
			var reports []int64
			r.Progress = func(_ string, n, total int64) error {
				if total != int64(len(payload)) {
					t.Errorf("total=%d", total)
				}
				reports = append(reports, n)
				return nil
			}
			result, err := RunSSHFiles(context.Background(), r)
			if err != nil || result.Files != 1 || result.VerifiedFiles != 1 || result.Bytes != int64(len(payload))-offset {
				t.Fatalf("resume result=%+v err=%v", result, err)
			}
			if len(reports) == 0 || reports[0] != offset || reports[len(reports)-1] != int64(len(payload)) {
				t.Fatalf("resume reports=%v", reports)
			}
			if operation == "put" {
				r.Operation, r.Local, r.Resume, r.Progress = "get", filepath.Join(t.TempDir(), "roundtrip.bin"), false, nil
				if _, err := RunSSHFiles(context.Background(), r); err != nil {
					t.Fatal(err)
				}
			}
			got, err := os.ReadFile(r.Local)
			if err != nil || !bytes.Equal(got, payload) {
				t.Fatalf("roundtrip mismatch: %v", err)
			}
		})
	}
}

func TestSFTPResumeRefusesMismatchAndOversizeWithoutWriting(t *testing.T) {
	for _, operation := range []string{"put", "get"} {
		for _, previous := range []string{"bad", "longer than the source"} {
			t.Run(operation+"/"+previous, func(t *testing.T) {
				config := fileServer(t, false)
				local := filepath.Join(t.TempDir(), "local.bin")
				source, destination := "source", previous
				if operation == "get" {
					source, destination = previous, "source"
				}
				if err := os.WriteFile(local, []byte(destination), 0600); err != nil {
					t.Fatal(err)
				}
				r := SSHFilesRequest{Config: config, Protocol: "sftp", Operation: "put", Local: local, Remote: "/data.bin"}
				if _, err := RunSSHFiles(context.Background(), r); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(local, []byte(source), 0600); err != nil {
					t.Fatal(err)
				}
				r.Operation, r.Resume = operation, true
				result, err := RunSSHFiles(context.Background(), r)
				if err == nil || !strings.Contains(err.Error(), "resume") || result.Bytes != 0 || result.Files != 0 {
					t.Fatalf("result=%+v err=%v", result, err)
				}
				if operation == "put" {
					r.Operation, r.Local, r.Resume = "get", filepath.Join(t.TempDir(), "check.bin"), false
					if _, err := RunSSHFiles(context.Background(), r); err != nil {
						t.Fatal(err)
					}
				}
				got, err := os.ReadFile(r.Local)
				if err != nil || string(got) != previous {
					t.Fatalf("modified destination=%q err=%v", got, err)
				}
			})
		}
	}
}

func TestSFTPResumeEmptyAndAlreadyComplete(t *testing.T) {
	for _, payload := range []string{"", "already complete"} {
		config := fileServer(t, false)
		local := filepath.Join(t.TempDir(), "file")
		if err := os.WriteFile(local, []byte(payload), 0600); err != nil {
			t.Fatal(err)
		}
		r := SSHFilesRequest{Config: config, Protocol: "sftp", Operation: "put", Local: local, Remote: "/file", Resume: true}
		if _, err := RunSSHFiles(context.Background(), r); err != nil {
			t.Fatal(err)
		}
		for _, op := range []string{"put", "get"} {
			r.Operation = op
			result, err := RunSSHFiles(context.Background(), r)
			if err != nil || result.Bytes != 0 || result.Files != 1 || result.VerifiedFiles != 1 {
				t.Fatalf("%s result=%+v err=%v", op, result, err)
			}
		}
	}
}

func TestSFTPResumeRejectsUnsupportedModesBeforeConnecting(t *testing.T) {
	for _, r := range []SSHFilesRequest{
		{Protocol: "scp", Operation: "put"}, {Protocol: "sftp", Operation: "ls"},
		{Protocol: "sftp", Operation: "put", Overwrite: true},
		{Protocol: "sftp", Operation: "get", SafeReplace: true},
		{Protocol: "sftp", Operation: "put", Recursive: true},
	} {
		r.Resume = true
		if _, err := RunSSHFiles(context.Background(), r); err == nil || !strings.Contains(err.Error(), "resume requires") {
			t.Fatalf("accepted unsupported request: %v", err)
		}
	}
}
