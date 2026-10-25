package app

import (
	"context"
	"errors"
)

// ErrClientPermissionDenied reports a rejected request before its side effects.
// It does not imply cancellation or rollback of earlier accepted operations.
var ErrClientPermissionDenied = errors.New("client permission denied; ask the local operator to authorize this credential")

// ClientAccess is trusted adapter authorization for one accepted HTTP request.
// The adapter reloads grants for every request. Missing access denotes an
// in-process/stdio caller under the local account's existing trust boundary.
type ClientAccess struct{ Write, Manage bool }

// ClientEffect classifies a use case's minimum authorization independently of
// protocol annotations and actor/source labels.
type ClientEffect uint8

const (
	// ClientRead covers observations and attachment bookkeeping without device
	// writes, lease changes or shared-resource management.
	ClientRead ClientEffect = iota
	// ClientWrite covers device operations and the caller's capability-bound leases.
	ClientWrite
	// ClientManage covers actions affecting other clients or Host configuration.
	ClientManage
)

type clientAccessKey struct{}

// WithClientAccess binds an authenticated request's grants. External arguments
// must never be used to construct these grants.
func WithClientAccess(ctx context.Context, access ClientAccess) context.Context {
	return context.WithValue(ctx, clientAccessKey{}, access)
}

// CheckClientAccess rejects insufficient grants before a use case begins. It is
// deliberately separate from lease ownership and in-flight cancellation.
func CheckClientAccess(ctx context.Context, effect ClientEffect) error {
	access, bound := ctx.Value(clientAccessKey{}).(ClientAccess)
	if !bound {
		return nil
	}
	switch effect {
	case ClientRead:
		return nil
	case ClientWrite:
		if access.Write {
			return nil
		}
	case ClientManage:
		if access.Manage {
			return nil
		}
	}
	return ErrClientPermissionDenied
}
