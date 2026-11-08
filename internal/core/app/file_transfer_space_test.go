package app

import (
	"context"
	"errors"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestTransferSpaceEstimatesAndRefusal(t *testing.T) {
	storage := transferStorage{available: 2 * 1024 * 1024, block: 4096, inodes: 10}
	for _, tt := range []struct {
		name    string
		f       transferFootprint
		storage transferStorage
		want    error
	}{
		{"enough", transferFootprint{bytes: 100, entries: 2}, storage, nil},
		{"bytes", transferFootprint{bytes: 2 * 1024 * 1024, entries: 2}, storage, ErrTransferSpaceInsufficient},
		{"inodes", transferFootprint{entries: 11}, storage, ErrTransferSpaceInsufficient},
		{"unknown inodes", transferFootprint{entries: 11}, transferStorage{available: 2 * 1024 * 1024, block: 4096, inodes: -1}, nil},
		{"overflow", transferFootprint{bytes: math.MaxInt64, entries: 1}, storage, ErrTransferSpaceUnavailable},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := checkTransferStorage("/data/destination", tt.f, tt.storage)
			if !errors.Is(err, tt.want) {
				t.Fatalf("error=%v, want %v", err, tt.want)
			}
		})
	}
}

func TestTransferSpaceRejectsMalformedPeerStats(t *testing.T) {
	for _, value := range []string{"UNKNOWN", "1:0:2:3", "-1:4096:2:3", "9223372036854775807:4096:2:3", "1:4096:bad:4", "1:4096:1:4:extra"} {
		if _, err := parseTransferStorage(strings.Split(value, ":")); !errors.Is(err, ErrTransferSpaceUnavailable) {
			t.Errorf("%q: %v", value, err)
		}
	}
	if s, err := parseTransferStorage([]string{"100", "4096", "-1", "0"}); err != nil || s.inodes != -1 {
		t.Fatalf("unlimited inodes: %+v %v", s, err)
	}
}

func TestDirectorySpacePreflightAndBeforeExtraction(t *testing.T) {
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "data.bin"), []byte("payload"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name      string
		responses []string
		want      error
		payload   bool
	}{
		{"enough", nil, nil, true},
		{"insufficient", []string{"0:4096:100:1000"}, ErrTransferSpaceInsufficient, false},
		{"unknown", []string{"UNKNOWN"}, ErrTransferSpaceUnavailable, false},
		{"space consumed before extraction", []string{"1000000:4096:100:1000", "0:4096:100:1000"}, ErrTransferSpaceInsufficient, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			peer := newFileTransferTestSession(nil)
			peer.spaceResponses = tt.responses
			_, err := SendDirectory(context.Background(), peer, source, "/data/out", nil)
			if !errors.Is(err, tt.want) {
				t.Fatalf("error=%v, want %v", err, tt.want)
			}
			if (len(peer.received) > 0) != tt.payload {
				t.Fatalf("payload received=%d", len(peer.received))
			}
			if peer.shellActive {
				t.Fatal("transfer shell was not restored")
			}
		})
	}
}

func TestLocalTransferSpaceFindsDestinationVolume(t *testing.T) {
	root := t.TempDir()
	file, err := os.CreateTemp(root, "destination-")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	for _, path := range []string{file.Name(), filepath.Join(root, "missing", "child")} {
		storage, err := localTransferStorage(path)
		if err != nil || storage.block <= 0 || storage.volume == "" {
			t.Fatalf("%q: %+v %v", path, storage, err)
		}
	}
	storage, err := localTransferStorage(root)
	if err != nil {
		t.Fatal(err)
	}
	// Each half fits individually, but their simultaneous allocation cannot.
	half := transferFootprint{bytes: storage.available/2 + 1}
	err = checkLocalTransferAllocations([]string{root, filepath.Join(root, "other")}, []transferFootprint{half, half})
	if !errors.Is(err, ErrTransferSpaceInsufficient) {
		t.Fatalf("same-volume check: %v", err)
	}
}

func TestTransferSpaceShellCommands(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux target shell tools")
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a '\n file"), []byte("payload"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "empty"), 0700); err != nil {
		t.Fatal(err)
	}
	quoted, _ := quoteRemotePath(root)
	out, err := exec.Command("sh", "-c", remoteDirectoryFootprintCommand("test", quoted)).CombinedOutput()
	if err != nil || !strings.Contains(string(out), "FOOTPRINT:7:3") {
		t.Fatalf("scan: %s %v", out, err)
	}
	quoted, _ = quoteRemotePath(filepath.Join(root, "not-created", "destination"))
	out, err = exec.Command("sh", "-c", remoteTransferSpaceCommand("test", quoted)).CombinedOutput()
	if err != nil {
		t.Fatalf("space query: %s %v", out, err)
	}
	fields := strings.Split(strings.TrimSpace(string(out)), ":")
	if len(fields) != 7 {
		t.Fatalf("space response: %s", out)
	}
	if _, err := parseTransferStorage(fields[3:]); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("missing", filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	quoted, _ = quoteRemotePath(root)
	out, err = exec.Command("sh", "-c", remoteDirectoryFootprintCommand("test", quoted)).CombinedOutput()
	if err != nil || !strings.Contains(string(out), "ERROR:directory-size-unavailable") {
		t.Fatalf("link refusal: %s %v", out, err)
	}
}
