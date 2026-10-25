package app

import (
	"context"
	"testing"

	"github.com/akira-init1/ChannelTerm/internal/core/session"
)

func TestClientProvenanceRetainedDuringDetachedLeaseCleanup(t *testing.T) {
	manager := session.NewManager()
	defer manager.Close()
	terminal := newFakeConnectedSession("board")
	if err := terminal.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := manager.Register(terminal); err != nil {
		t.Fatal(err)
	}
	application, err := New(Dependencies{Manager: manager})
	if err != nil {
		t.Fatal(err)
	}
	client := session.ClientIdentity{ID: "CL-example", Name: "AI", Kind: "agent"}
	ctx := WithClientIdentity(context.Background(), client)
	lease, err := application.AcquireLeaseForClient(ctx, "board", "example-owner", LeaseTypeTerminal)
	if err != nil {
		t.Fatal(err)
	}
	if lease.Client != client {
		t.Fatal("lease lost source")
	}
	var observed session.WriteRequest
	terminal.onWrite = func(request session.WriteRequest) { observed = request }
	if _, err := application.WriteSessionWithLease(context.Background(), "board", "example-owner", session.WriteRequest{Actor: session.ActorSystem, Data: []byte{3}}); err != nil {
		t.Fatal(err)
	}
	if observed.Client != client || observed.Actor != session.ActorSystem {
		t.Fatal("cleanup lost initiating client or system execution label")
	}
	if err := application.ReleaseLease("board", "example-owner"); err != nil {
		t.Fatal(err)
	}
	// A later unregistered request cannot inject a client identity in the payload.
	if _, err := application.WriteSession(context.Background(), "board", session.WriteRequest{Client: client, Actor: session.ActorUser, Data: []byte("x")}); err != nil {
		t.Fatal(err)
	}
	if observed.Client.ID != "" {
		t.Fatal("payload forged authentication")
	}
	events, err := application.ReadSessionEvents(context.Background(), "board", nil, 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range events.Events {
		if event.Type == session.EventLeaseAcquired || event.Type == session.EventLeaseReleased {
			if event.Client != client {
				t.Fatal("lease event lost identity")
			}
		}
	}
}

func TestClientOperationReferencePreservesEventsAndInputTracker(t *testing.T) {
	manager := session.NewManager()
	defer manager.Close()
	terminal := newFakeConnectedSession("board")
	if err := terminal.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := manager.Register(terminal); err != nil {
		t.Fatal(err)
	}
	application, err := New(Dependencies{Manager: manager})
	if err != nil {
		t.Fatal(err)
	}
	client := session.ClientIdentity{ID: "CL-example", Name: "AI", Kind: "agent"}
	ref, err := application.sessionForClient(WithClientIdentity(context.Background(), client), "board")
	if err != nil {
		t.Fatal(err)
	}
	if err := checkDebugPendingInput(ref); err != nil {
		t.Fatalf("changed empty input semantics: %v", err)
	}
	ref.PublishEvent(session.Event{Type: session.EventOperationState, Actor: "system", Metadata: map[string]any{"state": "recovering"}})
	events, err := terminal.ReadRecentEvents(10)
	if err != nil {
		t.Fatal(err)
	}
	if events.Events[len(events.Events)-1].Client != client {
		t.Fatal("asynchronous event lost initiating client")
	}
	if _, err := terminal.Write(session.WriteRequest{Actor: session.ActorUser, Data: []byte("unfinished")}); err != nil {
		t.Fatal(err)
	}
	if err := checkDebugPendingInput(ref); err == nil {
		t.Fatal("wrapper hid pending input")
	}
}
