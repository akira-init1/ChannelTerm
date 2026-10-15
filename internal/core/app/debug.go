package app

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/core/debugger"
	"github.com/akira-init1/ChannelTerm/internal/core/session"
)

const debugTimeout = 30 * time.Second
const debugClientTTL = 60 * time.Second

// DebugStartRequest starts one Linux all-stop inferior on an idle Bash Session.
// Owner is a caller-generated secret capability (at least 32 characters).
// RequestID must be reused unchanged after a lost response; conflicting reuse
// is rejected. GDB, GDBServer and ELF are absolute local paths. PID zero launches
// ELF; a positive PID attaches only after local terminal-isolation checks.
type DebugStartRequest struct {
	SessionID string   `json:"session_id"`
	Owner     string   `json:"owner"`
	RequestID string   `json:"request_id"`
	ELF       string   `json:"elf"`
	Args      []string `json:"args,omitempty"`
	PID       int      `json:"pid,omitempty"`
	GDB       string   `json:"gdb,omitempty"`
	GDBServer string   `json:"gdbserver,omitempty"`
}

// DebugStatus describes a debug operation without disclosing owner capabilities.
// Closed means the shell probe succeeded and its lease was released; failed
// means the Session was closed and must be reopened explicitly.
type DebugStatus struct {
	DebugID   string    `json:"debug_id"`
	SessionID string    `json:"session_id"`
	State     string    `json:"state"`
	Next      uint64    `json:"next"`
	ExpiresAt time.Time `json:"expires_at"`
	Error     string    `json:"error,omitempty"`
}

// DebugCommandRequest selects one supported MI action. RequestID makes retrying
// a mutation safe. Argument is one MI-quoted value, never a raw MI command.
// Multi-inferior and non-stop commands are deliberately absent.
type DebugCommandRequest struct {
	DebugID   string `json:"debug_id"`
	Owner     string `json:"owner"`
	RequestID string `json:"request_id"`
	Action    string `json:"action"`
	Argument  string `json:"argument,omitempty"`
}

// DebugEvent is an ordered MI record or a lifecycle transition.
type DebugEvent struct {
	Sequence uint64          `json:"sequence"`
	Record   debugger.Record `json:"record"`
}

// DebugEvents is a bounded event page. A stale cursor returns an explicit error;
// callers must never treat lost stop events as an unchanged inferior state.
type DebugEvents struct {
	Status DebugStatus  `json:"status"`
	Events []DebugEvent `json:"events"`
	Next   uint64       `json:"next"`
}

type debugCoordinator struct {
	mu       sync.Mutex
	runs     map[string]*debugRun
	requests map[string]*debugRun
}
type debugCommandResult struct {
	request DebugCommandRequest
	record  debugger.Record
	err     error
}
type debugRun struct {
	app                   *Application
	terminal              session.Session
	request               DebugStartRequest
	fingerprint           string
	id, owner, token, dir string
	op                    sync.Mutex // Serializes setup, MI actions, and recovery; never held by readers.
	mu                    sync.Mutex // State/events only; never held across I/O.
	state                 string
	err                   error
	expires               time.Time
	events                []DebugEvent
	eventBytes            int
	next                  uint64
	changed               chan struct{}
	commands              map[string]debugCommandResult
	commandBytes          int
	client                *debugger.Client
	listener              net.Listener
	bridgeCancel          context.CancelFunc
	bridgeDone            chan struct{}
	bridgeErr             error
	bridgeFinalACK        bool
	bridgeLastACK         bool
	bridgeStarted         bool
	protocol              *terminalCommandProtocol
	touched               bool
	finished              chan struct{}
	server                *debugProcess
}

func (c *debugCoordinator) owns(id, owner string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, r := range c.runs {
		if r.terminal.ID() == id && r.owner == owner {
			return true
		}
	}
	return false
}
func (c *debugCoordinator) get(id, owner string) (*debugRun, error) {
	c.mu.Lock()
	r := c.runs[id]
	c.mu.Unlock()
	if r == nil || subtle.ConstantTimeCompare([]byte(r.request.Owner), []byte(owner)) != 1 {
		return nil, errors.New("unknown debug operation or invalid owner")
	}
	return r, nil
}

// StartDebug takes over a Session, starts gdbserver and GDB/MI, and returns only
// after remote connection is established. Setup is bounded to 30 seconds.
// Cancellation after terminal mutation closes the Session unless recovery can
// be proven; a duplicate request returns the original operation, never launches
// another inferior. At most 128 operations are retained per Application.
func (a *Application) StartDebug(ctx context.Context, request DebugStartRequest) (DebugStatus, error) {
	if len(request.Owner) < 32 || len(request.Owner) > 128 || len(request.RequestID) < 1 || len(request.RequestID) > 128 {
		return DebugStatus{}, errors.New("owner must have 32..128 characters and request_id 1..128")
	}
	terminal, e := a.session(request.SessionID)
	if e != nil {
		return DebugStatus{}, e
	}
	request.SessionID = terminal.ID()
	encoded, _ := json.Marshal(request)
	key := request.SessionID + "\x00" + request.RequestID
	a.debug.mu.Lock()
	if r := a.debug.requests[key]; r != nil {
		a.debug.mu.Unlock()
		if r.fingerprint != string(encoded) {
			return DebugStatus{}, errors.New("debug request_id reused with different parameters")
		}
		r.op.Lock()
		defer r.op.Unlock()
		return r.status(), r.err
	}
	if len(a.debug.runs) >= 128 {
		a.debug.mu.Unlock()
		return DebugStatus{}, errors.New("debug operation retention limit reached; restart Host after closing Sessions")
	}
	token, e := newTerminalCommandToken()
	if e != nil {
		a.debug.mu.Unlock()
		return DebugStatus{}, e
	}
	owner, e := newTerminalCommandToken()
	if e != nil {
		a.debug.mu.Unlock()
		return DebugStatus{}, e
	}
	r := &debugRun{app: a, terminal: terminal, request: request, fingerprint: string(encoded), id: "DBG-" + token, token: token, owner: "debug-" + owner, state: "starting", expires: time.Now().Add(debugClientTTL), changed: make(chan struct{}), commands: map[string]debugCommandResult{}, finished: make(chan struct{})}
	r.op.Lock()
	defer r.op.Unlock()
	if a.debug.runs == nil {
		a.debug.runs = map[string]*debugRun{}
		a.debug.requests = map[string]*debugRun{}
	}
	a.debug.runs[r.id] = r
	a.debug.requests[key] = r
	a.debug.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, debugTimeout)
	defer cancel()
	if e = validateDebugStart(&r.request); e != nil {
		r.transition("failed", e)
		close(r.finished)
		return r.status(), e
	}
	if _, e = a.AcquireLease(terminal.ID(), r.owner, LeaseTypeDebug); e != nil {
		r.transition("failed", e)
		close(r.finished)
		return r.status(), e
	}
	if tracker, ok := terminal.(interface{ HasPendingInput() bool }); ok {
		if tracker.HasPendingInput() {
			e = ErrTerminalCommandInputPending
		}
	} else {
		var activity session.ActivityChunk
		activity, e = terminal.ReadRecentActivity(session.DefaultActivityBufferCapacity)
		if e == nil && (activity.Dropped || uint64(activity.Next) > uint64(len(activity.Events))) {
			e = errors.New("terminal input history incomplete; debug takeover refused")
		}
		if e == nil {
			var pending bool
			pending, e = terminalHasPendingInteractiveInput(terminal)
			if pending {
				e = ErrTerminalCommandInputPending
			}
		}
	}
	if e == nil {
		e = r.start(ctx)
	}
	if e != nil {
		r.fail(e)
		return r.status(), e
	}
	r.transition("stopped", nil)
	go r.monitor()
	return r.status(), nil
}
func (r *debugRun) status() DebugStatus { r.mu.Lock(); defer r.mu.Unlock(); return r.statusLocked() }
func (r *debugRun) statusLocked() DebugStatus {
	s := DebugStatus{DebugID: r.id, SessionID: r.terminal.ID(), State: r.state, Next: r.next, ExpiresAt: r.expires}
	if r.err != nil {
		s.Error = r.err.Error()
	}
	return s
}
func (r *debugRun) transition(state string, e error) {
	r.mu.Lock()
	r.state = state
	r.err = e
	r.appendLocked(debugger.Record{Kind: "lifecycle", Class: state})
	r.mu.Unlock()
	r.terminal.PublishEvent(session.Event{Type: session.EventDebugState, Actor: string(session.ActorSystem), Metadata: map[string]any{"debug_id": r.id, "state": state}})
}
func (r *debugRun) appendLocked(record debugger.Record) {
	encoded, _ := json.Marshal(record)
	r.events = append(r.events, DebugEvent{Sequence: r.next, Record: record})
	r.next++
	r.eventBytes += len(encoded)
	for len(r.events) > 512 || r.eventBytes > 4*1024*1024 {
		old, _ := json.Marshal(r.events[0].Record)
		r.eventBytes -= len(old)
		r.events[0] = DebugEvent{}
		r.events = r.events[1:]
	}
	close(r.changed)
	r.changed = make(chan struct{})
}
func (r *debugRun) record(record debugger.Record) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if (record.Kind == "*" || record.Kind == "^" && record.Class == "running") && (r.state == "starting" || r.state == "running" || r.state == "stopped") {
		switch record.Class {
		case "running":
			r.state = "running"
		case "stopped":
			r.state = "stopped"
			if reason, _ := record.Results["reason"].(string); strings.HasPrefix(reason, "exited") {
				r.state = "exited"
			}
		}
	}
	r.appendLocked(record)
}
func (r *debugRun) write(ctx context.Context, b []byte) error {
	_, e := r.app.writeSession(ctx, r.terminal.ID(), r.owner, session.WriteRequest{Actor: session.ActorSystem, Data: b})
	return e
}

func (r *debugRun) monitor() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	lastRenew := time.Now()
	for {
		select {
		case <-r.finished:
			return
		case <-ticker.C:
			s := r.status()
			if s.State == "exited" || time.Now().After(s.ExpiresAt) {
				r.op.Lock()
				current := r.status()
				ended := current.State == "closed" || current.State == "failed"
				expired := current.State == "exited" || time.Now().After(current.ExpiresAt)
				if !ended && expired {
					_ = r.stop(false)
				}
				r.op.Unlock()
				if ended || expired {
					return
				}
				continue
			}
			if r.terminal.State() != session.StateOpen {
				r.op.Lock()
				r.fail(errors.New("terminal disconnected during debugging"))
				r.op.Unlock()
				return
			}
			if time.Since(lastRenew) >= 10*time.Second {
				if _, e := r.app.RenewLease(r.terminal.ID(), r.owner); e != nil {
					r.op.Lock()
					r.fail(e)
					r.op.Unlock()
					return
				}
				if e := r.heartbeat(); e != nil {
					r.op.Lock()
					r.fail(e)
					r.op.Unlock()
					return
				}
				lastRenew = time.Now()
			}
		case <-r.bridgeDone:
			// GDB can close RSP before its MI reader receives the exit record.
			// Wait for that record, never infer successful exit from socket EOF.
			deadline := time.NewTimer(2 * time.Second)
			for {
				r.mu.Lock()
				state, changed, cause := r.state, r.changed, r.bridgeErr
				r.mu.Unlock()
				if cause != nil || state == "exited" || state == "closed" || state == "failed" || state == "recovering" {
					break
				}
				select {
				case <-changed:
					continue
				case <-deadline.C:
				}
				break
			}
			deadline.Stop()
			r.op.Lock()
			s := r.status()
			if s.State == "exited" {
				_ = r.stop(false)
			} else if s.State != "closed" && s.State != "failed" {
				r.mu.Lock()
				cause := r.bridgeErr
				r.mu.Unlock()
				if cause == nil {
					cause = errors.New("RSP peer disconnected before inferior exit")
				}
				r.fail(cause)
			}
			r.op.Unlock()
			return
		case <-r.client.Done():
			r.op.Lock()
			if s := r.status(); s.State != "closed" && s.State != "failed" {
				r.fail(errors.New("GDB exited unexpectedly"))
			}
			r.op.Unlock()
			return
		}
	}
}

// DebugCommand executes an allow-listed action with token correlation and
// retry deduplication. A timeout with uncertain execution fails the operation
// closed. Completed GDB errors remain retryable with a new request ID.
func (a *Application) DebugCommand(ctx context.Context, q DebugCommandRequest) (debugger.Record, error) {
	r, e := a.debug.get(q.DebugID, q.Owner)
	if e != nil {
		return debugger.Record{}, e
	}
	if q.RequestID == "" || len(q.RequestID) > 128 {
		return debugger.Record{}, errors.New("request_id is required (maximum 128 characters)")
	}
	command, e := debugMICommand(q.Action, q.Argument)
	if e != nil {
		return debugger.Record{}, e
	}
	r.op.Lock()
	defer r.op.Unlock()
	if old, ok := r.commands[q.RequestID]; ok {
		if old.request != q {
			return debugger.Record{}, errors.New("command request_id reused with different parameters")
		}
		return old.record, old.err
	}
	if len(r.commands) >= 4096 || r.commandBytes >= 16*1024*1024 {
		return debugger.Record{}, errors.New("debug command retention limit reached")
	}
	s := r.status()
	if s.State == "closed" || s.State == "failed" || s.State == "exited" {
		return debugger.Record{}, fmt.Errorf("debug operation is %s", s.State)
	}
	r.mu.Lock()
	if !time.Now().Before(r.expires) {
		r.mu.Unlock()
		return debugger.Record{}, errors.New("debug client heartbeat expired; wait for recovery")
	}
	r.expires = time.Now().Add(debugClientTTL)
	r.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, debugTimeout)
	defer cancel()
	var record debugger.Record
	if q.Action == "renew" {
		record = debugger.Record{Kind: "^", Class: "done"}
	} else {
		record, e = r.client.Command(ctx, command)
	}
	r.commands[q.RequestID] = debugCommandResult{request: q, record: record, err: e}
	encoded, _ := json.Marshal(record)
	r.commandBytes += len(encoded) + len(q.Argument) + len(q.RequestID)
	if e != nil && !debugger.IsCommandError(e) {
		r.fail(e)
	}
	return record, e
}

func debugMICommand(action, argument string) (string, error) {
	if len(argument) > 4096 || strings.ContainsAny(argument, "\x00\r\n") {
		return "", errors.New("invalid debug argument")
	}
	plain := map[string]string{"continue": "-exec-continue", "next": "-exec-next", "step": "-exec-step", "finish": "-exec-finish", "interrupt": "-exec-interrupt", "stack": "-stack-list-frames", "arguments": "-stack-list-arguments 1", "variables": "-stack-list-variables --all-values", "threads": "-thread-info", "registers": "-data-list-register-values x", "register_names": "-data-list-register-names", "renew": ""}
	if cmd, ok := plain[action]; ok {
		if argument != "" {
			return "", errors.New("action takes no argument")
		}
		return cmd, nil
	}
	switch action {
	case "break_insert":
		if argument != "" {
			return "-break-insert -- " + debugger.Quote(argument), nil
		}
	case "evaluate":
		if argument != "" {
			return "-data-evaluate-expression " + debugger.Quote(argument), nil
		}
	case "break_delete", "thread", "frame":
		if argument != "" && strings.Trim(argument, "0123456789") == "" {
			prefix := map[string]string{"break_delete": "-break-delete ", "thread": "-thread-select ", "frame": "-stack-select-frame "}
			return prefix[action] + argument, nil
		}
	}
	return "", errors.New("unsupported action or invalid argument; single inferior/all-stop only")
}

// ReadDebugEvents waits up to the caller's deadline for events after cursor.
// Reads renew the client heartbeat but not ownership after a terminal failure.
func (a *Application) ReadDebugEvents(ctx context.Context, id, owner string, cursor uint64) (DebugEvents, error) {
	r, e := a.debug.get(id, owner)
	if e != nil {
		return DebugEvents{}, e
	}
	for {
		r.mu.Lock()
		if time.Now().Before(r.expires) && r.state != "closed" && r.state != "failed" {
			r.expires = time.Now().Add(debugClientTTL)
		}
		base := r.next - uint64(len(r.events))
		if cursor < base || cursor > r.next {
			r.mu.Unlock()
			return DebugEvents{}, errors.New("debug event cursor lost or invalid; inspect status and stop/restart debugging")
		}
		if cursor < r.next || r.state == "closed" || r.state == "failed" {
			end := min(r.next, cursor+128)
			events := append([]DebugEvent{}, r.events[cursor-base:end-base]...)
			out := DebugEvents{Status: r.statusLocked(), Events: events, Next: end}
			r.mu.Unlock()
			return out, nil
		}
		changed := r.changed
		r.mu.Unlock()
		select {
		case <-ctx.Done():
			return DebugEvents{}, ctx.Err()
		case <-changed:
		}
	}
}

// StopDebug idempotently detaches by default. terminate is permitted only for
// an inferior launched by this operation. Recovery uses its own bounded context
// so a cancelled MCP request cannot release an unverified terminal.
func (a *Application) StopDebug(ctx context.Context, id, owner string, terminate bool) (DebugStatus, error) {
	r, e := a.debug.get(id, owner)
	if e != nil {
		return DebugStatus{}, e
	}
	if terminate && r.request.PID > 0 {
		return r.status(), errors.New("termination of an attached process is not supported; detach instead")
	}
	r.op.Lock()
	defer r.op.Unlock()
	s := r.status()
	if s.State == "closed" {
		return s, nil
	}
	if s.State == "failed" {
		return s, r.err
	}
	e = r.stop(terminate)
	return r.status(), e
}

func (r *debugRun) fail(cause error) {
	if s := r.status(); s.State == "closed" || s.State == "failed" {
		return
	}
	r.transition("failed", cause)
	if r.touched {
		_, _ = r.app.removeAndCloseSession(r.terminal.ID())
		r.app.leases.remove(r.terminal.ID())
	} else {
		_ = r.app.releaseLease(r.terminal, r.owner)
	}
	r.cleanup()
	close(r.finished)
}
func (r *debugRun) cleanup() {
	if r.server == nil && r.touched && r.dir != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		_ = r.openServer(ctx)
		cancel()
	}
	if r.bridgeCancel != nil {
		r.bridgeCancel()
	}
	if r.listener != nil {
		_ = r.listener.Close()
	}
	if r.client != nil {
		r.client.Close()
	}
	if r.bridgeStarted {
		<-r.bridgeDone
		r.bridgeStarted = false
	}
	if r.server != nil {
		r.server.close()
	}
	if r.dir != "" {
		_ = os.RemoveAll(r.dir)
	}
}
