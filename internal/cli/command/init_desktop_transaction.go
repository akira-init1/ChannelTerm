package command

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/core/config"
	initmcp "github.com/akira-init1/ChannelTerm/internal/init/mcp"
	"github.com/gofrs/flock"
)

// desktopRepairJournal contains no secret. It is durably written before a named
// registration is created, so a crash before Register returns is also recoverable.
// File fingerprints distinguish a published replacement from an uncommitted one.
type desktopRepairJournal struct {
	Version int    `json:"version"`
	Path    string `json:"path"`
	Before  string `json:"before"`
	After   string `json:"after,omitempty"`
	Name    string `json:"name"`
	ID      string `json:"client_id,omitempty"`
}

type desktopRepairResult struct {
	State       string `json:"state"`
	ClientID    string `json:"client_id,omitempty"`
	Permissions string `json:"permissions,omitempty"`
}

// desktopRepairCoordinator composes local credential authority and client-file
// adapters. It never changes Host listeners, old credentials, or Session state.
// One private journal and lock serialize this Host domain across GUI windows.
type desktopRepairCoordinator struct {
	locations initmcp.RepairLocations
	store     config.ClientStore
	journal   string
	probe     func(context.Context, string, string) error
	// publish is a test seam for an interrupted or failed configuration replacement.
	publish func(*initmcp.RepairPlan, string) error
}

func (c *desktopRepairCoordinator) locked(ctx context.Context, work func() (desktopRepairResult, error)) (desktopRepairResult, error) {
	if err := os.MkdirAll(filepath.Dir(c.journal), 0700); err != nil {
		return desktopRepairResult{}, errors.New("repair state directory is unavailable")
	}
	lock := flock.New(c.journal + ".lock")
	locked, err := lock.TryLockContext(ctx, 20*time.Millisecond)
	if err != nil || !locked {
		return desktopRepairResult{}, errors.New("another repair is active or cancellation was requested")
	}
	defer lock.Close()
	return work()
}

func (c *desktopRepairCoordinator) apply(ctx context.Context, target initmcp.RepairTarget, endpoint, expected string, replace bool) (desktopRepairResult, error) {
	return c.locked(ctx, func() (desktopRepairResult, error) {
		if _, err := os.Lstat(c.journal); err == nil {
			return desktopRepairResult{}, errors.New("recover the previous repair before applying another")
		} else if !errors.Is(err, os.ErrNotExist) {
			return desktopRepairResult{}, errors.New("repair recovery state is unavailable")
		}
		if !initmcp.SafeRepairEndpoint(endpoint) {
			return desktopRepairResult{}, errors.New("repair requires a numeric loopback HTTP endpoint")
		}
		makePlan := func(token string) (*initmcp.RepairPlan, error) {
			return initmcp.PreviewRepair(c.locations, target, initmcp.Endpoint{Transport: initmcp.TransportStreamableHTTP, URL: endpoint, Headers: map[string]string{"Authorization": "Bearer " + token}})
		}
		plan, err := makePlan("preview-placeholder")
		if err != nil {
			return desktopRepairResult{}, err
		}
		if expected != plan.Fingerprint {
			return desktopRepairResult{}, errors.New("configuration changed; request a fresh preview")
		}
		if plan.Existing && !replace {
			return desktopRepairResult{State: "preserved"}, nil
		}
		if err := ctx.Err(); err != nil {
			return desktopRepairResult{}, err
		}
		journal := desktopRepairJournal{Version: 1, Path: plan.Path, Before: plan.Fingerprint, Name: "ChannelTerm " + target.Client + " " + rand.Text()}
		clients, err := c.store.List()
		if err != nil {
			return desktopRepairResult{}, errors.New("client registrations unavailable")
		}
		for _, client := range clients {
			if client.Name == journal.Name {
				return desktopRepairResult{}, errors.New("repair registration name collision")
			}
		}
		if err := writeDesktopRepairJournal(c.journal, journal); err != nil {
			return desktopRepairResult{}, err
		}
		identity, token, err := c.store.Register(journal.Name, "agent")
		if err != nil {
			return c.abortUnpublished(journal, errors.New("could not create independent read-only credential"))
		}
		journal.ID = identity.ID
		// Probe before touching client configuration. A credential for a different
		// local Host domain fails authentication and is rolled back, never exported.
		if err := c.probe(ctx, endpoint, token); err != nil {
			return c.abortUnpublished(journal, errors.Join(errors.New("new credential could not authenticate a read-only Host request"), err))
		}
		plan, err = makePlan(token)
		if err != nil {
			return c.abortUnpublished(journal, err)
		}
		if plan.Fingerprint != expected {
			return c.abortUnpublished(journal, errors.New("configuration changed; request a fresh preview"))
		}
		journal.After, err = plan.ReplacementFingerprint()
		if err != nil {
			return c.abortUnpublished(journal, errors.New("replacement could not be encoded"))
		}
		if err := writeDesktopRepairJournal(c.journal, journal); err != nil {
			return c.abortUnpublished(journal, err)
		}
		if err := ctx.Err(); err != nil {
			return c.abortUnpublished(journal, err)
		}
		publish := c.publish
		if publish == nil {
			publish = func(plan *initmcp.RepairPlan, expected string) error { return plan.Apply(expected) }
		}
		if err := publish(plan, expected); err != nil {
			return c.failedRepair(errors.New("client configuration could not be safely replaced"))
		}
		if err := os.Remove(c.journal); err != nil {
			return desktopRepairResult{State: "applied", ClientID: identity.ID}, errors.New("configuration saved; recovery marker cleanup is pending")
		}
		return desktopRepairResult{State: "applied", ClientID: identity.ID, Permissions: "read_only"}, nil
	})
}

func (c *desktopRepairCoordinator) failedRepair(cause error) (desktopRepairResult, error) {
	result, err := c.recoverLocked()
	if err != nil {
		return result, errors.Join(cause, err)
	}
	if result.State == "recovered_applied" {
		return result, nil
	}
	return result, cause
}

func (c *desktopRepairCoordinator) recover(ctx context.Context) (desktopRepairResult, error) {
	// Opening an unused wizard creates no directory, lock, Host identity or client.
	if _, err := os.Lstat(c.journal); errors.Is(err, os.ErrNotExist) {
		return desktopRepairResult{State: "none"}, nil
	}
	return c.locked(ctx, c.recoverLocked)
}

// recoverLocked never restores an old file over a concurrent user's edit. An
// unexpected hash or registration change is an explicit manual-recovery boundary.
func (c *desktopRepairCoordinator) recoverLocked() (desktopRepairResult, error) {
	info, err := os.Lstat(c.journal)
	if errors.Is(err, os.ErrNotExist) {
		return desktopRepairResult{State: "none"}, nil
	}
	if err != nil || !info.Mode().IsRegular() || info.Size() > 16384 {
		return desktopRepairResult{State: "recovery_required"}, errors.New("repair recovery record needs manual inspection")
	}
	file, err := os.Open(c.journal)
	if err != nil {
		return desktopRepairResult{State: "recovery_required"}, errors.New("repair recovery record is unreadable")
	}
	data, err := io.ReadAll(io.LimitReader(file, 16385))
	_ = file.Close()
	var journal desktopRepairJournal
	if err != nil || len(data) > 16384 || json.Unmarshal(data, &journal) != nil || journal.Version != 1 || !filepath.IsAbs(journal.Path) || len(journal.Before) != 64 || journal.Name == "" {
		return desktopRepairResult{State: "recovery_required"}, errors.New("repair recovery record is invalid")
	}
	actual, err := initmcp.RepairFingerprint(journal.Path)
	if err != nil {
		return desktopRepairResult{State: "recovery_required"}, errors.New("repair target cannot be verified")
	}
	result := desktopRepairResult{State: "rolled_back", ClientID: journal.ID}
	if journal.After != "" && actual == journal.After {
		result.State = "recovered_applied"
	} else if actual == journal.Before {
		if err := c.removeNewRegistration(journal); err != nil {
			return desktopRepairResult{State: "recovery_required"}, err
		}
	} else {
		return desktopRepairResult{State: "recovery_required"}, errors.New("configuration changed after interruption; original and new credentials are preserved for manual inspection")
	}
	if err := os.Remove(c.journal); err != nil {
		return result, errors.New("repair recovery marker cleanup is pending")
	}
	return result, nil
}

func writeDesktopRepairJournal(path string, journal desktopRepairJournal) error {
	data, err := json.Marshal(journal)
	if err != nil {
		return errors.New("repair state could not be encoded")
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".mcp-repair-*.tmp")
	if err != nil {
		return errors.New("repair recovery state cannot be created")
	}
	defer os.Remove(file.Name())
	defer file.Close()
	if err := file.Chmod(0600); err != nil {
		return errors.New("repair recovery permissions cannot be set")
	}
	if _, err := file.Write(data); err != nil {
		return errors.New("repair recovery state cannot be written")
	}
	if err := file.Sync(); err != nil {
		return errors.New("repair recovery state cannot be synchronized")
	}
	if err := file.Close(); err != nil {
		return errors.New("repair recovery state cannot be closed")
	}
	if err := os.Rename(file.Name(), path); err != nil {
		return errors.New("repair recovery state cannot be published")
	}
	return nil
}

// abortUnpublished knows publication has not started. It can revoke only this
// transaction's new registration even if an unrelated editor changed the file.
func (c *desktopRepairCoordinator) abortUnpublished(journal desktopRepairJournal, cause error) (desktopRepairResult, error) {
	if err := c.removeNewRegistration(journal); err != nil {
		return desktopRepairResult{State: "recovery_required"}, errors.Join(cause, err)
	}
	if err := os.Remove(c.journal); err != nil {
		return desktopRepairResult{State: "recovery_required"}, errors.Join(cause, errors.New("repair marker cleanup is pending"))
	}
	return desktopRepairResult{State: "rolled_back"}, cause
}

func (c *desktopRepairCoordinator) removeNewRegistration(journal desktopRepairJournal) error {
	clients, err := c.store.List()
	if err != nil {
		return errors.New("repair credential cleanup is pending")
	}
	var owned []config.ClientIdentity
	for _, client := range clients {
		if client.Name == journal.Name {
			owned = append(owned, client)
		}
	}
	if len(owned) > 1 {
		return errors.New("repair registration ownership is ambiguous")
	}
	if len(owned) == 1 {
		client := owned[0]
		if client.Kind != "agent" || client.WriteAllowed || (journal.ID != "" && client.ID != journal.ID) {
			return errors.New("repair registration changed externally")
		}
		if err := c.store.Remove(client.ID); err != nil {
			return errors.New("repair credential cleanup is pending")
		}
	}
	return nil
}
