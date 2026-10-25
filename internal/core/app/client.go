package app

import (
	"context"

	"github.com/akira-init1/ChannelTerm/internal/core/session"
)

type clientContextKey struct{}

// WithClientIdentity binds an adapter-authenticated registration to a request.
// Only trusted adapters may supply it; protocol arguments are never identities.
// It grants no lease or write permission. Derived contexts preserve provenance.
func WithClientIdentity(ctx context.Context, client session.ClientIdentity) context.Context {
	return context.WithValue(ctx, clientContextKey{}, client)
}

// ClientIdentityFromContext returns the public authenticated source, or an empty
// identity for local/unregistered callers. An actor label is not authentication.
func ClientIdentityFromContext(ctx context.Context) session.ClientIdentity {
	client, _ := ctx.Value(clientContextKey{}).(session.ClientIdentity)
	return client
}

// clientActor prevents registered callers from relabelling themselves through
// actor fields. Internal system writes retain their execution label; Client still
// identifies the original caller independently of the executor.
func clientActor(ctx context.Context, actor session.Actor) session.Actor {
	client := ClientIdentityFromContext(ctx)
	if client.ID != "" && actor != session.ActorSystem {
		return session.Actor(client.Kind)
	}
	return actor
}

// sessionForClient decorates an existing Session reference with operation
// provenance. It owns no stream, buffer, lifecycle or authorization state. The
// operation retains this reference through asynchronous events and recovery.
func (a *Application) sessionForClient(ctx context.Context, identifier string) (session.Session, error) {
	terminal, err := a.session(identifier)
	client := ClientIdentityFromContext(ctx)
	if err != nil || client.ID == "" {
		return terminal, err
	}
	decorated := &clientSession{Session: terminal, client: client}
	if tracker, ok := terminal.(interface{ HasPendingInput() bool }); ok {
		return &trackedClientSession{clientSession: decorated, tracker: tracker}, nil
	}
	return decorated, nil
}

type clientSession struct {
	session.Session
	client session.ClientIdentity
}

func (s *clientSession) PublishEvent(event session.Event) {
	event.Client = s.client
	s.Session.PublishEvent(event)
}

func (s *clientSession) Write(request session.WriteRequest) (int, error) {
	request.Client = s.client
	return s.Session.Write(request)
}

// trackedClientSession preserves the optional tracker only when supplied by the
// original Session. Other implementations retain their existing error/fallback
// semantics instead of accidentally acquiring this optional capability.
type trackedClientSession struct {
	*clientSession
	tracker interface{ HasPendingInput() bool }
}

func (s *trackedClientSession) HasPendingInput() bool { return s.tracker.HasPendingInput() }
