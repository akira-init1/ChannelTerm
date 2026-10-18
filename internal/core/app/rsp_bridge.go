package app

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/core/debugger"
	"github.com/akira-init1/ChannelTerm/internal/core/session"
)

const bridgeReadinessWarning = "target RSP readiness is unverified: passive Session inspection cannot distinguish a silent gdbserver from a shell; prepare the target first and explicitly acknowledge this risk"

// bridgeMaxLifetime bounds even a live but abandoned external helper. Neither
// automatic nor explicit renewal can extend an operation past this deadline.
const bridgeMaxLifetime = 24 * time.Hour

var errBridgeSessionIO = errors.New("bridge Session I/O failed")

// BridgeStartRequest leases an already prepared serial Session without sending
// target commands. Owner authenticates management calls; RequestID deduplicates
// exact retries. AcknowledgeUnverifiedTarget confirms out-of-band preparation,
// not protocol detection. No GDB or gdbserver is started or controlled.
type BridgeStartRequest struct {
	SessionID                   string `json:"session_id"`
	Owner                       string `json:"owner"`
	RequestID                   string `json:"request_id"`
	ListenAddress               string `json:"listen_address,omitempty"`
	AllowedPeer                 string `json:"allowed_peer,omitempty"`
	StrictPortPool              bool   `json:"strict_port_pool,omitempty"`
	AcknowledgeUnverifiedTarget bool   `json:"acknowledge_unverified_target"`
}

// BridgeStatus separates transport lifecycle from target and shell state.
// Disconnected retains the lease; restored alone proves a fresh shell probe
// and exact caller-supplied TTY-state readback. Closed/failed close the Session.
// AutoKeepalive reflects recent control evidence. MaxExpiresAt is the absolute
// lifetime bound, independent of manual or automatic management renewal.
type BridgeStatus struct {
	BridgeID      string    `json:"bridge_id"`
	SessionID     string    `json:"session_id"`
	State         string    `json:"state"`
	Address       string    `json:"address"`
	ExpiresAt     time.Time `json:"expires_at"`
	MaxExpiresAt  time.Time `json:"max_expires_at"`
	AutoKeepalive bool      `json:"auto_keepalive"`
	TargetState   string    `json:"target_state"`
	Warning       string    `json:"warning,omitempty"`
	Error         string    `json:"error,omitempty"`
}

// BridgeStartResult includes secret connection material only in the authorized
// start response. Store Connection privately for the external rsp-client helper.
type BridgeStartResult struct {
	Status     BridgeStatus               `json:"status"`
	Connection *debugger.BridgeConnection `json:"connection,omitempty"`
}

type bridgeCoordinator struct {
	endpoints debugger.EndpointAllocator
	mu        sync.Mutex
	runs      map[string]*bridgeRun
	requests  map[string]*bridgeRun
}

type bridgeRun struct {
	app         *Application
	terminal    session.Session
	request     BridgeStartRequest
	fingerprint string
	id, owner   string
	op          sync.Mutex // Management mutations; never held by the relay worker.
	mu          sync.Mutex // Status only; no blocking I/O while held.
	status      BridgeStatus
	endpoint    *debugger.Endpoint
	cancel      context.CancelFunc
	workerDone  chan struct{}
	finished    chan struct{}
	relayErr    error
	// Report deduplication is bounded to the latest accepted sequence, under op.
	reportSequence uint64
	reportDigest   [32]byte
}

func (c *bridgeCoordinator) owns(id, owner string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, r := range c.runs {
		if r.terminal.ID() == id && r.owner == owner {
			return true
		}
	}
	return false
}

func (c *bridgeCoordinator) get(id, owner string) (*bridgeRun, error) {
	c.mu.Lock()
	r := c.runs[id]
	c.mu.Unlock()
	if r == nil || subtle.ConstantTimeCompare([]byte(r.request.Owner), []byte(owner)) != 1 {
		return nil, errors.New("unknown bridge or invalid owner")
	}
	return r, nil
}

// StartRSPBridge exposes one prepared serial Session through authenticated TLS
// over TCP. It checks open state and pending input under an exclusive lease,
// but does not send an RSP probe or infer readiness from silence/history. Output
// begins at the current cursor, never by replaying terminal history. Setup errors
// release the lease without mutating target bytes. Retention is bounded to 128.
func (a *Application) StartRSPBridge(ctx context.Context, q BridgeStartRequest) (BridgeStartResult, error) {
	if err := ctx.Err(); err != nil {
		return BridgeStartResult{}, err
	}
	if len(q.Owner) < 32 || len(q.Owner) > 128 || len(q.RequestID) == 0 || len(q.RequestID) > 128 {
		return BridgeStartResult{}, errors.New("owner must have 32..128 characters and request_id 1..128")
	}
	encoded, _ := json.Marshal(q)
	key := q.Owner + "\x00" + q.RequestID
	a.bridges.mu.Lock()
	if r := a.bridges.requests[key]; r != nil {
		a.bridges.mu.Unlock()
		if r.fingerprint != string(encoded) {
			return BridgeStartResult{}, errors.New("bridge request_id reused with different parameters")
		}
		r.op.Lock()
		defer r.op.Unlock()
		return r.startResult(), nil
	}
	defer a.bridges.mu.Unlock()
	if !q.AcknowledgeUnverifiedTarget {
		return BridgeStartResult{}, errors.New(bridgeReadinessWarning)
	}
	if len(a.bridges.runs) >= 128 {
		return BridgeStartResult{}, errors.New("bridge retention limit reached; restart Host after closing Sessions")
	}
	terminal, err := a.session(q.SessionID)
	if err != nil {
		return BridgeStartResult{}, err
	}
	serial := false
	for _, info := range a.manager.ListInfo() {
		if info.ID == terminal.ID() && info.Metadata.Transport == serialTransportName {
			serial = true
		}
	}
	if !serial || terminal.State() != session.StateOpen {
		return BridgeStartResult{}, errors.New("RSP bridge requires an open serial Session")
	}
	id, err := newTerminalCommandToken()
	if err != nil {
		return BridgeStartResult{}, err
	}
	secret, err := newTerminalCommandToken()
	if err != nil {
		return BridgeStartResult{}, err
	}
	owner := "rsp-bridge-" + secret
	if _, err = a.AcquireLease(terminal.ID(), owner, LeaseTypeDebug); err != nil {
		return BridgeStartResult{}, err
	}
	release := true
	defer func() {
		if release {
			_ = a.releaseLease(terminal, owner)
		}
	}()
	if err = checkDebugPendingInput(terminal); err != nil {
		return BridgeStartResult{}, err
	}
	endpoint, err := a.bridges.endpoints.Listen(q.ListenAddress, q.AllowedPeer, q.StrictPortPool)
	if err != nil {
		return BridgeStartResult{}, err
	}
	if err = ctx.Err(); err != nil {
		endpoint.Close()
		return BridgeStartResult{}, err
	}
	workerCtx, cancel := context.WithCancel(context.Background())
	r := &bridgeRun{app: a, terminal: terminal, request: q, fingerprint: string(encoded), id: "RSP-" + id, owner: owner, endpoint: endpoint, cancel: cancel, workerDone: make(chan struct{}), finished: make(chan struct{})}
	now := time.Now()
	r.status = BridgeStatus{BridgeID: r.id, SessionID: terminal.ID(), State: "listening", Address: endpoint.Connection().Address, ExpiresAt: now.Add(debugClientTTL), MaxExpiresAt: now.Add(bridgeMaxLifetime), TargetState: "unverified", Warning: bridgeReadinessWarning}
	if a.bridges.runs == nil {
		a.bridges.runs = make(map[string]*bridgeRun)
		a.bridges.requests = make(map[string]*bridgeRun)
	}
	a.bridges.runs[r.id] = r
	a.bridges.requests[key] = r
	cursor := sessionOutputCursor(terminal)
	release = false
	r.publish("listening")
	go r.relay(workerCtx, cursor)
	go r.monitor()
	return r.startResult(), nil
}

// checkDebugPendingInput shares takeover checks with the managed MI backend.
// The persistent Core summary remains reliable after activity-buffer overwrite.
func checkDebugPendingInput(terminal session.Session) error {
	if tracker, ok := terminal.(interface{ HasPendingInput() bool }); ok {
		if tracker.HasPendingInput() {
			return ErrTerminalCommandInputPending
		}
		return nil
	}
	activity, err := terminal.ReadRecentActivity(session.DefaultActivityBufferCapacity)
	if err != nil {
		return err
	}
	if activity.Dropped || uint64(activity.Next) > uint64(len(activity.Events)) {
		return errors.New("terminal input history incomplete; debug takeover refused")
	}
	pending, err := terminalHasPendingInteractiveInput(terminal)
	if pending {
		return ErrTerminalCommandInputPending
	}
	return err
}

func (r *bridgeRun) snapshot() BridgeStatus {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.status
}

func (r *bridgeRun) startResult() BridgeStartResult {
	info := r.endpoint.Connection()
	return BridgeStartResult{Status: r.snapshot(), Connection: &info}
}

func (r *bridgeRun) publish(state string) {
	r.terminal.PublishEvent(session.Event{Type: session.EventDebugState, Actor: string(session.ActorSystem), Metadata: map[string]any{"bridge_id": r.id, "state": state}})
}

func (r *bridgeRun) setState(state string, err error) {
	r.mu.Lock()
	r.status.State = state
	if state != "connected" {
		r.status.AutoKeepalive = false
	}
	r.status.Error = ""
	if err != nil {
		r.status.Error = err.Error()
	}
	if state == "restored" {
		r.status.TargetState = "shell_verified"
		r.status.Warning = ""
	}
	r.mu.Unlock()
	r.publish(state)
}

func (r *bridgeRun) relay(ctx context.Context, cursor session.OutputCursor) {
	defer close(r.workerDone)
	conn, err := r.endpoint.Accept(ctx)
	if err == nil {
		// Only the worker transitions listening -> connected -> disconnected.
		// Management joins it before publishing a final state.
		r.setState("connected", nil)
		err = debugger.Relay(ctx, conn, &bridgeSessionStream{run: r, cursor: cursor})
		conn.Close()
		err = errors.Join(err, r.endpoint.ClientError())
	}
	r.mu.Lock()
	r.relayErr = err
	r.mu.Unlock()
	r.setState("disconnected", err)
}

type bridgeSessionStream struct {
	run    *bridgeRun
	cursor session.OutputCursor
}

func (s *bridgeSessionStream) Read(ctx context.Context) ([]byte, bool, error) {
	chunk, err := s.run.terminal.ReadOutput(ctx, s.cursor, 32*1024)
	s.cursor = chunk.Next
	if err != nil && !errors.Is(err, context.Canceled) {
		err = fmt.Errorf("%w: %w", errBridgeSessionIO, err)
	}
	return chunk.Data, chunk.Dropped, err
}

func (s *bridgeSessionStream) Write(ctx context.Context, data []byte) error {
	_, err := s.run.app.writeSession(ctx, s.run.terminal.ID(), s.run.owner, session.WriteRequest{Actor: session.ActorSystem, Data: data})
	if err != nil && !errors.Is(err, context.Canceled) {
		err = fmt.Errorf("%w: %w", errBridgeSessionIO, err)
	}
	return err
}

func (r *bridgeRun) monitor() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.finished:
			return
		case <-ticker.C:
			r.maintain()
		}
	}
}

// maintain serializes with explicit management. The internal lease is checked
// before extending management time; failed renewal can never report a healthy
// bridge or reopen ordinary writes. Listening/disconnected/legacy clients still
// need explicit management renewal. It is also called by deterministic tests.
func (r *bridgeRun) maintain() {
	r.op.Lock()
	defer r.op.Unlock()
	s := r.snapshot()
	if bridgeEnded(s.State) {
		return
	}
	r.mu.Lock()
	cause := r.relayErr
	r.mu.Unlock()
	now := time.Now()
	switch {
	case errors.Is(cause, debugger.ErrDataLost), errors.Is(cause, errBridgeSessionIO):
	case r.terminal.State() != session.StateOpen:
		cause = errors.New("serial Session disconnected during RSP bridge")
	case !now.Before(s.MaxExpiresAt):
		cause = errors.New("bridge maximum lifetime reached; Session closed without claiming shell restoration")
	case !now.Before(s.ExpiresAt):
		cause = errors.New("bridge management heartbeat expired; Session closed without claiming shell restoration")
	default:
		// LeaseStatus itself expires stale leases fail-closed. Renew before the
		// remaining time drops below twenty seconds, rather than trusting a
		// monitor-local timer that could outlive the underlying lease.
		lease, active, err := r.app.LeaseStatus(r.terminal.ID())
		cause = err
		if cause == nil && (!active || lease.Owner != r.owner) {
			cause = ErrLeaseNotOwned
		}
		if cause == nil && time.Until(lease.ExpiresAt) <= 20*time.Second {
			_, cause = r.app.RenewLease(r.terminal.ID(), r.owner)
		}
		now = time.Now()
		if cause == nil && (!now.Before(s.ExpiresAt) || !now.Before(s.MaxExpiresAt)) {
			cause = errors.New("bridge expired while renewing the internal lease")
		}
		if cause == nil {
			r.mu.Lock()
			r.status.AutoKeepalive = r.status.State == "connected" && r.endpoint.ClientAlive()
			if r.status.AutoKeepalive {
				r.status.ExpiresAt = minBridgeExpiry(now.Add(debugClientTTL), s.MaxExpiresAt)
			}
			r.mu.Unlock()
		}
	}
	if cause != nil {
		r.end("failed", cause)
	}
}

func minBridgeExpiry(expiry, maximum time.Time) time.Time {
	if expiry.After(maximum) {
		return maximum
	}
	return expiry
}

func bridgeEnded(state string) bool {
	return state == "closed" || state == "failed" || state == "restored"
}

// end runs under op. Closing the Session precedes joining the relay because a
// Channel write may ignore context cancellation and require Close to unblock.
func (r *bridgeRun) end(state string, cause error) {
	if bridgeEnded(r.snapshot().State) {
		return
	}
	r.cancel()
	if _, err := r.app.removeAndCloseSession(r.terminal.ID()); err != nil {
		state = "failed"
		cause = errors.Join(cause, fmt.Errorf("close serial Session: %w", err))
	}
	r.app.leases.remove(r.terminal.ID())
	<-r.workerDone
	r.endpoint.Close()
	r.setState(state, cause)
	close(r.finished)
}

// RSPBridgeStatus reads lifecycle state without renewing the management TTL.
func (a *Application) RSPBridgeStatus(id, owner string) (BridgeStatus, error) {
	r, err := a.bridges.get(id, owner)
	if err != nil {
		return BridgeStatus{}, err
	}
	return r.snapshot(), nil
}

// RenewRSPBridge renews the internal lease before extending the 60-second
// management TTL, capped by the absolute lifetime. An expired or ended operation
// cannot be resurrected, even if the timer has not run yet.
func (a *Application) RenewRSPBridge(id, owner string) (BridgeStatus, error) {
	r, err := a.bridges.get(id, owner)
	if err != nil {
		return BridgeStatus{}, err
	}
	r.op.Lock()
	defer r.op.Unlock()
	s := r.snapshot()
	if bridgeEnded(s.State) || !time.Now().Before(s.ExpiresAt) || !time.Now().Before(s.MaxExpiresAt) {
		return s, errors.New("bridge is ended or heartbeat expired")
	}
	if _, err := a.RenewLease(r.terminal.ID(), r.owner); err != nil {
		r.end("failed", err)
		return r.snapshot(), err
	}
	if !time.Now().Before(s.ExpiresAt) || !time.Now().Before(s.MaxExpiresAt) {
		err := errors.New("bridge expired while renewing the internal lease")
		r.end("failed", err)
		return r.snapshot(), err
	}
	r.mu.Lock()
	r.status.ExpiresAt = minBridgeExpiry(time.Now().Add(debugClientTTL), s.MaxExpiresAt)
	r.mu.Unlock()
	return r.snapshot(), nil
}

// CloseRSPBridge idempotently closes both the bridge and its serial Session.
// It never sends RSP commands or claims shell recovery. Use RestoreRSPBridge
// after externally ending RSP to preserve a verified terminal Session instead.
func (a *Application) CloseRSPBridge(id, owner string) (BridgeStatus, error) {
	r, err := a.bridges.get(id, owner)
	if err != nil {
		return BridgeStatus{}, err
	}
	r.op.Lock()
	defer r.op.Unlock()
	r.end("closed", nil)
	status := r.snapshot()
	if status.State == "failed" {
		return status, errors.New(status.Error)
	}
	return status, nil
}

// RestoreRSPBridge requires explicit confirmation that the target has returned
// to its shell and the exact stty -g value saved before target preparation.
// It refuses an active client. A fresh split-token probe applies and reads back
// that TTY state; only success releases the lease. Recovery has an independent
// ten-second deadline; uncertainty closes the Session. No RSP detach is injected.
func (a *Application) RestoreRSPBridge(id, owner, ttyState string, confirmShellReady bool) (BridgeStatus, error) {
	r, err := a.bridges.get(id, owner)
	if err != nil {
		return BridgeStatus{}, err
	}
	if !confirmShellReady || ttyState == "" || len(ttyState) > 512 || validateTerminalCommand(ttyState) != nil {
		return r.snapshot(), errors.New("restore requires confirm_shell_ready and the saved pre-debug stty -g value (1..512 printable bytes)")
	}
	r.op.Lock()
	defer r.op.Unlock()
	s := r.snapshot()
	if s.State == "restored" {
		return s, nil
	}
	if bridgeEnded(s.State) || !time.Now().Before(s.ExpiresAt) {
		return s, errors.New("bridge is ended or heartbeat expired")
	}
	if s.State != "disconnected" {
		return s, errors.New("disconnect the external GDB client before requesting shell restoration")
	}
	<-r.workerDone
	r.mu.Lock()
	relayErr := r.relayErr
	r.mu.Unlock()
	if errors.Is(relayErr, debugger.ErrDataLost) || errors.Is(relayErr, errBridgeSessionIO) || r.terminal.State() != session.StateOpen {
		err = errors.New("bridge lost serial data or its Session; restoration cannot be confirmed")
		r.end("failed", err)
		return r.snapshot(), err
	}
	r.setState("recovering", nil)
	// TODO: Diagnose the reported serial-board restore timeout after GDB detach.
	// The root cause is unconfirmed; see docs/development/known-issues.md.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// Closing the Channel on timeout also bounds a blocked probe write.
	closed := make(chan struct{})
	stop := context.AfterFunc(ctx, func() { r.terminal.Close(); close(closed) })
	token, err := newTerminalCommandToken()
	if err == nil {
		p := &terminalCommandProtocol{terminal: r.terminal, cursor: sessionOutputCursor(r.terminal), token: token}
		command := rspBridgeRestoreCommand(ttyState, token)
		_, err = a.writeSession(ctx, r.terminal.ID(), r.owner, session.WriteRequest{Actor: session.ActorSystem, Data: []byte(command)})
		if err == nil {
			_, err = p.waitStart(ctx)
		}
	}
	if !stop() {
		<-closed
		err = ctx.Err()
	}
	if err == nil && r.terminal.State() != session.StateOpen {
		err = errors.New("serial Session closed during recovery")
	}
	if err == nil {
		err = a.releaseLease(r.terminal, r.owner)
	}
	if err != nil {
		err = fmt.Errorf("shell/TTY restoration could not be verified; Session closed: %w", err)
		r.end("failed", err)
		return r.snapshot(), err
	}
	r.cancel()
	r.endpoint.Close()
	r.setState("restored", nil)
	close(r.finished)
	return r.snapshot(), nil
}

// rspBridgeRestoreCommand verifies the saved TTY state before emitting the
// split-token response, so neither input echo nor a failed stty proves recovery.
func rspBridgeRestoreCommand(ttyState, token string) string {
	quoted := quoteTerminalCommand(ttyState)
	splitToken := quoteSplitTerminalCommandToken(token)
	// Only delete a Bash entry containing this fresh probe's literal split token.
	// HISTIGNORE or disabled history can leave a user's command as the last entry;
	// blindly deleting HISTCMD would remove it. Other shells keep normal history.
	cleanup := "if [ -n \"${BASH_VERSION-}\" ];then case \"$(HISTTIMEFORMAT= builtin history 1)\" in *" + quoteTerminalCommand(splitToken) + "*) builtin " + bashDeleteCurrentHistoryEntry + ";;esac;fi;"
	return cleanup + "stty " + quoted + " && [ \"$(stty -g)\" = " + quoted + " ] && printf '\\036\\037%s\\037\\036' " + splitToken + "\n"
}

// ConnectRSPBridge connects a byte-stream adapter and its independent control
// heartbeat without starting or controlling GDB. ctx bounds dialing/authentication;
// after return the caller owns both sockets through the stream's Close method.
// Secret descriptors must remain private.
func ConnectRSPBridge(ctx context.Context, info debugger.BridgeConnection) (io.ReadWriteCloser, error) {
	return debugger.DialBridgeKeepAlive(ctx, info)
}
