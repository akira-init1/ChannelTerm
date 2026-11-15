package app

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/akira-init1/ChannelTerm/internal/core/session"
)

const (
	// ClientInstanceTTL bounds presence; expiration removes idle-policy retention
	// by explicit uses but never directly closes a Session or revokes a lease.
	ClientInstanceTTL  = 60 * time.Second
	maxClientInstances = 128
	maxInstanceUses    = 64
	instanceRetention  = 5 * time.Minute
)

var (
	// ErrClientInstance rejects missing, expired, foreign or invalid capabilities.
	ErrClientInstance = errors.New("client instance unavailable; register a new instance explicitly")
	// ErrClientInstanceLimit reports bounded presence storage exhaustion.
	ErrClientInstanceLimit = errors.New("client instance or usage capacity reached")
	// ErrClientInstanceInput reports invalid public presence metadata.
	ErrClientInstanceInput = errors.New("invalid client instance request")
)

// InstanceProof is a private, credential-bound presence capability, not write permission.
type InstanceProof struct {
	ID     string `json:"id"`
	Secret string `json:"secret"`
	UseID  string `json:"use_id,omitempty"`
}

type instanceProofKey struct{}

// WithInstanceProof carries untrusted adapter input for Application validation.
func WithInstanceProof(ctx context.Context, proof InstanceProof) context.Context {
	return context.WithValue(ctx, instanceProofKey{}, proof)
}

// InstanceUse describes explicit observation/monitoring or an operation, never a lease.
type InstanceUse struct {
	ID        string `json:"id"`
	SessionID string `json:"session_id"`
	Kind      string `json:"kind"`
	Label     string `json:"label"`
	Active    bool   `json:"active"`
}

// ClientInstance is a public snapshot. Offline does not mean its accepted work stopped.
type ClientInstance struct {
	ID                string                 `json:"id"`
	Client            session.ClientIdentity `json:"client"`
	Name              string                 `json:"name"`
	DeclaredEntry     string                 `json:"declared_entry"`
	ParentInstanceID  string                 `json:"parent_instance_id,omitempty"`
	ParentOperationID string                 `json:"parent_operation_id,omitempty"`
	State             string                 `json:"state"`
	ExpiresAt         time.Time              `json:"expires_at"`
	Uses              []InstanceUse          `json:"uses"`
	UsesTruncated     bool                   `json:"uses_truncated"`
}

// InstanceRegistration supplies display claims and an optional one-use parent delegation.
type InstanceRegistration struct {
	Name       string `json:"name"`
	Entry      string `json:"entry"`
	Delegation string `json:"delegation,omitempty"`
}

type instanceRecord struct {
	view   ClientInstance
	secret [32]byte
	uses   map[string]InstanceUse
	keys   map[string]string
	target string
}
type instanceDelegation struct {
	clientID, parentID, operationID, target string
	expires                                 time.Time
}
type clientInstances struct {
	mu          sync.Mutex
	now         func() time.Time // Tests control expiry without sleeps or background sweep goroutines.
	entries     map[string]*instanceRecord
	delegations map[[32]byte]instanceDelegation
}

func (r *clientInstances) clock() time.Time {
	if r.now != nil {
		return r.now()
	}
	return time.Now()
}

// sweep only removes expired presence metadata. Accepted operations retain value
// copies of provenance; active calls pin their record until their deferred cleanup.
func (r *clientInstances) sweep(now time.Time, leases map[string][]InstanceUse) {
	for id, entry := range r.entries {
		active := len(leases[id]) != 0
		for _, use := range entry.uses {
			active = active || use.Active
		}
		if !active && now.After(entry.view.ExpiresAt.Add(instanceRetention)) {
			delete(r.entries, id)
		}
	}
	for key, delegation := range r.delegations {
		if !now.Before(delegation.expires) {
			delete(r.delegations, key)
		}
	}
}

func presenceText(s string, max int) bool {
	return s != "" && len(s) <= max && utf8.ValidString(s) && strings.TrimSpace(s) == s && !strings.ContainsFunc(s, unicode.IsControl)
}

// RegisterClientInstance creates a volatile instance under the authenticated caller.
// Claimed executable/entry names are display metadata, never verified identity.
func (a *Application) RegisterClientInstance(ctx context.Context, q InstanceRegistration) (ClientInstance, InstanceProof, error) {
	if err := ctx.Err(); err != nil {
		return ClientInstance{}, InstanceProof{}, err
	}
	if !presenceText(q.Name, 128) || !presenceText(q.Entry, 32) || len(q.Delegation) > 128 {
		return ClientInstance{}, InstanceProof{}, ErrClientInstanceInput
	}
	client := ClientIdentityFromContext(ctx)
	// Presence does not invent an authenticated human for stdio/local callers.
	if client.ID == "" {
		client = session.ClientIdentity{Name: "Local account", Kind: "unknown"}
	}
	client.InstanceID, client.ParentInstanceID, client.OperationID, client.Entry, client.DeclaredEntry = "", "", "", "", ""
	leases := a.instanceLeaseUses()
	r := &a.instances
	r.mu.Lock()
	defer r.mu.Unlock()
	if a.idle.draining {
		return ClientInstance{}, InstanceProof{}, ErrHostDraining
	}
	a.idle.epoch++
	a.idle.exitSince = time.Time{}
	now := r.clock()
	r.sweep(now, leases)
	if len(r.entries) >= maxClientInstances {
		// Short CLI invocations must not exhaust admission merely by leaving
		// retained history. Evict only the oldest offline record with no work.
		var oldest *instanceRecord
		for _, e := range r.entries {
			if e.view.State != "ended" && now.Before(e.view.ExpiresAt) || len(leases[e.view.ID]) != 0 {
				continue
			}
			active := false
			for _, use := range e.uses {
				active = active || use.Active
			}
			if !active && (oldest == nil || e.view.ExpiresAt.Before(oldest.view.ExpiresAt)) {
				oldest = e
			}
		}
		if oldest == nil {
			return ClientInstance{}, InstanceProof{}, ErrClientInstanceLimit
		}
		delete(r.entries, oldest.view.ID)
	}
	var parent instanceDelegation
	if q.Delegation != "" {
		key := sha256.Sum256([]byte(q.Delegation))
		var ok bool
		parent, ok = r.delegations[key]
		if !ok || parent.clientID != client.ID || !now.Before(parent.expires) {
			return ClientInstance{}, InstanceProof{}, ErrClientInstance
		}
		p := r.entries[parent.parentID]
		if p == nil || p.view.State == "ended" || !now.Before(p.view.ExpiresAt) {
			return ClientInstance{}, InstanceProof{}, ErrClientInstance
		}
		if _, ok := p.uses[parent.operationID]; !ok {
			return ClientInstance{}, InstanceProof{}, ErrClientInstance
		}
		delete(r.delegations, key)
	}
	proof := InstanceProof{ID: "CI-" + rand.Text(), Secret: rand.Text()}
	view := ClientInstance{ID: proof.ID, Client: client, Name: q.Name, DeclaredEntry: q.Entry, ParentInstanceID: parent.parentID, ParentOperationID: parent.operationID, State: "online", ExpiresAt: now.Add(ClientInstanceTTL), Uses: []InstanceUse{}}
	if r.entries == nil {
		r.entries = make(map[string]*instanceRecord)
	}
	r.entries[proof.ID] = &instanceRecord{view: view, secret: sha256.Sum256([]byte(proof.Secret)), uses: make(map[string]InstanceUse), keys: make(map[string]string), target: parent.target}
	return view, proof, nil
}

func (r *clientInstances) authenticate(ctx context.Context, proof InstanceProof, allowEnded bool) (*instanceRecord, error) {
	e := r.entries[proof.ID]
	digest := sha256.Sum256([]byte(proof.Secret))
	if e == nil || len(proof.Secret) > 128 || subtle.ConstantTimeCompare(digest[:], e.secret[:]) != 1 || e.view.Client.ID != ClientIdentityFromContext(ctx).ID {
		return nil, ErrClientInstance
	}
	if !allowEnded && (e.view.State == "ended" || !r.clock().Before(e.view.ExpiresAt)) {
		return nil, ErrClientInstance
	}
	return e, nil
}

func (r *clientInstances) snapshot(e *instanceRecord) ClientInstance {
	view := e.view
	if view.State != "ended" && !r.clock().Before(view.ExpiresAt) {
		view.State = "expired"
	}
	view.Uses = make([]InstanceUse, 0, len(e.uses))
	for _, use := range e.uses {
		if view.State == "online" || use.Active {
			view.Uses = append(view.Uses, use)
		}
		sort.Slice(view.Uses, func(i, j int) bool { return view.Uses[i].ID < view.Uses[j].ID })
	}
	sort.Slice(view.Uses, func(i, j int) bool { return view.Uses[i].ID < view.Uses[j].ID })
	return view
}

// RenewClientInstance renews only presence; it never renews device leases or uses other instances.
func (a *Application) RenewClientInstance(ctx context.Context, proof InstanceProof) (ClientInstance, error) {
	r := &a.instances
	r.mu.Lock()
	defer r.mu.Unlock()
	e, err := r.authenticate(ctx, proof, false)
	if err != nil {
		return ClientInstance{}, err
	}
	e.view.ExpiresAt = r.clock().Add(ClientInstanceTTL)
	return r.snapshot(e), nil
}

// EndClientInstance is idempotent while retained. Active work is not cancelled.
func (a *Application) EndClientInstance(ctx context.Context, proof InstanceProof) error {
	r := &a.instances
	r.mu.Lock()
	defer r.mu.Unlock()
	e, err := r.authenticate(ctx, proof, true)
	if err != nil {
		return err
	}
	if e.view.State != "ended" {
		e.view.State = "ended"
		e.view.ExpiresAt = r.clock()
	}
	return nil
}

// ListClientInstances returns bounded public presence without refreshing it.
func (a *Application) ListClientInstances(ctx context.Context) ([]ClientInstance, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	leases := a.instanceLeaseUses()
	r := &a.instances
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sweep(r.clock(), leases)
	views := make([]ClientInstance, 0, len(r.entries))
	for _, e := range r.entries {
		view := r.snapshot(e)
		for _, use := range leases[e.view.ID] {
			if len(view.Uses) >= maxInstanceUses {
				view.UsesTruncated = true
				break
			}
			view.Uses = append(view.Uses, use)
		}
		views = append(views, view)
	}
	sort.Slice(views, func(i, j int) bool { return views[i].ID < views[j].ID })
	return views, nil
}

// SetClientUse records explicit observation, monitoring or an operation scope.
// key is local to this instance, allowing multiple views of the same Session.
// It is bookkeeping only, never proof of exclusive control or an active device job.
func (a *Application) SetClientUse(ctx context.Context, proof InstanceProof, key, target, kind string) (InstanceUse, error) {
	if !presenceText(key, 128) || (kind != "observe" && kind != "monitor" && kind != "operation") {
		return InstanceUse{}, ErrClientInstanceInput
	}
	r := &a.instances
	r.mu.Lock()
	defer r.mu.Unlock()
	if a.idle.draining {
		return InstanceUse{}, ErrHostDraining
	}
	terminal, err := a.session(target)
	if err != nil {
		return InstanceUse{}, err
	}
	if terminal.State() != session.StateOpen {
		return InstanceUse{}, ErrSessionNotFound
	}
	e, err := r.authenticate(ctx, proof, false)
	if err != nil {
		return InstanceUse{}, err
	}
	if e.target != "" && e.target != terminal.ID() {
		return InstanceUse{}, ErrClientInstance
	}
	a.touchIdleLocked(terminal.ID())
	if id := e.keys[key]; id != "" {
		use := e.uses[id]
		if use.SessionID != terminal.ID() || use.Kind != kind {
			return InstanceUse{}, ErrClientInstanceInput
		}
		return use, nil
	}
	if len(e.uses) >= maxInstanceUses {
		return InstanceUse{}, ErrClientInstanceLimit
	}
	use := InstanceUse{ID: "CU-" + rand.Text(), SessionID: terminal.ID(), Kind: kind, Label: key}
	e.uses[use.ID], e.keys[key] = use, use.ID
	return use, nil
}

// EndClientUse releases only this instance's explicit usage record.
func (a *Application) EndClientUse(ctx context.Context, proof InstanceProof, id string) error {
	r := &a.instances
	r.mu.Lock()
	defer r.mu.Unlock()
	e, err := r.authenticate(ctx, proof, true)
	if err != nil {
		return err
	}
	if use, ok := e.uses[id]; ok {
		if use.Active {
			return ErrClientInstanceInput
		}
		delete(e.keys, use.Label)
		delete(e.uses, id)
		a.touchIdleLocked(use.SessionID)
	}
	return nil
}

// DelegateClientInstance issues a one-use, 30-second child-registration capability
// bound to the caller, live parent operation use and exact Session. It grants no writes.
func (a *Application) DelegateClientInstance(ctx context.Context, proof InstanceProof, useID string) (string, error) {
	leases := a.instanceLeaseUses()
	r := &a.instances
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sweep(r.clock(), leases)
	e, err := r.authenticate(ctx, proof, false)
	if err != nil {
		return "", err
	}
	use, ok := e.uses[useID]
	if !ok || use.Kind != "operation" {
		return "", ErrClientInstanceInput
	}
	if len(r.delegations) >= maxClientInstances {
		return "", ErrClientInstanceLimit
	}
	secret := rand.Text()
	if r.delegations == nil {
		r.delegations = make(map[[32]byte]instanceDelegation)
	}
	r.delegations[sha256.Sum256([]byte(secret))] = instanceDelegation{clientID: e.view.Client.ID, parentID: e.view.ID, operationID: useID, target: use.SessionID, expires: r.clock().Add(30 * time.Second)}
	return secret, nil
}

// BindClientInstance validates optional metadata and freezes provenance for one
// accepted call. Without a proof old callers remain instance-unknown. Adapter entry
// is supplied by trusted composition, unlike the client's declared executable label.
func (a *Application) BindClientInstance(ctx context.Context, entry string) (context.Context, error) {
	proof, supplied := ctx.Value(instanceProofKey{}).(InstanceProof)
	if !supplied {
		return ctx, nil
	}
	r := &a.instances
	r.mu.Lock()
	defer r.mu.Unlock()
	e, err := r.authenticate(ctx, proof, false)
	if err != nil {
		return ctx, err
	}
	client := e.view.Client
	client.InstanceID, client.ParentInstanceID, client.OperationID = e.view.ID, e.view.ParentInstanceID, e.view.ParentOperationID
	client.Entry, client.DeclaredEntry = entry, e.view.DeclaredEntry
	if proof.UseID != "" {
		use, ok := e.uses[proof.UseID]
		if !ok || use.Kind != "operation" {
			return ctx, ErrClientInstance
		}
		client.OperationID = use.ID
	}
	return WithClientIdentity(ctx, client), nil
}

// BeginClientCall tracks a request against its resolved Session until completion.
// Its cleanup is idempotent, and does not clear captured provenance used by async work.
func (a *Application) BeginClientCall(ctx context.Context, target, label string, operation bool) (context.Context, func(), error) {
	client := ClientIdentityFromContext(ctx)
	if client.InstanceID == "" || target == "" && !operation {
		if a.idle.policy.SessionTimeout != 0 && (target != "" || operation) {
			if controlExternal(ctx) {
				return ctx, nil, ErrIdleRegistration
			}
			finish, err := a.beginIdleWork()
			return ctx, finish, err
		}
		return ctx, func() {}, nil
	}
	r := &a.instances
	r.mu.Lock()
	if a.idle.draining {
		r.mu.Unlock()
		return ctx, nil, ErrHostDraining
	}
	resolved := ""
	if target != "" {
		terminal, err := a.session(target)
		if err != nil {
			r.mu.Unlock()
			return ctx, func() {}, nil
		}
		resolved = terminal.ID()
	}
	e := r.entries[client.InstanceID]
	if e == nil || e.target != "" && e.target != resolved {
		r.mu.Unlock()
		return ctx, nil, ErrClientInstance
	}
	if proof, ok := ctx.Value(instanceProofKey{}).(InstanceProof); ok && proof.UseID != "" {
		use, exists := e.uses[proof.UseID]
		if !exists || use.SessionID != resolved {
			r.mu.Unlock()
			return ctx, nil, ErrClientInstance
		}
	}
	if len(e.uses) >= maxInstanceUses {
		r.mu.Unlock()
		return ctx, nil, ErrClientInstanceLimit
	}
	kind := "observe"
	if operation {
		kind = "operation"
	}
	use := InstanceUse{ID: "CU-" + rand.Text(), SessionID: resolved, Kind: kind, Label: label, Active: true}
	e.uses[use.ID] = use
	a.touchIdleLocked(resolved)
	if operation && client.OperationID == "" {
		client.OperationID = use.ID
	}
	r.mu.Unlock()
	var once sync.Once
	return WithClientIdentity(ctx, client), func() {
		once.Do(func() { r.mu.Lock(); delete(e.uses, use.ID); a.touchIdleLocked(resolved); r.mu.Unlock() })
	}, nil
}

// instanceLeaseUses snapshots existing authority without renewing or cancelling it.
// Lock ordering is lease snapshot first, then presence. Per-instance display is
// bounded; truncated snapshots must never be used to decide resource idleness.
func (a *Application) instanceLeaseUses() map[string][]InstanceUse {
	result := make(map[string][]InstanceUse)
	a.leases.mu.Lock()
	defer a.leases.mu.Unlock()
	add := func(lease SessionLease) {
		id := lease.Client.InstanceID
		if id == "" || len(result[id]) > maxInstanceUses {
			return
		}
		result[id] = append(result[id], InstanceUse{ID: "lease:" + lease.SessionID, SessionID: lease.SessionID, Kind: "operation", Label: string(lease.Type) + " lease", Active: true})
	}
	for _, lease := range a.leases.leases {
		add(lease)
	}
	for _, lease := range a.leases.recovering {
		add(lease)
	}
	return result
}
