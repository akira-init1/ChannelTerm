//go:build linux || darwin

package ssh

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestSCPUploadGuardPreservesSymlinkParentTraversal runs only the generated
// existence guard against a temporary filesystem. A stub sink never copies
// anything; reaching it would demonstrate bypass of the no-overwrite guard.
func TestSCPUploadGuardPreservesSymlinkParentTraversal(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"alias", "real/child", "real/destination", "bin"} {
		if err := os.MkdirAll(filepath.Join(root, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(root, "real/child"), filepath.Join(root, "alias/link")); err != nil {
		t.Fatal(err)
	}
	existing := filepath.Join(root, "real/destination/payload.bin")
	if err := os.WriteFile(existing, []byte("keep-existing-data"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "bin/scp"), []byte("#!/bin/sh\nprintf 'UNEXPECTED-COPY\\n'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	// filepath.Join would normalize the very dot component under test.
	destination := root + "/alias/link/../destination"
	command, err := scpUploadCommand("payload.bin", destination, false, false)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("/bin/sh", "-c", command)
	cmd.Env = append(os.Environ(), "PATH="+filepath.Join(root, "bin"))
	output, err := cmd.CombinedOutput()
	if err == nil || !bytes.Equal(output, []byte("\x01destination exists\n")) {
		t.Fatalf("guard bypassed: output=%q err=%v", output, err)
	}
	data, err := os.ReadFile(existing)
	if err != nil || string(data) != "keep-existing-data" {
		t.Fatalf("fixture changed: %q %v", data, err)
	}
}
