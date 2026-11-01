package app

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/akira-init1/ChannelTerm/internal/core/debugger"
)

// DebugRegistration registers metadata only. Executor is manual or mi; neither
// launches a process. MI executors must own their structured observation stream.
// Owner stays private; RequestID deduplicates registration under the authenticated
// client identity. Association does not acquire a Session or its input lease.
type DebugRegistration struct {
	Owner     string      `json:"owner"`
	RequestID string      `json:"request_id"`
	Name      string      `json:"name"`
	Mode      string      `json:"mode"`
	SessionID string      `json:"session_id,omitempty"`
	Executor  string      `json:"executor"`
	Ports     []DebugPort `json:"ports"`
}

// DebugObservationReport is a bounded full connection snapshot plus an optional
// single MI record. It is an external report, not a command or trusted Host fact.
// Sequence is monotonic; retrying the latest exact report does not refresh time.
type DebugObservationReport struct {
	ID          string      `json:"id"`
	Owner       string      `json:"owner"`
	Sequence    uint64      `json:"sequence"`
	Connection  string      `json:"connection"`
	TunnelState string      `json:"tunnel_state"`
	MI          string      `json:"mi,omitempty"`
	Ports       []DebugPort `json:"ports"`
}

type debugObservations struct {
	mu       sync.Mutex
	runs     map[string]*debugObservationRun
	requests map[string]string
}
type debugObservationRun struct {
	item                        DebugObservation
	owner, registration, report [32]byte
	sequence                    uint64
	executor                    string
}

func debugText(s string, maximum int) bool {
	return len(s) <= maximum && utf8.ValidString(s) && !strings.ContainsFunc(s, unicode.IsControl)
}
func debugRoles(mode string) []string {
	switch mode {
	case "tcp":
		return []string{"target"}
	case "ssh_tunnel":
		return []string{"local_forward", "ssh", "target"}
	case "ssh_stdio", "ssh_terminal", "ssh_terminal_tcp":
		return []string{"ssh", "target"}
	}
	return nil
}
func debugAddress(address string, zero bool) (uint16, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil || host == "" || !debugText(address, 256) || strings.ContainsAny(host, " /\\@?#") {
		return 0, errors.New("invalid debug endpoint; use host:port or [IPv6]:port")
	}
	n, err := strconv.ParseUint(port, 10, 16)
	if err != nil || (!zero && n == 0) {
		return 0, errors.New("invalid debug port")
	}
	return uint16(n), nil
}

// normalizeDebugPorts fixes roles/perspectives from mode and rejects invented
// ports for stdio/local debugging. Reported endpoints remain client assertions.
func normalizeDebugPorts(mode string, ports []DebugPort, report bool) ([]DebugPort, error) {
	roles := debugRoles(mode)
	if roles == nil || len(ports) > len(roles) {
		return nil, errors.New("invalid debug mode or port roles")
	}
	out := make([]DebugPort, 0, len(roles))
	seen := map[string]bool{}
	for _, role := range roles {
		p := DebugPort{Role: role, Perspective: "executor", State: "unknown"}
		if role == "target" && mode == "ssh_tunnel" {
			p.Perspective = "remote"
		}
		if role == "target" && (mode == "ssh_stdio" || mode == "ssh_terminal") {
			p.State = "not_applicable"
		}
		for _, input := range ports {
			if input.Role != role {
				continue
			}
			if seen[role] {
				return nil, errors.New("duplicate debug port role")
			}
			seen[role] = true
			if input.Perspective != "" && input.Perspective != p.Perspective {
				return nil, errors.New("invalid endpoint perspective")
			}
			if p.State == "not_applicable" {
				if input.Address != "" || input.Configured != "" || input.Port != 0 || (input.State != "" && input.State != "not_applicable") {
					return nil, errors.New("debug TCP port is not applicable")
				}
				continue
			}
			p.Configured = input.Configured
			if p.Configured != "" {
				if _, err := debugAddress(p.Configured, true); err != nil {
					return nil, err
				}
			}
			if input.Address != "" {
				if !report {
					return nil, errors.New("registration cannot assert observed endpoints")
				}
				n, err := debugAddress(input.Address, false)
				if err != nil {
					return nil, err
				}
				if input.Port != 0 && input.Port != n {
					return nil, errors.New("port does not match observed address")
				}
				p.Address, p.Port, p.State = input.Address, n, "observed"
			} else if input.Port != 0 {
				return nil, errors.New("observed port requires address")
			}
			if input.State != "" && input.State != p.State {
				return nil, errors.New("invalid endpoint evidence state")
			}
		}
		out = append(out, p)
	}
	if len(seen) != len(ports) {
		return nil, errors.New("unknown debug port role")
	}
	return out, nil
}

// RegisterDebugObservation records an explicit external operation without I/O.
// At most 128 registrations are retained per Host lifetime, including completed
// operations. Reports require both this capability and the registering client.
func (a *Application) RegisterDebugObservation(ctx context.Context, q DebugRegistration) (DebugObservation, error) {
	if err := CheckClientAccess(ctx, ClientWrite); err != nil {
		return DebugObservation{}, err
	}
	if err := ctx.Err(); err != nil {
		return DebugObservation{}, err
	}
	if len(q.Owner) < 32 || len(q.Owner) > 128 || q.RequestID == "" || !debugText(q.RequestID, 128) || q.Name == "" || !debugText(q.Name, 128) || !debugText(q.SessionID, 512) || (q.Executor != "manual" && q.Executor != "mi") {
		return DebugObservation{}, errors.New("invalid debug registration")
	}
	if strings.Contains(q.Name, q.Owner) {
		return DebugObservation{}, errors.New("registration must not publish capabilities")
	}
	ports, err := normalizeDebugPorts(q.Mode, q.Ports, false)
	if err != nil {
		return DebugObservation{}, err
	}
	if debugPortsContain(ports, q.Owner) {
		return DebugObservation{}, errors.New("registration must not publish capabilities")
	}
	client := ClientIdentityFromContext(ctx)
	encoded, _ := json.Marshal(q)
	digest := sha256.Sum256(encoded)
	key := client.ID + "\x00" + q.RequestID
	a.observations.mu.Lock()
	defer a.observations.mu.Unlock()
	if id := a.observations.requests[key]; id != "" {
		r := a.observations.runs[id]
		if r.registration != digest {
			return DebugObservation{}, errors.New("conflicting debug registration retry")
		}
		return cloneDebugObservation(r.item), nil
	}
	if q.SessionID != "" {
		found := false
		for _, info := range a.manager.ListInfo() {
			if info.ID == q.SessionID {
				found = true
				if strings.HasPrefix(q.Mode, "ssh_terminal") && info.Metadata.Transport != "ssh" {
					return DebugObservation{}, errors.New("terminal debugger requires SSH Session")
				}
			}
		}
		if !found {
			return DebugObservation{}, ErrSessionNotFound
		}
	} else if strings.HasPrefix(q.Mode, "ssh_terminal") {
		return DebugObservation{}, errors.New("terminal debugger requires full SSH Session ID")
	}
	if len(a.observations.runs) >= 128 {
		return DebugObservation{}, errors.New("debug observation retention limit reached")
	}
	id, err := newTerminalCommandToken()
	if err != nil {
		return DebugObservation{}, err
	}
	item := DebugObservation{ID: "OBS-" + id, Name: q.Name, Mode: q.Mode, SessionID: q.SessionID, Connection: "unknown", TargetState: "unknown", LastTargetState: "unknown", TunnelState: "unknown", Source: "manual", Client: client, UpdatedAt: time.Now().UTC(), Ports: ports}
	if q.Mode != "ssh_tunnel" {
		item.TunnelState = "not_applicable"
	}
	if a.observations.runs == nil {
		a.observations.runs = map[string]*debugObservationRun{}
		a.observations.requests = map[string]string{}
	}
	a.observations.runs[item.ID] = &debugObservationRun{item: item, owner: sha256.Sum256([]byte(q.Owner)), registration: digest, executor: q.Executor}
	a.observations.requests[key] = item.ID
	return cloneDebugObservation(item), nil
}
func cloneDebugObservation(item DebugObservation) DebugObservation {
	item.Ports = append([]DebugPort{}, item.Ports...)
	return item
}

func debugPortsContain(ports []DebugPort, secret string) bool {
	for _, p := range ports {
		if strings.Contains(p.Address, secret) || strings.Contains(p.Configured, secret) {
			return true
		}
	}
	return false
}
func debugConnection(s string) bool {
	return s == "unknown" || s == "connecting" || s == "connected" || s == "disconnected"
}

// ReportDebugObservation accepts executor-owned MI evidence without storing raw
// MI text or inspecting terminal output. Disconnect/staleness invalidates current
// state; reports neither renew a debug lease nor control the debugger/target.
func (a *Application) ReportDebugObservation(ctx context.Context, q DebugObservationReport) (DebugObservation, error) {
	if err := CheckClientAccess(ctx, ClientWrite); err != nil {
		return DebugObservation{}, err
	}
	if err := ctx.Err(); err != nil {
		return DebugObservation{}, err
	}
	if q.Sequence == 0 || q.Sequence > 1<<53-1 || !debugConnection(q.Connection) || len(q.MI) > 8192 || len(q.Owner) < 32 || len(q.Owner) > 128 || !debugText(q.ID, 128) || len(q.Ports) > 3 {
		return DebugObservation{}, errors.New("invalid debug report")
	}
	var record debugger.Record
	if q.MI != "" {
		var err error
		record, err = debugger.Parse(q.MI)
		if err != nil {
			return DebugObservation{}, errors.New("invalid structured MI record")
		}
	}
	encoded, _ := json.Marshal(q)
	digest := sha256.Sum256(encoded)
	owner := sha256.Sum256([]byte(q.Owner))
	a.observations.mu.Lock()
	defer a.observations.mu.Unlock()
	r := a.observations.runs[q.ID]
	if r == nil || subtle.ConstantTimeCompare(owner[:], r.owner[:]) != 1 || r.item.Client.ID != ClientIdentityFromContext(ctx).ID {
		return DebugObservation{}, errors.New("unknown debug observation or invalid reporter")
	}
	if r.executor != "mi" {
		return DebugObservation{}, errors.New("manual registration has no structured observer")
	}
	if q.Sequence == r.sequence && digest == r.report {
		return cloneDebugObservation(r.item), nil
	}
	if q.Sequence <= r.sequence {
		return DebugObservation{}, errors.New("debug report sequence must increase")
	}
	if r.item.Mode == "ssh_tunnel" {
		if !debugConnection(q.TunnelState) {
			return DebugObservation{}, errors.New("invalid tunnel state")
		}
	} else if q.TunnelState != "" && q.TunnelState != "not_applicable" {
		return DebugObservation{}, errors.New("tunnel state not applicable")
	}
	ports, err := normalizeDebugPorts(r.item.Mode, q.Ports, true)
	if err != nil {
		return DebugObservation{}, err
	}
	if debugPortsContain(ports, q.Owner) {
		return DebugObservation{}, errors.New("report must not publish capabilities")
	}
	for i := range ports {
		if ports[i].Configured != "" && ports[i].Configured != r.item.Ports[i].Configured {
			return DebugObservation{}, errors.New("configured endpoint cannot change")
		}
		ports[i].Configured = r.item.Ports[i].Configured
	}
	target := r.item.TargetState
	if r.item.Connection != "connected" || time.Since(r.item.UpdatedAt) > 30*time.Second {
		target = "unknown"
	}
	if record.Kind == "*" || record.Kind == "^" && record.Class == "running" {
		switch record.Class {
		case "running":
			target = "running"
		case "stopped":
			target = "stopped"
			if reason, _ := record.Results["reason"].(string); strings.HasPrefix(reason, "exited") {
				target = "exited"
			}
		}
	}
	if record.Kind == "=" && record.Class == "thread-group-exited" {
		target = "exited"
	}
	if target != "unknown" {
		r.item.LastTargetState = target
	}
	if q.Connection != "connected" {
		target = "unknown"
	}
	r.item.Connection, r.item.TargetState, r.item.Source = q.Connection, target, "client_report"
	if r.item.Mode == "ssh_tunnel" {
		r.item.TunnelState = q.TunnelState
	}
	r.item.Ports, r.item.UpdatedAt = ports, time.Now().UTC()
	r.sequence, r.report = q.Sequence, digest
	return cloneDebugObservation(r.item), nil
}
