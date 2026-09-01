package session

import (
	"context"
	"sync"
	"testing"
)

func TestManagerRegisterGetRemove(t *testing.T) {
	manager := NewManager()
	s, err := New("board-1", newFakeTransport())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := manager.Register(s); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if _, ok := manager.Get("board-1"); !ok {
		t.Error("Get() did not return the registered session")
	}
	if err := manager.Register(s); err != ErrDuplicateSession {
		t.Errorf("duplicate Register() error = %v, want ErrDuplicateSession", err)
	}
	if _, ok := manager.Remove("board-1"); !ok {
		t.Error("Remove() did not return the registered session")
	}
	if _, ok := manager.Get("board-1"); ok {
		t.Error("Get() returned a removed session")
	}
}

func TestManagerListReturnsSessionSnapshot(t *testing.T) {
	manager := NewManager()
	first, err := New("board-1", newFakeTransport())
	if err != nil {
		t.Fatalf("New() first error = %v", err)
	}
	second, err := New("board-2", newFakeTransport())
	if err != nil {
		t.Fatalf("New() second error = %v", err)
	}
	if err := manager.Register(first); err != nil {
		t.Fatalf("Register() first error = %v", err)
	}
	if err := manager.Register(second); err != nil {
		t.Fatalf("Register() second error = %v", err)
	}

	listed := manager.List()
	if len(listed) != 2 {
		t.Fatalf("List() returned %d sessions, want 2", len(listed))
	}
	if _, ok := manager.Remove("board-1"); !ok {
		t.Fatal("Remove() did not return the registered session")
	}
	if len(listed) != 2 {
		t.Errorf("List() snapshot length = %d after Remove(), want 2", len(listed))
	}
}

func TestManagerListInfoReturnsFixedMetadata(t *testing.T) {
	manager := NewManager()
	terminal, err := New("board-1", newFakeTransport())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	metadata := SessionMetadata{Transport: "serial", Endpoint: "COM8", Label: "imx6ull-left"}
	if err := manager.RegisterWithMetadata(terminal, metadata); err != nil {
		t.Fatalf("RegisterWithMetadata() error = %v", err)
	}

	infos := manager.ListInfo()
	if len(infos) != 1 {
		t.Fatalf("ListInfo() returned %d entries, want 1", len(infos))
	}
	metadata.Reference = "SER-1"
	if got := infos[0]; got.ID != "board-1" || got.Metadata != metadata || got.State != StateNew {
		t.Errorf("ListInfo() = %+v, want ID, metadata, and new state", got)
	}

	if err := manager.RegisterWithMetadata(terminal, SessionMetadata{Label: "duplicate"}); err != ErrDuplicateSession {
		t.Errorf("duplicate RegisterWithMetadata() error = %v, want ErrDuplicateSession", err)
	}
}

func TestManagerReferencesUseTransportPrefixesAndAreNotReused(t *testing.T) {
	manager := NewManager()
	for _, entry := range []struct {
		id        string
		transport string
		wantRef   string
	}{
		{id: "serial-one", transport: "serial", wantRef: "SER-1"},
		{id: "ssh-one", transport: "ssh", wantRef: "SSH-1"},
		{id: "telnet-one", transport: "telnet", wantRef: "TELNET-1"},
	} {
		terminal, err := New(entry.id, newFakeTransport())
		if err != nil {
			t.Fatalf("New(%q) error = %v", entry.id, err)
		}
		if err := manager.RegisterWithMetadata(terminal, SessionMetadata{Transport: entry.transport}); err != nil {
			t.Fatalf("RegisterWithMetadata(%q) error = %v", entry.id, err)
		}
		if got, ok := manager.Reference(entry.id); !ok || got != entry.wantRef {
			t.Errorf("Reference(%q) = %q, %t; want %q, true", entry.id, got, ok, entry.wantRef)
		}
		if got, ok := manager.Get(entry.wantRef); !ok || got.ID() != entry.id {
			t.Errorf("Get(%q) = %v, %t; want %q, true", entry.wantRef, got, ok, entry.id)
		}
	}
	if _, ok := manager.Remove("SER-1"); !ok {
		t.Fatal("Remove(SER-1) did not resolve the short reference")
	}
	terminal, err := New("serial-two", newFakeTransport())
	if err != nil {
		t.Fatalf("New(serial-two) error = %v", err)
	}
	if err := manager.RegisterWithMetadata(terminal, SessionMetadata{Transport: "serial"}); err != nil {
		t.Fatalf("RegisterWithMetadata(serial-two) error = %v", err)
	}
	if got, ok := manager.Reference("serial-two"); !ok || got != "SER-2" {
		t.Errorf("Reference(serial-two) = %q, %t; want SER-2, true", got, ok)
	}
}

func TestManagerCloseClosesRegisteredSessions(t *testing.T) {
	manager := NewManager()
	terminal := newFakeTransport()
	s, err := New("board-1", terminal)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := s.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	if err := manager.Register(s); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if err := manager.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if terminal.closeCount() != 1 {
		t.Errorf("transport close calls = %d, want 1", terminal.closeCount())
	}
	if _, ok := manager.Get("board-1"); ok {
		t.Error("Get() returned a session after manager Close()")
	}
}

func TestManagerGetOrCreateSharesOneEndpointSession(t *testing.T) {
	manager := NewManager()
	metadata := SessionMetadata{Transport: "serial", Endpoint: "COM8"}
	var createdCount int
	var createdMu sync.Mutex
	results := make(chan struct {
		info    SessionInfo
		created bool
		err     error
	}, 2)
	for range 2 {
		go func() {
			info, created, err := manager.GetOrCreate(context.Background(), metadata, func() (Session, error) {
				createdMu.Lock()
				createdCount++
				createdMu.Unlock()
				return New("shared-board", newFakeTransport())
			})
			results <- struct {
				info    SessionInfo
				created bool
				err     error
			}{info: info, created: created, err: err}
		}()
	}
	first := <-results
	second := <-results
	if first.err != nil || second.err != nil {
		t.Fatalf("GetOrCreate() errors = %v, %v", first.err, second.err)
	}
	createdMu.Lock()
	count := createdCount
	createdMu.Unlock()
	if count != 1 {
		t.Errorf("create calls = %d, want 1", count)
	}
	if first.info.ID != "shared-board" || second.info.ID != "shared-board" {
		t.Errorf("session IDs = %q, %q, want shared-board", first.info.ID, second.info.ID)
	}
	if first.created == second.created {
		t.Errorf("created results = %t, %t, want exactly one creator", first.created, second.created)
	}
}
