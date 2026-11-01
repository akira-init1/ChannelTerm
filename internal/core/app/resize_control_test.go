package app

import (
	"context"
	"errors"
	"testing"

	"github.com/akira-init1/ChannelTerm/internal/core/channel"
	"github.com/akira-init1/ChannelTerm/internal/core/session"
)

func TestResizeControlCreationTakeoverReleaseAndRegistration(t *testing.T) {
	m := session.NewManager()
	defer m.Close()
	a, _ := New(Dependencies{Manager: m})
	terminal := &resizeSession{fakeConnectedSession: newFakeConnectedSession("ssh-size")}
	if err := terminal.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := m.RegisterWithMetadata(terminal, session.SessionMetadata{Transport: "ssh"}); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	first, err := a.takeResizeControl(ctx, terminal.ID(), true)
	if err != nil || len(first) != 64 {
		t.Fatalf("initial control: %q %v", first, err)
	}
	if err := a.ResizeSessionOwned(ctx, terminal.ID(), "", 100, 40); !errors.Is(err, ErrResizeNotOwned) {
		t.Fatal(err)
	}
	if err := a.ResizeSessionOwned(ctx, terminal.ID(), first, 100, 40); err != nil {
		t.Fatal(err)
	}
	second, err := a.TakeResizeControl(ctx, terminal.ID())
	if err != nil || second == first {
		t.Fatal("takeover", err)
	}
	if err := a.ResizeSessionOwned(ctx, terminal.ID(), first, 80, 24); !errors.Is(err, ErrResizeNotOwned) {
		t.Fatal("stale owner", err)
	}
	if err := a.ReleaseResizeControl(terminal.ID(), first); err != nil {
		t.Fatal(err)
	}
	if owned, _ := a.OwnsResizeControl(terminal.ID(), second); !owned {
		t.Fatal("stale release revoked successor")
	}
	for range 2 {
		if err := a.ReleaseResizeControl(terminal.ID(), second); err != nil {
			t.Fatal(err)
		}
	}
	if late, err := a.takeResizeControl(ctx, terminal.ID(), true); err != nil || late != "" {
		t.Fatal("late creator reclaimed released control", err)
	}
	if err := a.ResizeSessionOwned(ctx, terminal.ID(), second, 90, 30); !errors.Is(err, ErrResizeNotOwned) {
		t.Fatal(err)
	}
	if _, err := a.TakeResizeControl(WithClientAccess(ctx, ClientAccess{Write: true}), terminal.ID()); !errors.Is(err, ErrClientPermissionDenied) {
		t.Fatal("agent takeover", err)
	}
	third, _ := a.TakeResizeControl(ctx, terminal.ID())
	if err := a.ResizeSessionOwned(WithClientAccess(ctx, ClientAccess{}), terminal.ID(), third, 90, 30); !errors.Is(err, ErrClientPermissionDenied) {
		t.Fatal(err)
	}
	if err := a.ResizeSessionOwned(ctx, terminal.ID(), third, 0, 30); !errors.Is(err, ErrResizeDimensions) {
		t.Fatal(err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if err := a.ResizeSessionOwned(cancelled, terminal.ID(), third, 90, 30); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := a.CloseSession(terminal.ID()); err != nil {
		t.Fatal(err)
	}
	replacement := &resizeSession{fakeConnectedSession: newFakeConnectedSession(terminal.ID())}
	_ = replacement.Connect(ctx)
	_ = m.RegisterWithMetadata(replacement, session.SessionMetadata{Transport: "ssh"})
	if err := a.ResizeSessionOwned(ctx, replacement.ID(), third, 90, 30); !errors.Is(err, ErrResizeNotOwned) {
		t.Fatal("reused ID accepted old authority", err)
	}
	serial := newFakeConnectedSession("serial-size")
	_ = serial.Connect(ctx)
	_ = m.RegisterWithMetadata(serial, session.SessionMetadata{Transport: "serial"})
	if _, err := a.TakeResizeControl(ctx, serial.ID()); !errors.Is(err, channel.ErrResizeUnsupported) {
		t.Fatal(err)
	}
}

type blockedResizeSession struct {
	*fakeConnectedSession
	started chan struct{}
	finish  chan struct{}
}

func (s *blockedResizeSession) Resize(uint16, uint16) error { close(s.started); <-s.finish; return nil }

func TestResizeControlDoesNotQueueTakeoverOrBlockOtherSessions(t *testing.T) {
	ctx := context.Background()
	m := session.NewManager()
	defer m.Close()
	a, _ := New(Dependencies{Manager: m})
	s := &blockedResizeSession{newFakeConnectedSession("blocked-size"), make(chan struct{}), make(chan struct{})}
	_ = s.Connect(ctx)
	_ = m.RegisterWithMetadata(s, session.SessionMetadata{Transport: "ssh"})
	owner, _ := a.TakeResizeControl(ctx, s.ID())
	done := make(chan error, 1)
	go func() { done <- a.ResizeSessionOwned(ctx, s.ID(), owner, 100, 40) }()
	<-s.started
	if _, err := a.TakeResizeControl(ctx, s.ID()); !errors.Is(err, ErrSessionBusy) {
		t.Error("takeover must not queue", err)
	}
	other := &resizeSession{fakeConnectedSession: newFakeConnectedSession("other-size")}
	_ = other.Connect(ctx)
	_ = m.RegisterWithMetadata(other, session.SessionMetadata{Transport: "ssh"})
	next, err := a.TakeResizeControl(ctx, other.ID())
	if err != nil {
		t.Error(err)
	}
	if err := a.ResizeSessionOwned(ctx, other.ID(), next, 80, 24); err != nil {
		t.Error(err)
	}
	close(s.finish)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if _, err := a.TakeResizeControl(ctx, s.ID()); err != nil {
		t.Fatal(err)
	}
}
