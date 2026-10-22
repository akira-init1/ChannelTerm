package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/core/session"
)

var (
	// ErrInvalidUBootRequest identifies invalid settings before leases or writes.
	ErrInvalidUBootRequest = errors.New("invalid U-Boot request")
	// ErrUBootFailed means entry was not confirmed by fresh terminal evidence.
	ErrUBootFailed = errors.New("U-Boot entry not confirmed")
)

// UBootRequest defines a single bounded serial boot interruption. Reboot mode
// requires the caller to have verified an idle interactive Bash prompt. Capture
// explicitly opts into retaining raw terminal contents in a diagnostic archive.
type UBootRequest struct {
	Mode                string
	RebootCommand       string
	CountdownPattern    string
	PromptPattern       string
	BootStartedPatterns []string
	Interrupt           []byte
	Timeout             time.Duration
	Capture             bool
}

// UBootResult records observed evidence, not merely successful writes. Cursors
// count raw Session bytes; elapsed time is measured by the Host. Matched flags
// disambiguate a real match at cursor zero from no match. DiagnosticError never
// contains raw terminal contents.
type UBootResult struct {
	OperationID      string `json:"operation_id"`
	SessionID        string `json:"session_id"`
	Status           string `json:"status"`
	OutputStart      uint64 `json:"output_start"`
	OutputEnd        uint64 `json:"output_end"`
	CountdownCursor  uint64 `json:"countdown_cursor"`
	PromptCursor     uint64 `json:"prompt_cursor"`
	CountdownMatched bool   `json:"countdown_matched"`
	PromptMatched    bool   `json:"prompt_matched"`
	BytesWritten     int    `json:"bytes_written"`
	ElapsedMS        int64  `json:"elapsed_ms"`
	Dropped          bool   `json:"dropped"`
	DiagnosticID     string `json:"diagnostic_id,omitempty"`
	DiagnosticError  string `json:"diagnostic_error,omitempty"`
}

func validateUBootRequest(r *UBootRequest) error {
	invalid := func(reason string) error { return fmt.Errorf("%w: %s", ErrInvalidUBootRequest, reason) }
	if r.Mode != "wait_reset" && r.Mode != "reboot" {
		return invalid("mode must be wait_reset or reboot")
	}
	if r.Mode == "reboot" {
		if err := validateTerminalCommand(r.RebootCommand); err != nil {
			return fmt.Errorf("%w: %w", ErrInvalidUBootRequest, err)
		}
	} else if r.RebootCommand != "" {
		return invalid("reboot_command requires reboot mode")
	}
	for _, p := range append([]string{r.CountdownPattern, r.PromptPattern}, r.BootStartedPatterns...) {
		if len(p) == 0 || len(p) > 256 {
			return invalid("patterns must contain 1..256 bytes")
		}
	}
	if strings.ContainsAny(r.PromptPattern, "\r\n") {
		return invalid("prompt must be a single line")
	}
	if len(r.BootStartedPatterns) == 0 || len(r.BootStartedPatterns) > 8 {
		return invalid("provide 1..8 boot_started_patterns")
	}
	if len(r.Interrupt) == 0 || len(r.Interrupt) > 32 {
		return invalid("interrupt must contain 1..32 bytes")
	}
	if r.Timeout == 0 {
		r.Timeout = time.Minute
	}
	if r.Timeout < time.Millisecond || r.Timeout > 3*time.Minute {
		return invalid("timeout must be 1..180000 milliseconds")
	}
	return nil
}

// EnterUBoot arms an independent raw-byte observer before an optional reboot,
// holds and renews the existing terminal lease, and confirms a fresh line-bound
// prompt after the countdown. Cancellation stops writes and releases ownership;
// it never sends recovery keys or closes a healthy shared Session. OPERATION_STATE
// with status ready is observable through the existing Session event stream.
func (a *Application) EnterUBoot(ctx context.Context, identifier string, r UBootRequest) (result UBootResult, err error) {
	if err = validateUBootRequest(&r); err != nil {
		return result, err
	}
	if err = ctx.Err(); err != nil {
		return result, err
	}
	t, err := a.session(identifier)
	if err != nil {
		return result, err
	}
	info := a.diagnosticSessionInfo(t.ID())
	if info.Metadata.Transport != "serial" {
		return result, fmt.Errorf("%w: requires an open serial Session", ErrInvalidUBootRequest)
	}
	if t.State() != session.StateOpen {
		return result, session.ErrNotOpen
	}
	token, err := newTerminalCommandToken()
	if err != nil {
		return result, err
	}
	// The public operation/marker nonce must never reveal the lease capability.
	ownerToken, err := newTerminalCommandToken()
	if err != nil {
		return result, err
	}
	owner := "uboot-" + ownerToken
	if _, err = a.AcquireLease(t.ID(), owner, LeaseTypeTerminal); err != nil {
		return result, err
	}
	started := time.Now()
	result = UBootResult{OperationID: "BOOT-" + token, SessionID: t.ID(), Status: "failed"}
	ctx, stop := context.WithTimeout(ctx, r.Timeout)
	defer stop()
	runCtx, cancel := context.WithCancelCause(ctx)
	done := make(chan struct{})
	go a.renewTerminalCommandLease(runCtx, cancel, t.ID(), owner, done)
	var capture *diagnosticCapture
	if r.Capture {
		capture = &diagnosticCapture{}
	}
	defer func() {
		cancel(nil)
		<-done
		if releaseErr := a.ReleaseLease(t.ID(), owner); releaseErr != nil && !errors.Is(releaseErr, ErrSessionNotFound) {
			err = errors.Join(err, releaseErr)
			if result.Status == "entered" {
				result.Status = "cleanup_failed"
			}
		}
		result.ElapsedMS = time.Since(started).Milliseconds()
		if capture != nil {
			archive, captureErr := a.exportBootDiagnostics(t, info, capture, result)
			if captureErr != nil {
				result.DiagnosticError = "diagnostic archive unavailable"
			} else {
				result.DiagnosticID = archive.ID
			}
		}
		// Publish the archive ID after construction so another observer can
		// retrieve evidence even if a cancelled MCP request has no response.
		metadata := map[string]any{"operation_id": result.OperationID, "operation": "uboot", "status": result.Status, "output_cursor": result.OutputEnd}
		if result.DiagnosticID != "" {
			metadata["diagnostic_id"] = result.DiagnosticID
		}
		if result.DiagnosticError != "" {
			metadata["diagnostic_error"] = result.DiagnosticError
		}
		t.PublishEvent(session.Event{Type: session.EventOperationState, Actor: string(session.ActorSystem), Metadata: metadata})
	}()
	if r.Mode == "reboot" {
		pending, inspectErr := terminalHasPendingInteractiveInput(t)
		if inspectErr != nil {
			return result, inspectErr
		}
		if pending {
			result.Status = "input_pending"
			return result, ErrTerminalCommandInputPending
		}
	}
	tail, err := t.ReadRecent(1)
	if err != nil {
		return result, err
	}
	cursor := tail.Next
	result.OutputStart, result.OutputEnd = uint64(cursor), uint64(cursor)
	t.PublishEvent(session.Event{Type: session.EventOperationState, Actor: string(session.ActorSystem), Metadata: map[string]any{"operation_id": result.OperationID, "operation": "uboot", "status": "ready", "output_cursor": uint64(cursor)}})
	// Unlike terminal_exec, reboot must not wait for an end marker or disable
	// echo. A split nonce prevents echoed bootstrap text from acting as evidence.
	startMarker := []byte("\x1e" + token + "\x1f")
	unsupported := []byte("\x1e" + token + ":unsupported\x1f")
	armed := r.Mode == "wait_reset"
	if !armed {
		bootstrap := "if [ -n \"$BASH_VERSION\" ];then " + bashDeleteCurrentHistoryEntry + ";printf '\\036%s\\037' " + quoteSplitTerminalCommandToken(token) + ";\"$BASH\" --noprofile --norc -c " + quoteTerminalCommand(r.RebootCommand) + ";else printf '\\036%s:unsupported\\037' " + quoteSplitTerminalCommandToken(token) + ";fi\n"
		n, writeErr := a.writeSession(runCtx, t.ID(), owner, session.WriteRequest{Actor: session.ActorSystem, Data: []byte(bootstrap)})
		result.BytesWritten += n
		if writeErr != nil {
			result.Status = "write_failed"
			return result, writeErr
		}
	}
	matcher := bootMatcher{request: r}
	var handshake []byte
	sent := false
	for {
		if cause := context.Cause(runCtx); cause != nil {
			result.Status = bootErrorStatus(cause)
			return result, cause
		}
		chunk, readErr := t.ReadOutput(runCtx, cursor, 32*1024)
		if readErr != nil {
			if cause := context.Cause(runCtx); cause != nil {
				readErr = cause
			}
			result.Status = bootErrorStatus(readErr)
			return result, readErr
		}
		cursor = chunk.Next
		result.OutputEnd = uint64(cursor)
		if capture != nil {
			capture.append(chunk)
		}
		if chunk.Dropped {
			result.Dropped = true
			result.Status = "output_lost"
			return result, ErrUBootFailed
		}
		data := chunk.Data
		if !armed {
			handshake = append(handshake, data...)
			if bytes.Contains(handshake, unsupported) {
				result.Status = "unsupported_shell"
				return result, ErrTerminalCommandRequiresBash
			}
			if i := bytes.Index(handshake, startMarker); i >= 0 {
				data = handshake[i+len(startMarker):]
				armed = true
			} else {
				handshake = append([]byte(nil), handshake[max(0, len(handshake)-len(unsupported)):]...)
				continue
			}
		}
		matcher.feed(data, uint64(cursor)-uint64(len(data)))
		result.CountdownCursor, result.PromptCursor = matcher.countdownCursor, matcher.promptCursor
		result.CountdownMatched, result.PromptMatched = matcher.countdown, matcher.prompt
		// Drain already retained output before acting on an earlier marker. The
		// total deadline also bounds continuous high-rate output.
		latest, latestErr := t.ReadRecent(1)
		if latestErr != nil {
			result.Status = bootErrorStatus(latestErr)
			return result, latestErr
		}
		if latest.Next > cursor {
			continue
		}
		if matcher.booted {
			result.Status = "boot_started"
			return result, ErrUBootFailed
		}
		if matcher.countdown && matcher.prompt {
			result.Status = "entered"
			return result, nil
		}
		if matcher.countdown && !sent {
			n, writeErr := a.writeSession(runCtx, t.ID(), owner, session.WriteRequest{Actor: session.ActorAgent, Data: r.Interrupt})
			result.BytesWritten += n
			if writeErr != nil {
				result.Status = "write_failed"
				return result, writeErr
			}
			sent = true
		}
	}
}

func bootErrorStatus(err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, context.Canceled):
		return "cancelled"
	case errors.Is(err, session.ErrNotOpen), errors.Is(err, io.EOF):
		return "session_closed"
	default:
		return "failed"
	}
}

// bootMatcher retains only bounded pattern overlap and a bounded current line.
// Boot-start evidence is sticky: once observed, later text cannot turn failure
// into success. Prompt recognition requires a line boundary observed this run.
type bootMatcher struct {
	request                       UBootRequest
	tail, line                    []byte
	lineKnown, longLine           bool
	countdown, booted, prompt     bool
	countdownCursor, promptCursor uint64
}

func (m *bootMatcher) feed(data []byte, start uint64) {
	joined := append(m.tail, data...)
	base := start - uint64(len(m.tail))
	if i := bytes.Index(joined, []byte(m.request.CountdownPattern)); i >= 0 && !m.countdown {
		m.countdown, m.countdownCursor = true, base+uint64(i)
	}
	for _, p := range m.request.BootStartedPatterns {
		if bytes.Contains(joined, []byte(p)) {
			m.booted = true
		}
	}
	m.tail = append([]byte(nil), joined[max(0, len(joined)-255):]...)
	for i, b := range data {
		if b == '\r' || b == '\n' {
			m.line = m.line[:0]
			m.lineKnown = true
			m.longLine = false
			m.prompt = false
			continue
		}
		if len(m.line) >= 256 {
			m.longLine = true
		} else {
			m.line = append(m.line, b)
		}
		m.prompt = m.countdown && m.lineKnown && !m.longLine && bytes.Equal(m.line, []byte(m.request.PromptPattern))
		if m.prompt {
			m.promptCursor = start + uint64(i+1) - uint64(len(m.line))
		}
	}
}

func (a *Application) exportBootDiagnostics(t session.Session, info session.SessionInfo, c *diagnosticCapture, result UBootResult) (DiagnosticArchive, error) {
	a.diagnostics.mu.Lock()
	defer a.diagnostics.mu.Unlock()
	var failures []map[string]any
	if result.Status != "entered" {
		failures = []map[string]any{{"operation_id": result.OperationID, "tool": "terminal_enter_uboot", "code": result.Status, "output_cursor": result.OutputEnd}}
	}
	return a.buildDiagnosticLocked(context.Background(), t, info, c, true, "operation", &result, failures)
}
