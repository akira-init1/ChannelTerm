package app

import (
	"context"
	"fmt"

	"github.com/akira-init1/ChannelTerm/internal/core/session"
	sshtransport "github.com/akira-init1/ChannelTerm/internal/transport/ssh"
)

// OpenSSHRequest supplies explicit SSH connection settings and display-only
// metadata. Config contains ordinary settings and file paths, not SSH-library
// objects. Authentication stays inside Transport and never enters SessionInfo.
type OpenSSHRequest struct {
	Config sshtransport.Config
	Label  string
}

// OpenSSHResult identifies a Manager-owned SSH PTY shell Session.
type OpenSSHResult struct {
	Info   session.SessionInfo
	Reused bool
}

// OpenSSH opens or reuses a Session for the exact SSH user, host, and port.
// ctx cancels connection setup and duplicate-open waiting. Reuse retains the
// original credentials, PTY settings, and label; it does not reauthenticate.
// Manager owns successful Sessions, and closes them through existing lifecycle
// handling. No SSH configuration file or discovery state is created.
func (a *Application) OpenSSH(ctx context.Context, request OpenSSHRequest) (OpenSSHResult, error) {
	return a.openSSH(ctx, request, func(id string, transport *sshtransport.Transport) (ConnectedSession, error) {
		return session.New(id, transport)
	})
}

// openSSH keeps the construction seam private while using the same candidate
// ownership and Manager coordination as serial opens.
func (a *Application) openSSH(ctx context.Context, request OpenSSHRequest, create func(string, *sshtransport.Transport) (ConnectedSession, error)) (OpenSSHResult, error) {
	if a == nil || a.manager == nil {
		return OpenSSHResult{}, ErrNilApplicationManager
	}
	if err := ctx.Err(); err != nil {
		return OpenSSHResult{}, err
	}
	transport, err := sshtransport.New(request.Config)
	if err != nil {
		return OpenSSHResult{}, err
	}
	info, created, err := a.manager.GetOrCreate(ctx, session.SessionMetadata{
		Transport: "ssh", Endpoint: transport.Endpoint(), Label: request.Label,
	}, func() (session.Session, error) {
		id, err := allocateSessionID(ctx, a.manager, newSessionID)
		if err != nil {
			return nil, err
		}
		candidate, err := create(id, transport)
		if err != nil {
			return nil, fmt.Errorf("create SSH session: %w", err)
		}
		if err := candidate.Connect(ctx); err != nil {
			return nil, closeSessionCandidate(fmt.Errorf("connect SSH %s: %w", transport.Endpoint(), err), candidate, "SSH session")
		}
		if err := ctx.Err(); err != nil {
			return nil, closeSessionCandidate(err, candidate, "SSH session")
		}
		return candidate, nil
	})
	if err != nil {
		return OpenSSHResult{}, err
	}
	return OpenSSHResult{Info: info, Reused: !created}, nil
}
