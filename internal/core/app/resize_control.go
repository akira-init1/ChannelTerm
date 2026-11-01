package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"

	"github.com/akira-init1/ChannelTerm/internal/core/channel"
	"github.com/akira-init1/ChannelTerm/internal/core/session"
)

// ErrResizeNotOwned rejects a missing, released or superseded resize capability.
var ErrResizeNotOwned = errors.New("remote size is controlled by another view")

// ErrResizeDimensions rejects dimensions outside the supported character bounds.
var ErrResizeDimensions = errors.New("terminal dimensions must be positive")

// resizeControls retains at most one record per live SSH registration. Its lock
// protects lookup only; a blocked transport never holds the registry lock.
type resizeControls struct {
	mu      sync.Mutex
	entries map[string]*resizeControl
}

type resizeControl struct {
	mu          sync.Mutex
	owner       string
	initialized bool
}

func (a *Application) resizeControl(identifier string) (session.Session, *resizeControl, error) {
	if a == nil || a.manager == nil {
		return nil, nil, ErrNilApplicationManager
	}
	a.resize.mu.Lock()
	defer a.resize.mu.Unlock()
	var selected session.SessionInfo
	live := make(map[string]bool)
	for _, info := range a.manager.ListInfo() {
		if info.State == session.StateOpen && info.Metadata.Transport == "ssh" {
			live[info.Metadata.Reference] = true
		}
		if info.ID == identifier || info.Metadata.Reference == identifier {
			selected = info
		}
	}
	if selected.ID == "" {
		return nil, nil, ErrSessionNotFound
	}
	if selected.Metadata.Transport != "ssh" {
		return nil, nil, channel.ErrResizeUnsupported
	}
	if selected.State != session.StateOpen {
		return nil, nil, session.ErrNotOpen
	}
	terminal, ok := a.manager.Get(selected.Metadata.Reference)
	if !ok {
		return nil, nil, ErrSessionNotFound
	}
	if a.resize.entries == nil {
		a.resize.entries = make(map[string]*resizeControl)
	}
	for reference := range a.resize.entries {
		if !live[reference] {
			delete(a.resize.entries, reference)
		}
	}
	control := a.resize.entries[selected.Metadata.Reference]
	if control == nil {
		control = &resizeControl{}
		a.resize.entries[selected.Metadata.Reference] = control
	}
	return terminal, control, nil
}

// TakeResizeControl explicitly replaces the SSH size controller. The returned
// private capability belongs to one adapter attachment, not a registered client
// identity or writer lease. No dimensions are sent and no device is opened.
func (a *Application) TakeResizeControl(ctx context.Context, identifier string) (string, error) {
	return a.takeResizeControl(ctx, identifier, false)
}

// initial is used only after creation, never reuse. A concurrent explicit
// takeover wins over delayed creator initialization, including a later release.
func (a *Application) takeResizeControl(ctx context.Context, identifier string, initial bool) (string, error) {
	if err := CheckClientAccess(ctx, ClientManage); err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	var secret [32]byte
	if _, err := rand.Read(secret[:]); err != nil {
		return "", err
	}
	_, control, err := a.resizeControl(identifier)
	if err != nil {
		return "", err
	}
	if !control.mu.TryLock() {
		return "", ErrSessionBusy
	}
	defer control.mu.Unlock()
	if initial && control.initialized {
		return "", nil
	}
	owner := hex.EncodeToString(secret[:])
	control.owner = owner
	control.initialized = true
	return owner, nil
}

// OwnsResizeControl reports a capability match without granting or renewing it.
// Released controls never automatically pass to another observer.
func (a *Application) OwnsResizeControl(identifier, owner string) (bool, error) {
	_, control, err := a.resizeControl(identifier)
	if err != nil {
		return false, err
	}
	if !control.mu.TryLock() {
		return false, ErrSessionBusy
	}
	defer control.mu.Unlock()
	return owner != "" && control.owner == owner, nil
}

// ReleaseResizeControl is idempotent for stale capabilities and cannot release
// a successor's control. It does not close the Session or alter a writer lease.
func (a *Application) ReleaseResizeControl(identifier, owner string) error {
	_, control, err := a.resizeControl(identifier)
	if err != nil {
		return err
	}
	if !control.mu.TryLock() {
		return ErrSessionBusy
	}
	defer control.mu.Unlock()
	if owner != "" && control.owner == owner {
		control.owner = ""
	}
	return nil
}

// ResizeSessionOwned serializes capability validation and transport submission
// against explicit takeover/release. A busy controller is rejected, not queued;
// already submitted bytes cannot be recalled. Session Close can unblock I/O.
func (a *Application) ResizeSessionOwned(ctx context.Context, identifier, owner string, cols, rows uint16) error {
	if err := CheckClientAccess(ctx, ClientWrite); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if cols < 1 || rows < 1 {
		return ErrResizeDimensions
	}
	terminal, control, err := a.resizeControl(identifier)
	if err != nil {
		return err
	}
	if !control.mu.TryLock() {
		return ErrSessionBusy
	}
	defer control.mu.Unlock()
	if owner == "" || owner != control.owner {
		return ErrResizeNotOwned
	}
	return terminal.Resize(cols, rows)
}
