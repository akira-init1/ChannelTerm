package app

import (
	"context"
	"errors"
	"testing"

	"github.com/akira-init1/ChannelTerm/internal/core/channel"
	"github.com/akira-init1/ChannelTerm/internal/core/session"
)

type resizeSession struct {
	*fakeConnectedSession
	cols, rows uint16
	err        error
}

// Resize records the application request and returns a simulated Channel result.
func (s *resizeSession) Resize(cols, rows uint16) error {
	s.cols, s.rows = cols, rows
	return s.err
}

// TestResizeSession delegates unchanged dimensions and lifecycle/capability
// errors without adding any SSH-library dependency to the application API.
func TestResizeSession(t *testing.T) {
	manager := session.NewManager()
	defer manager.Close()
	application, err := New(Dependencies{Manager: manager})
	if err != nil {
		t.Fatal(err)
	}
	terminal := &resizeSession{fakeConnectedSession: newFakeConnectedSession("resize-test")}
	if err := terminal.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := manager.RegisterWithMetadata(terminal, session.SessionMetadata{Transport: "ssh", Endpoint: "user@host.example:22"}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []error{nil, channel.ErrResizeUnsupported, session.ErrNotOpen} {
		terminal.err = want
		if err := application.ResizeSession("SSH-1", 132, 43); !errors.Is(err, want) {
			t.Fatalf("ResizeSession = %v; want %v", err, want)
		}
		if terminal.cols != 132 || terminal.rows != 43 {
			t.Fatalf("dimensions = %d x %d", terminal.cols, terminal.rows)
		}
	}
	if err := application.ResizeSession("missing", 132, 43); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("unknown Session = %v", err)
	}
	if err := (*Application)(nil).ResizeSession("SSH-1", 132, 43); !errors.Is(err, ErrNilApplicationManager) {
		t.Fatalf("nil Application = %v", err)
	}
}
