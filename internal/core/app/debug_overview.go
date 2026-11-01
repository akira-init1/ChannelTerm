package app

import (
	"context"
	"net"
	"sort"
	"strconv"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/core/session"
)

// DebugPort separates configured targets from observed endpoints and their role.
// Address/Port are empty/zero unless State is observed; no default port is inferred.
type DebugPort struct {
	Role        string `json:"role"`
	Perspective string `json:"perspective"`
	Configured  string `json:"configured"`
	Address     string `json:"address"`
	Port        uint16 `json:"port"`
	State       string `json:"state"`
}

// DebugObservation is public, capability-free inventory. Client reports are not
// Host verification. SessionID is optional and never a short reference. Stale or
// disconnected reports expose unknown target state, retaining the last observation.
type DebugObservation struct {
	ID              string                 `json:"id"`
	Name            string                 `json:"name"`
	Mode            string                 `json:"mode"`
	SessionID       string                 `json:"session_id"`
	Connection      string                 `json:"connection"`
	TargetState     string                 `json:"target_state"`
	LastTargetState string                 `json:"last_target_state"`
	TunnelState     string                 `json:"tunnel_state"`
	Source          string                 `json:"source"`
	Client          session.ClientIdentity `json:"client"`
	UpdatedAt       time.Time              `json:"updated_at"`
	Stale           bool                   `json:"stale"`
	Ports           []DebugPort            `json:"ports"`
}

// DebugOverview is one bounded inventory observation, not an atomic target-state
// transaction. Reading it never renews a lease/heartbeat or contacts a debugger.
type DebugOverview struct {
	ObservedAt time.Time          `json:"observed_at"`
	Operations []DebugObservation `json:"operations"`
}

// DebugOverview returns existing bridges, managed runs and registered external
// observations. Each owner is copied under its state lock, never its I/O lock.
// Unregistered shell commands/processes/ports are deliberately not discovered.
func (a *Application) DebugOverview(ctx context.Context) (DebugOverview, error) {
	out := DebugOverview{ObservedAt: time.Now().UTC(), Operations: []DebugObservation{}}
	if err := ctx.Err(); err != nil {
		return out, err
	}
	a.bridges.mu.Lock()
	bridges := make([]*bridgeRun, 0, len(a.bridges.runs))
	for _, r := range a.bridges.runs {
		bridges = append(bridges, r)
	}
	a.bridges.mu.Unlock()
	for _, r := range bridges {
		r.mu.Lock()
		s, updated, client := r.status, r.updated, r.clientIdentity
		r.mu.Unlock()
		if bridgeEnded(s.State) {
			continue
		}
		p := DebugPort{Role: "authenticated_bridge", Perspective: "host", Address: s.Address, State: "observed"}
		_, port, err := net.SplitHostPort(s.Address)
		if err == nil {
			n, _ := strconv.ParseUint(port, 10, 16)
			p.Port = uint16(n)
		}
		if p.Port == 0 {
			p.State, p.Address = "unknown", ""
		}
		out.Operations = append(out.Operations, DebugObservation{ID: s.BridgeID, Name: s.BridgeID, Mode: "serial_bridge", SessionID: s.SessionID, Connection: s.State, TargetState: "unknown", LastTargetState: "unknown", TunnelState: "not_applicable", Source: "host", Client: client, UpdatedAt: updated, Ports: []DebugPort{p}})
	}
	a.debug.mu.Lock()
	runs := make([]*debugRun, 0, len(a.debug.runs))
	for _, r := range a.debug.runs {
		runs = append(runs, r)
	}
	a.debug.mu.Unlock()
	for _, r := range runs {
		r.mu.Lock()
		state, updated, client := r.state, r.updated, r.clientIdentity
		r.mu.Unlock()
		connection, target := "unknown", "unknown"
		switch state {
		case "starting":
			connection = "connecting"
		case "running", "stopped", "exited":
			connection, target = "connected", state
		case "closed", "failed":
			connection = "disconnected"
		}
		out.Operations = append(out.Operations, DebugObservation{ID: r.id, Name: r.id, Mode: "managed", SessionID: r.terminal.ID(), Connection: connection, TargetState: target, LastTargetState: target, TunnelState: "not_applicable", Source: "host", Client: client, UpdatedAt: updated, Ports: []DebugPort{{Role: "target", Perspective: "host", State: "not_applicable"}}})
	}
	a.observations.mu.Lock()
	for _, r := range a.observations.runs {
		item := r.item
		item.Ports = append([]DebugPort{}, item.Ports...)
		item.Stale = item.Source == "client_report" && out.ObservedAt.Sub(item.UpdatedAt) > 30*time.Second
		if item.Stale {
			item.Connection = "unknown"
			if item.Mode == "ssh_tunnel" {
				item.TunnelState = "unknown"
			}
		}
		if item.Stale || item.Connection != "connected" {
			item.TargetState = "unknown"
		}
		out.Operations = append(out.Operations, item)
	}
	a.observations.mu.Unlock()
	sort.Slice(out.Operations, func(i, j int) bool { return out.Operations[i].ID < out.Operations[j].ID })
	return out, ctx.Err()
}
