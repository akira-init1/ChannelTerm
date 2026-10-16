package command

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/core/debugger"
)

func TestRSPClientForwardsOnlyBytesAndStopsOnRemoteEOF(t *testing.T) {
	e, err := debugger.ListenBridge("", "")
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	path := filepath.Join(t.TempDir(), "connection.json")
	data, _ := json.Marshal(e.Connection())
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	in, inputWriter := io.Pipe()
	defer in.Close()
	defer inputWriter.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	payload := []byte{0, 0xff, '$', '}', '#', '+', 3}
	done := make(chan error, 1)
	go func() {
		conn, err := e.Accept(ctx)
		if err != nil {
			done <- err
			return
		}
		defer conn.Close()
		buf := make([]byte, len(payload))
		_, err = io.ReadFull(conn, buf)
		if err == nil && !bytes.Equal(buf, payload) {
			err = io.ErrUnexpectedEOF
		}
		if err == nil {
			_, err = conn.Write(payload)
		}
		done <- err
	}()
	go func() { _, _ = inputWriter.Write(payload) }()
	var out bytes.Buffer
	if err := runRSPClient(ctx, []string{"--connection", path}, in, &out); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out.Bytes(), payload) {
		t.Fatalf("stdout contaminated: %x", out.Bytes())
	}
}

func TestRSPClientRejectsInvalidDescriptors(t *testing.T) {
	for _, args := range [][]string{nil, {"--unknown"}, {"--connection"}} {
		var out bytes.Buffer
		if err := runRSPClient(context.Background(), args, bytes.NewReader(nil), &out); err == nil || out.Len() != 0 {
			t.Fatalf("invalid flags contaminated stdout: %v %q", err, out.String())
		}
	}
	for _, data := range []string{`{}`, `{"token":"x","extra":true}`, `{} {}`, `null`} {
		path := filepath.Join(t.TempDir(), "connection.json")
		os.WriteFile(path, []byte(data), 0600)
		var out bytes.Buffer
		if err := runRSPClient(context.Background(), []string{"--connection", path}, bytes.NewReader(nil), &out); err == nil {
			t.Fatalf("accepted %s", data)
		}
		if out.Len() != 0 {
			t.Fatal("descriptor error written to GDB stream")
		}
	}
	if runtime.GOOS != "windows" {
		path := filepath.Join(t.TempDir(), "public.json")
		if err := os.WriteFile(path, []byte(`{}`), 0644); err != nil {
			t.Fatal(err)
		}
		if err := runRSPClient(context.Background(), []string{"--connection", path}, bytes.NewReader(nil), io.Discard); err == nil {
			t.Fatal("world-readable connection descriptor accepted")
		}
	}
}
