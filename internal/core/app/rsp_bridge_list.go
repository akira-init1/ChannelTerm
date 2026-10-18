package app

import (
	"context"
	"fmt"
	"net/netip"
	"sort"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/core/session"
)

// BridgeSummary is a non-secret snapshot of an unfinished external RSP bridge.
// Address and Port identify the Host endpoint, not an unauthenticated GDB target.
// SessionRef and Target are empty if the Session was removed during finalization.
// No management capability or connection descriptor is included.
type BridgeSummary struct {
	BridgeID      string    `json:"bridge_id"`
	SessionID     string    `json:"session_id"`
	SessionRef    string    `json:"session_ref"`
	Target        string    `json:"target"`
	Address       string    `json:"address"`
	Port          uint16    `json:"port"`
	State         string    `json:"state"`
	ExpiresAt     time.Time `json:"expires_at"`
	AutoKeepalive bool      `json:"auto_keepalive"`
}

// ListRSPBridges returns all unfinished external bridges in this Application,
// ordered by port, address, then bridge ID. Disconnected and recovering bridges
// remain included until finalization; closed, failed, and restored runs are
// excluded. Snapshots may change immediately after reading; this is not an
// OS-wide port scan. It neither renews leases nor sends target bytes.
// Cancellation stops collection without affecting bridges. Adapters must enforce
// Host access policy; observation does not require a per-bridge owner capability.
func (a *Application) ListRSPBridges(ctx context.Context) ([]BridgeSummary, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Copy run pointers before reading per-run status so inventory collection
	// does not hold the coordinator lock across Session inspection or sorting.
	a.bridges.mu.Lock()
	runs := make([]*bridgeRun, 0, len(a.bridges.runs))
	for _, r := range a.bridges.runs {
		runs = append(runs, r)
	}
	a.bridges.mu.Unlock()
	metadata := make(map[string]session.SessionMetadata)
	for _, info := range a.manager.ListInfo() {
		metadata[info.ID] = info.Metadata
	}
	items := make([]BridgeSummary, 0, len(runs))
	for _, r := range runs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		s := r.snapshot()
		if bridgeEnded(s.State) {
			continue
		}
		address, err := netip.ParseAddrPort(s.Address)
		if err != nil {
			return nil, fmt.Errorf("read bridge address: %w", err)
		}
		info := metadata[s.SessionID]
		items = append(items, BridgeSummary{
			BridgeID: s.BridgeID, SessionID: s.SessionID, SessionRef: info.Reference,
			Target: info.Endpoint, Address: s.Address, Port: address.Port(),
			State: s.State, ExpiresAt: s.ExpiresAt, AutoKeepalive: s.AutoKeepalive,
		})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Port != items[j].Port {
			return items[i].Port < items[j].Port
		}
		if items[i].Address != items[j].Address {
			return items[i].Address < items[j].Address
		}
		return items[i].BridgeID < items[j].BridgeID
	})
	return items, ctx.Err()
}
