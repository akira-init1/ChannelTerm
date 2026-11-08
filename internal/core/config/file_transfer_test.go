package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestFileTransferConfigurationRoundTripAndDefaultDisabled(t *testing.T) {
	file := File{FileTransfer: FileTransfer{Root: t.TempDir(), SSH: map[string]FileTransferSSH{"board": {User: "fixture", Host: "192.0.2.1", Port: 2222, PrivateKey: "/fixture/key", KnownHosts: "/fixture/known_hosts"}}}}
	p := filepath.Join(t.TempDir(), "config.toml")
	if err := Save(p, file); err != nil {
		t.Fatal(err)
	}
	got, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.FileTransfer, file.FileTransfer) {
		t.Fatalf("%+v", got.FileTransfer)
	}
	if err := Save(p, File{}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "file_transfer") {
		t.Fatalf("disabled section persisted: %s", b)
	}
	got, err = Load(p)
	if err != nil || got.FileTransfer.Root != "" {
		t.Fatalf("%+v %v", got.FileTransfer, err)
	}
}
