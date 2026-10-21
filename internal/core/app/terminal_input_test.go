package app

import (
	"context"
	"testing"

	"github.com/akira-init1/ChannelTerm/internal/core/session"
)

func newSerialTestApplication(t *testing.T) (*Application, *fakeConnectedSession) {
	t.Helper()
	terminal := newFakeConnectedSession("boot-test")
	if err := terminal.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	manager := session.NewManager()
	if err := manager.RegisterWithMetadata(terminal, session.SessionMetadata{Transport: "serial", Endpoint: "fake"}); err != nil {
		t.Fatal(err)
	}
	a, err := New(Dependencies{Manager: manager})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.Close() })
	return a, terminal
}

func TestPendingInputRecognizesOnlyCompleteCursorReplies(t *testing.T) {
	for _, tc := range []struct {
		name    string
		parts   []string
		pending bool
	}{
		{"split", []string{"\x1b", "[30;120R"}, false},
		{"complete", []string{"\x1b[24;80R"}, false},
		{"real_input", []string{"ls", "\x1b[24;80R"}, true},
		{"arrow", []string{"\x1b[A"}, true},
		{"partial", []string{"\x1b[24;"}, true},
		{"malformed", []string{"\x1b[0;80R"}, true},
		{"oversized", []string{"\x1b[99999999999;80R"}, true},
		{"reset", []string{"ls\x1b[24;80R\x03"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, s := newSerialTestApplication(t)
			for _, p := range tc.parts {
				_, _ = s.Write(session.WriteRequest{Actor: session.ActorUser, Data: []byte(p)})
			}
			pending, err := terminalHasPendingInteractiveInput(s)
			if err != nil || pending != tc.pending {
				t.Fatalf("pending=%v err=%v", pending, err)
			}
		})
	}
}
func TestPendingInputUnknownHistoryStaysConservative(t *testing.T) {
	_, s := newSerialTestApplication(t)
	for i := 0; i < session.DefaultActivityBufferCapacity+1; i++ {
		_, _ = s.Write(session.WriteRequest{Actor: session.ActorUser, Data: []byte("\x1b[24;80R")})
	}
	pending, err := terminalHasPendingInteractiveInput(s)
	if err != nil || !pending {
		t.Fatalf("pending=%v err=%v", pending, err)
	}
	_, _ = s.Write(session.WriteRequest{Actor: session.ActorUser, Data: []byte("\r")})
	pending, err = terminalHasPendingInteractiveInput(s)
	if err != nil || pending {
		t.Fatalf("reset pending=%v err=%v", pending, err)
	}
}
