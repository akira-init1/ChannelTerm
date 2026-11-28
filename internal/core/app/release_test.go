package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/core/session"
)

func TestReleaseMatchesHostSerialPortWithoutOpeningIt(t *testing.T) {
	m := session.NewManager()
	defer m.Close()
	a, err := New(Dependencies{Manager: m})
	if err != nil {
		t.Fatal(err)
	}
	ctx := WithClientIdentity(context.Background(), session.ClientIdentity{Kind: "user"})
	port := "COM8"
	alias := "com8"
	if runtime.GOOS != "windows" {
		dir, err := filepath.EvalSymlinks(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		port = filepath.Join(dir, "ttyUSB0")
		alias = filepath.Join(dir, "alias")
		if err := os.WriteFile(port, nil, 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(port, alias); err != nil {
			t.Fatal(err)
		}
	}
	testSession := newFakeConnectedSession("board")
	if err := testSession.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	if err := m.RegisterWithMetadata(testSession, session.SessionMetadata{Transport: "serial", Endpoint: port}); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{port, alias, "SER-1", "board"} {
		rows, err := a.ReleaseTargets(ctx, target)
		if err != nil || len(rows) != 1 || rows[0].ID != "board" {
			t.Fatalf("lookup %q: %+v %v", target, rows, err)
		}
	}
	if runtime.GOOS != "windows" {
		rows, err := a.ReleaseTargets(ctx, filepath.Join(filepath.Dir(port), "TTYUSB0"))
		if err != nil || len(rows) != 0 {
			t.Fatalf("case mismatch: %v %v", rows, err)
		}
	}
}

func TestReleaseInterruptsBlockedWriteAndRejectsStaleRegistration(t *testing.T) {
	m := session.NewManager()
	defer m.Close()
	a, err := New(Dependencies{Manager: m})
	if err != nil {
		t.Fatal(err)
	}
	ctx := WithClientAccess(WithClientIdentity(context.Background(), session.ClientIdentity{ID: "operator", Kind: "user"}), ClientAccess{Manage: true})
	original := newBlockingWriteSession("board")
	if err := original.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	metadata := session.SessionMetadata{Transport: "serial", Endpoint: "COM8"}
	if err := m.RegisterWithMetadata(original, metadata); err != nil {
		t.Fatal(err)
	}
	targets, err := a.ReleaseTargets(ctx, "COM8")
	if err != nil || len(targets) != 1 {
		t.Fatalf("preview: %+v %v", targets, err)
	}
	row := targets[0]
	agent := WithClientIdentity(ctx, session.ClientIdentity{ID: "agent", Kind: "agent"})
	for _, denied := range []context.Context{agent, WithClientAccess(ctx, ClientAccess{Write: true})} {
		if err := a.ReleaseRegistration(denied, row.ID, row.Reference); !errors.Is(err, ErrClientPermissionDenied) {
			t.Fatalf("unauthorized release: %v", err)
		}
	}
	if _, err := a.AcquireLease(row.ID, "worker", LeaseTypeFileTransfer); err != nil {
		t.Fatal(err)
	}
	wrote := make(chan error, 1)
	go func() {
		_, err := a.WriteSessionWithLease(context.Background(), row.ID, "worker", session.WriteRequest{Actor: session.ActorSystem, Data: []byte("test")})
		wrote <- err
	}()
	select {
	case <-original.writeStarted:
	case <-time.After(time.Second):
		t.Fatal("worker did not start")
	}
	released := make(chan error, 1)
	go func() { released <- a.ReleaseRegistration(ctx, row.ID, row.Reference) }()
	select {
	case err := <-released:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("release blocked behind write")
	}
	if err := <-wrote; !errors.Is(err, session.ErrNotOpen) {
		t.Fatal(err)
	}
	next := newFakeConnectedSession(row.ID)
	if err := next.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	if err := m.RegisterWithMetadata(next, metadata); err != nil {
		t.Fatal(err)
	}
	if err := a.ReleaseRegistration(ctx, row.ID, row.Reference); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("stale close: %v", err)
	}
	// A debugger can discover its broken stream after the endpoint reconnects.
	run := &debugRun{app: a, terminal: original, reference: row.Reference, touched: true, state: "running", changed: make(chan struct{}), finished: make(chan struct{})}
	run.fail(errors.New("old bridge disconnected"))
	if current, ok := m.Get(row.ID); !ok || current != next {
		t.Fatal("old debugger removed replacement registration")
	}
	if next.State() != session.StateOpen {
		t.Fatal("replacement closed")
	}
}
