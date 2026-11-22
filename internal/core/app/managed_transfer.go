package app

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/core/config"
	"github.com/akira-init1/ChannelTerm/internal/core/session"
)

// TransferRequest starts a Host-local transfer. Sources and Destination are
// literal paths, never shell expressions. Host paths are relative to the
// configured transfer root. Mode auto/files delivers ordinary files; tar and
// tar.gz explicitly deliver an archive. A single directory delivers its contents
// into Destination; multiple sources retain their distinct base names there.
type TransferRequest struct {
	RequestID      string   `json:"request_id"`
	Direction      string   `json:"direction"`
	Transport      string   `json:"transport"`
	SessionID      string   `json:"session_id,omitempty"`
	SSHProfile     string   `json:"ssh_profile,omitempty"`
	Sources        []string `json:"sources"`
	Destination    string   `json:"destination"`
	Mode           string   `json:"mode,omitempty"`
	TimeoutSeconds int      `json:"timeout_seconds,omitempty"`
}

// TransferStatus is a bounded snapshot, available without a terminal Session.
// Completed means verification and cleanup finished, including serial shell
// restoration and lease release. A failed/cancelled upload can leave remote
// partial data; ResolvedPath identifies the attempted destination when known.
type TransferStatus struct {
	// StopReason identifies an explicit human interruption without claiming cleanup has completed.
	StopReason        string                 `json:"stop_reason,omitempty"`
	SessionID         string                 `json:"session_id,omitempty"`
	Client            session.ClientIdentity `json:"client,omitzero"`
	ID                string                 `json:"transfer_id"`
	State             string                 `json:"state"`
	Phase             string                 `json:"phase"`
	Transport         string                 `json:"transport"`
	Mode              string                 `json:"mode"`
	WireMode          string                 `json:"wire_mode,omitempty"`
	Reason            string                 `json:"selection_reason,omitempty"`
	ResolvedPath      string                 `json:"resolved_path,omitempty"`
	SessionTransferID string                 `json:"session_transfer_id,omitempty"`
	CurrentFile       string                 `json:"current_file,omitempty"`
	CurrentBytes      int64                  `json:"current_bytes"`
	CurrentTotal      int64                  `json:"current_total"`
	Bytes             int64                  `json:"bytes"`
	TotalBytes        int64                  `json:"total_bytes"`
	FilesVerified     int                    `json:"files_verified"`
	FilesTotal        int                    `json:"files_total"`
	SHA256            string                 `json:"sha256,omitempty"`
	ChecksumScope     string                 `json:"checksum_scope,omitempty"`
	Error             string                 `json:"error,omitempty"`
}

// The coordinator bounds active I/O and retained retry records separately.
// Completed records are evicted oldest first only when admitting new requests.
// No worker holds this mutex during filesystem, network or terminal operations.
type transferCoordinator struct {
	mu       sync.Mutex
	settings config.FileTransfer
	runs     map[string]*transferRun
	closed   bool
}

type transferRun struct {
	connection  config.ManualConnection
	mu          sync.Mutex
	status      TransferStatus
	request     TransferRequest
	fingerprint string
	client      string
	created     time.Time
	cancel      context.CancelFunc
	done        chan struct{}
	app         *Application
	// Progress reporting is worker-owned; snapshots alone are read concurrently.
	reportProgress func(TransferStatus) error
	lastProgress   time.Time
}

// Separate SSH file channels have no Session proof. Fence matching agent
// workers after pausing the shell, using the same mutex as new admissions.
func (a *Application) cancelSSHTransfers(key config.ManualConnection) {
	if key.Transport != "ssh" {
		return
	}
	a.transfers.mu.Lock()
	defer a.transfers.mu.Unlock()
	for _, run := range a.transfers.runs {
		if run.connection == key && run.snapshot().Client.Kind == "agent" {
			run.cancel()
		}
	}
}

func (r *transferRun) snapshot() TransferStatus {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.status
}

func (r *transferRun) update(f func(*TransferStatus)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	f(&r.status)
}

// StartFileTransfer accepts an asynchronous operation. Request cancellation
// before admission has no effect; after admission the task survives disconnects.
// Retry the same request_id and exact arguments to retrieve the retained task.
// All external callers need write access; configured root/SSH grants are still
// required for trusted internal callers. Unregistered stdio is read-only.
// CloseFileTransfers belongs to composition.
func (a *Application) StartFileTransfer(ctx context.Context, q TransferRequest) (TransferStatus, error) {
	if err := RequireControlIdentity(ctx); err != nil {
		return TransferStatus{}, err
	}
	if err := CheckClientAccess(ctx, ClientWrite); err != nil {
		return TransferStatus{}, err
	}
	if err := ctx.Err(); err != nil {
		return TransferStatus{}, err
	}
	if err := validateTransferRequest(q); err != nil {
		return TransferStatus{}, err
	}
	q.Sources = append([]string(nil), q.Sources...)
	encoded, _ := json.Marshal(q)
	c := &a.transfers
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return TransferStatus{}, errors.New("file transfer service is closed")
	}
	if c.settings.Root == "" {
		return TransferStatus{}, errors.New("managed file transfer is disabled; configure file_transfer.root on the Host")
	}
	if !filepath.IsAbs(c.settings.Root) {
		return TransferStatus{}, errors.New("file_transfer.root must be an absolute Host directory")
	}
	client := ClientIdentityFromContext(ctx).ID
	active := 0
	var oldest *transferRun
	for _, r := range c.runs {
		if r.client == client && r.request.RequestID == q.RequestID {
			if r.fingerprint != string(encoded) {
				return TransferStatus{}, errors.New("request_id was already used with different arguments")
			}
			return r.snapshot(), nil
		}
		select {
		case <-r.done:
			if oldest == nil || r.created.Before(oldest.created) {
				oldest = r
			}
		default:
			active++
		}
	}
	if q.Transport == "serial" {
		if err := a.CheckSessionControl(ctx, q.SessionID); err != nil {
			return TransferStatus{}, err
		}
	}
	if active >= 4 {
		return TransferStatus{}, errors.New("four file transfers are already active; wait or cancel one")
	}
	if q.Transport == "serial" {
		serial := false
		for _, info := range a.ListSessions() {
			if (info.ID == q.SessionID || info.Metadata.Reference == q.SessionID) && info.Metadata.Transport == "serial" {
				serial = true
			}
		}
		if !serial {
			return TransferStatus{}, errors.New("serial transfer requires an active serial Session; use an SSH profile for SFTP/SCP")
		}
		ref, ok := a.manager.Reference(q.SessionID)
		if !ok {
			return TransferStatus{}, ErrSessionNotFound
		}
		q.SessionID = ref // A reference is never reused, even if an opaque ID is.
	} else if _, ok := c.settings.SSH[q.SSHProfile]; !ok {
		return TransferStatus{}, errors.New("unknown Host file-transfer SSH profile")
	}
	if q.Mode == "" {
		q.Mode = "auto"
	}
	if q.TimeoutSeconds == 0 {
		q.TimeoutSeconds = 3600
	}
	operationContext, finishUse, presenceErr := a.BeginClientCall(ctx, q.SessionID, "file_transfer", true)
	if presenceErr != nil {
		return TransferStatus{}, presenceErr
	}
	var preparation ControlProof
	if q.Transport == "serial" && controlExternal(ctx) {
		p, _ := ctx.Value(controlProofKey{}).(ControlProof)
		scope, err := a.beginControl(ctx, q.SessionID, p, managedScope)
		if err != nil {
			finishUse()
			return TransferStatus{}, err
		}
		preparation = ControlProof{Version: scope.Version, OperationID: scope.OperationID}
		operationContext = context.WithValue(WithControlProof(operationContext, preparation), controlWorkerKey{}, true)
	}
	worker, cancel := context.WithTimeout(context.WithoutCancel(operationContext), time.Duration(q.TimeoutSeconds)*time.Second)
	key := a.connectionFor(q.SessionID)
	if q.Transport != "serial" {
		profile := c.settings.SSH[q.SSHProfile]
		port := profile.Port
		if port == 0 {
			port = 22
		}
		key = connectionKey("ssh", profile.User+"@"+net.JoinHostPort(profile.Host, strconv.Itoa(port)))
	}
	if err := a.checkTransferPause(ctx, key); err != nil {
		cancel()
		finishUse()
		return TransferStatus{}, err
	}
	worker, finishAccess, accessErr := a.guardConnection(worker, key)
	if accessErr != nil {
		cancel()
		finishUse()
		return TransferStatus{}, accessErr
	}
	id := "FT-" + rand.Text()
	sessionID := ""
	if q.SessionID != "" {
		if terminal, lookupErr := a.session(q.SessionID); lookupErr == nil {
			sessionID = terminal.ID()
		}
	}
	r := &transferRun{connection: key, app: a, request: q, fingerprint: string(encoded), client: client, created: time.Now(), cancel: cancel, done: make(chan struct{}), status: TransferStatus{Client: ClientIdentityFromContext(operationContext), ID: id, State: "running", Phase: "preflight", Transport: q.Transport, Mode: q.Mode}}
	if c.runs == nil {
		c.runs = make(map[string]*transferRun)
	}
	if len(c.runs) >= 128 && oldest != nil {
		delete(c.runs, oldest.status.ID)
	}
	c.runs[id] = r
	r.status.SessionID = sessionID
	finishPreparation := func() {}
	if preparation.Version != "" {
		finishPreparation = a.bindPreparingControl(sessionID, preparation, cancel)
	}
	// Host-owned preparation renews its own captured scope. Presence expiration
	// does not cancel accepted work, while takeover still fences every admission.
	controlDone := make(chan struct{})
	if preparation.Version != "" {
		go func() {
			defer close(controlDone)
			ticker := time.NewTicker(10 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-worker.Done():
					return
				case <-ticker.C:
					a.leases.mu.Lock()
					current := a.leases.controls.entries[sessionID]
					live := current != nil && current.view.Version == preparation.Version && current.view.OperationID == preparation.OperationID && current.view.State == "active"
					if live {
						a.renewControlLocked(current)
					}
					a.leases.mu.Unlock()
					if !live {
						cancel()
						return
					}
				}
			}
		}()
	} else {
		close(controlDone)
	}
	go func() {
		defer func() { cancel(); <-controlDone }()
		defer finishAccess()
		defer finishPreparation()
		defer finishUse()
		defer close(r.done)
		defer cancel()
		err := r.execute(worker, c.settings)
		r.update(func(s *TransferStatus) {
			s.Phase = "finished"
			s.State = "completed"
			if err != nil {
				s.State, s.Error = "failed", err.Error()
				if errors.Is(err, context.Canceled) {
					s.State = "cancelled"
				}
			}
		})
	}()
	return r.snapshot(), nil
}

// FileTransferStatus observes or cancels only the authenticated caller's task.
// wait is bounded to 30 seconds; its timeout returns the latest running snapshot
// without cancelling the task. Cancel is idempotent and still requires cleanup.
func (a *Application) FileTransferStatus(ctx context.Context, id string, cancel bool, wait time.Duration) (status TransferStatus, statusErr error) {
	effect := ClientRead
	if cancel {
		effect = ClientWrite
	}
	if err := CheckClientAccess(ctx, effect); err != nil {
		return TransferStatus{}, err
	}
	if wait < 0 || wait > 30*time.Second {
		return TransferStatus{}, errors.New("wait must be between zero and 30 seconds")
	}
	a.transfers.mu.Lock()
	r := a.transfers.runs[id]
	a.transfers.mu.Unlock()
	if r == nil || r.client != ClientIdentityFromContext(ctx).ID {
		return TransferStatus{}, errors.New("unknown file transfer for this client")
	}
	ctx, finishAccess, accessErr := a.guardConnection(ctx, r.connection)
	if accessErr != nil {
		return TransferStatus{}, accessErr
	}
	defer func() {
		if denied := finishAccess(); denied != nil {
			status = TransferStatus{}
			statusErr = denied
		}
	}()

	if cancel {
		r.cancel()
	}
	if wait > 0 {
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case <-r.done:
		case <-timer.C:
		case <-ctx.Done():
			return TransferStatus{}, ctx.Err()
		}
	}
	return r.snapshot(), nil
}

// TransferSummary is a public observation, excluding paths, profiles, errors and capabilities.
// It conveys no right to query private status; interruption requires separate human management authority.
type TransferSummary struct {
	StopReason        string                 `json:"stop_reason,omitempty"`
	ID                string                 `json:"transfer_id"`
	SessionID         string                 `json:"session_id,omitempty"`
	SessionTransferID string                 `json:"session_transfer_id,omitempty"`
	Client            session.ClientIdentity `json:"client,omitzero"`
	Transport         string                 `json:"transport"`
	Direction         string                 `json:"direction"`
	State             string                 `json:"state"`
	Phase             string                 `json:"phase"`
	Bytes             int64                  `json:"bytes"`
	TotalBytes        int64                  `json:"total_bytes"`
}

// ListFileTransfers copies at most 128 retained task summaries without renewing,
// cancelling or waiting for I/O. The existing coordinator bounds retention.
func (a *Application) ListFileTransfers(ctx context.Context) ([]TransferSummary, error) {
	if err := CheckClientAccess(ctx, ClientRead); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	a.transfers.mu.Lock()
	defer a.transfers.mu.Unlock()
	rows := make([]TransferSummary, 0, len(a.transfers.runs))
	for _, r := range a.transfers.runs {
		rows = append(rows, r.summary())
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
	return rows, nil
}

// InterruptSSHFileTransfer requests cancellation of one immutable managed SSH task
// for a live user instance with management permission. It never touches a terminal
// Session or returns private paths. Completion can win the race; a running result
// means cleanup is still pending. Query ListFileTransfers for the final state.
func (a *Application) InterruptSSHFileTransfer(ctx context.Context, id string) (TransferSummary, error) {
	if err := requireHumanControl(ctx); err != nil {
		return TransferSummary{}, err
	}
	if err := a.validateControlIdentity(ctx, ""); err != nil {
		return TransferSummary{}, err
	}
	if len(id) < 19 || len(id) > 67 || !strings.HasPrefix(id, "FT-") {
		return TransferSummary{}, errors.New("invalid managed SSH task ID")
	}
	a.transfers.mu.Lock()
	r := a.transfers.runs[id]
	a.transfers.mu.Unlock()
	if r == nil {
		return TransferSummary{}, errors.New("managed SSH task unavailable")
	}
	if r.request.Transport != "sftp" && r.request.Transport != "scp" {
		return TransferSummary{}, errors.New("serial tasks require exact Session takeover")
	}
	// Keep the captured worker pointer even if retention changes concurrently.
	// cancel is idempotent and does not claim that remote background work stopped.
	r.update(func(s *TransferStatus) {
		if s.State == "running" {
			s.StopReason = "user_interrupted"
		}
	})
	r.cancel()
	return r.summary(), nil
}
func (r *transferRun) summary() TransferSummary {
	s := r.snapshot()
	return TransferSummary{StopReason: s.StopReason, ID: s.ID, SessionID: s.SessionID, SessionTransferID: s.SessionTransferID, Client: s.Client, Transport: s.Transport, Direction: r.request.Direction, State: s.State, Phase: s.Phase, Bytes: s.Bytes, TotalBytes: s.TotalBytes}
}

// CloseFileTransfers prevents admission, cancels and joins all managed workers.
// It is idempotent. Composition must call it before closing the Session Manager;
// allow at least the serial recovery timeout before forcing Host shutdown.
func (a *Application) CloseFileTransfers(ctx context.Context) error {
	c := &a.transfers
	c.mu.Lock()
	c.closed = true
	runs := make([]*transferRun, 0, len(c.runs))
	for _, r := range c.runs {
		r.cancel()
		runs = append(runs, r)
	}
	c.mu.Unlock()
	for _, r := range runs {
		select {
		case <-r.done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

func validateTransferRequest(q TransferRequest) error {
	if len(q.RequestID) < 1 || len(q.RequestID) > 128 {
		return errors.New("request_id must be 1..128 characters")
	}
	if q.Direction != "send" && q.Direction != "receive" {
		return errors.New("direction must be send or receive")
	}
	if q.Transport != "serial" && q.Transport != "sftp" && q.Transport != "scp" {
		return errors.New("transport must be serial, sftp or scp")
	}
	if q.Transport == "serial" && (q.SessionID == "" || q.SSHProfile != "") || q.Transport != "serial" && (q.SSHProfile == "" || q.SessionID != "") {
		return errors.New("serial requires only session_id; SSH requires only ssh_profile")
	}
	if q.Mode != "" && q.Mode != "auto" && q.Mode != "files" && q.Mode != "tar" && q.Mode != "tar.gz" {
		return errors.New("mode must be auto, files, tar or tar.gz")
	}
	if q.TimeoutSeconds < 0 || q.TimeoutSeconds > 86400 {
		return errors.New("timeout_seconds must be 1..86400 or omitted")
	}
	if len(q.Sources) < 1 || len(q.Sources) > 128 {
		return errors.New("sources must contain 1..128 paths")
	}
	seen := map[string]bool{}
	for _, source := range q.Sources {
		local := q.Direction == "send"
		if err := validateManagedPath(source, local); err != nil {
			return err
		}
		name := path.Base(strings.ReplaceAll(source, "\\", "/"))
		key := strings.ToLower(name)
		if seen[key] {
			return fmt.Errorf("source base names must be distinct: %q", name)
		}
		seen[key] = true
	}
	return validateManagedPath(q.Destination, q.Direction == "receive")
}

func validateManagedPath(name string, local bool) error {
	if len(name) == 0 || len(name) > 2048 || strings.ContainsAny(name, "\x00\r\n") {
		return errors.New("paths must be nonempty, line-safe and at most 2048 bytes")
	}
	if local {
		if !filepath.IsLocal(name) || filepath.Clean(name) == "." {
			return errors.New("Host paths must be relative children of file_transfer.root")
		}
	} else if !strings.HasPrefix(name, "/") || path.Clean(name) != name || name == "/" || strings.Contains(name, "\\") {
		return errors.New("remote paths must be absolute, normalized POSIX child paths")
	}
	return nil
}
