package app

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/akira-init1/ChannelTerm/internal/core/session"
)

const (
	// MaxTerminalCommandBytes bounds one terminal_exec shell command before any
	// remote wrapper is allocated or written.
	MaxTerminalCommandBytes = 64 * 1024

	terminalCommandReadSize        = 32 * 1024
	terminalCommandRenewInterval   = 10 * time.Second
	terminalCommandRecoveryTimeout = 5 * time.Second
)

var (
	// ErrTerminalCommandRequired is returned when terminal_exec has no command.
	ErrTerminalCommandRequired = errors.New("terminal command is required")
	// ErrTerminalCommandTooLarge is returned before acquiring a lease when one
	// command exceeds MaxTerminalCommandBytes.
	ErrTerminalCommandTooLarge = errors.New("terminal command exceeds maximum size")
	// ErrTerminalCommandControlCharacter prevents multiline input and terminal
	// control bytes from escaping the structured Agent presentation boundary.
	ErrTerminalCommandControlCharacter = errors.New("terminal command must be one printable line")
	// ErrTerminalCommandInputPending prevents an isolated command bootstrap from
	// being appended to bytes already waiting in the interactive line editor.
	ErrTerminalCommandInputPending = errors.New("terminal has unsubmitted interactive input")
	// ErrTerminalCommandRequiresBash reports that zero-history execution cannot
	// be guaranteed for the active interactive shell.
	ErrTerminalCommandRequiresBash = errors.New("zero-history terminal commands require an interactive Bash shell")
	// ErrTerminalCommandProtocol identifies a missing or malformed target-side
	// command marker.
	ErrTerminalCommandProtocol = errors.New("invalid terminal command protocol response")
)

// TerminalCommandResult describes one completed isolated Agent command.
// OutputStart and OutputEnd delimit only command output in the raw Session
// stream; each observer retains its own independent output cursor.
type TerminalCommandResult struct {
	SessionID   string
	CommandID   string
	ExitCode    int
	OutputStart session.OutputCursor
	OutputEnd   session.OutputCursor
}

// ExecuteTerminalCommand runs command in a non-interactive child shell of an
// idle interactive Bash prompt. The parent Bash removes the bootstrap from its
// current in-memory history before disabling terminal echo. A terminal lease
// excludes concurrent ChannelTerm writers for the operation, and Application
// renews that lease until completion or bounded cancellation recovery.
//
// ctx cancellation sends Ctrl+C through the owned lease and waits briefly for
// the wrapper to restore terminal echo. If recovery cannot be confirmed, the
// Session is closed rather than releasing a possibly corrupted terminal.
func (a *Application) ExecuteTerminalCommand(ctx context.Context, identifier, command string) (result TerminalCommandResult, err error) {
	if err := ctx.Err(); err != nil {
		return TerminalCommandResult{}, err
	}
	if err := validateTerminalCommand(command); err != nil {
		return TerminalCommandResult{}, err
	}
	terminal, err := a.session(identifier)
	if err != nil {
		return TerminalCommandResult{}, err
	}
	token, err := newTerminalCommandToken()
	if err != nil {
		return TerminalCommandResult{}, err
	}
	ownerToken, err := newTerminalCommandToken()
	if err != nil {
		return TerminalCommandResult{}, err
	}
	owner := "terminal-command-" + ownerToken
	_, err = a.AcquireLease(terminal.ID(), owner, LeaseTypeTerminal)
	if err != nil {
		return TerminalCommandResult{}, err
	}
	commandID := "CMD-" + token
	result = TerminalCommandResult{SessionID: terminal.ID(), CommandID: commandID}
	released := false
	defer func() {
		if released {
			return
		}
		if releaseErr := a.ReleaseLease(terminal.ID(), owner); releaseErr != nil && !errors.Is(releaseErr, ErrSessionNotFound) {
			err = errors.Join(err, fmt.Errorf("release terminal command lease: %w", releaseErr))
		}
	}()

	if pending, pendingErr := terminalHasPendingInteractiveInput(terminal); pendingErr != nil {
		return result, pendingErr
	} else if pending {
		a.publishTerminalCommandFailed(terminal, commandID, ErrTerminalCommandInputPending)
		return result, ErrTerminalCommandInputPending
	}

	startCursor := sessionOutputCursor(terminal)
	terminal.PublishEvent(session.Event{
		Type:  session.EventTerminalCommandStarted,
		Actor: string(session.ActorAgent),
		Metadata: map[string]any{
			"command_id":    commandID,
			"command":       command,
			"output_cursor": uint64(startCursor),
		},
	})

	executionCtx, cancelExecution := context.WithCancelCause(ctx)
	renewDone := make(chan struct{})
	go a.renewTerminalCommandLease(executionCtx, cancelExecution, terminal.ID(), owner, renewDone)
	defer func() {
		cancelExecution(nil)
		<-renewDone
	}()

	protocol := &terminalCommandProtocol{terminal: terminal, cursor: startCursor, token: token}
	bootstrap := terminalCommandBootstrap(command, token)
	if _, writeErr := a.writeSession(executionCtx, terminal.ID(), owner, session.WriteRequest{Actor: session.ActorSystem, Data: []byte(bootstrap + "\n")}); writeErr != nil {
		a.publishTerminalCommandFailed(terminal, commandID, writeErr)
		cancelExecution(writeErr)
		return result, fmt.Errorf("start terminal command: %w", writeErr)
	}

	outputStart, startErr := protocol.waitStart(executionCtx)
	if startErr != nil {
		startErr = terminalCommandExecutionError(executionCtx, startErr)
		if errors.Is(startErr, ErrTerminalCommandRequiresBash) {
			a.publishTerminalCommandFailed(terminal, commandID, startErr)
			cancelExecution(startErr)
			return result, startErr
		}
		return a.recoverTerminalCommand(terminal, owner, commandID, protocol, result, startErr, cancelExecution)
	}
	result.OutputStart = outputStart
	terminal.PublishEvent(session.Event{
		Type:  session.EventTerminalCommandOutputStarted,
		Actor: string(session.ActorSystem),
		Metadata: map[string]any{
			"command_id":    commandID,
			"output_cursor": uint64(outputStart),
		},
	})

	exitCode, outputEnd, markerEnd, waitErr := protocol.waitEnd(executionCtx)
	if waitErr != nil {
		waitErr = terminalCommandExecutionError(executionCtx, waitErr)
		return a.recoverTerminalCommand(terminal, owner, commandID, protocol, result, waitErr, cancelExecution)
	}
	result.ExitCode = exitCode
	result.OutputEnd = outputEnd
	terminal.PublishEvent(session.Event{
		Type:  session.EventTerminalCommandCompleted,
		Actor: string(session.ActorAgent),
		Metadata: map[string]any{
			"command_id":    commandID,
			"exit_code":     exitCode,
			"output_cursor": uint64(outputEnd),
			"hidden_start":  uint64(outputEnd),
			"hidden_end":    uint64(markerEnd),
		},
	})
	cancelExecution(nil)
	if releaseErr := a.ReleaseLease(terminal.ID(), owner); releaseErr != nil {
		return result, fmt.Errorf("release terminal command lease: %w", releaseErr)
	}
	released = true
	return result, nil
}

func terminalCommandExecutionError(ctx context.Context, fallback error) error {
	if cause := context.Cause(ctx); cause != nil {
		return cause
	}
	return fallback
}

func validateTerminalCommand(command string) error {
	if strings.TrimSpace(command) == "" {
		return ErrTerminalCommandRequired
	}
	if len(command) > MaxTerminalCommandBytes {
		return fmt.Errorf("%w: got %d bytes, maximum %d", ErrTerminalCommandTooLarge, len(command), MaxTerminalCommandBytes)
	}
	if !utf8.ValidString(command) {
		return ErrTerminalCommandControlCharacter
	}
	for _, value := range command {
		if unicode.IsControl(value) {
			return ErrTerminalCommandControlCharacter
		}
	}
	return nil
}

func terminalHasPendingInteractiveInput(terminal session.Session) (bool, error) {
	activity, err := terminal.ReadRecentActivity(session.DefaultActivityBufferCapacity)
	if err != nil {
		return false, fmt.Errorf("inspect terminal input activity: %w", err)
	}
	pending := false
	for _, event := range activity.Events {
		if event.Operation != session.OperationWrite || event.Actor == session.ActorSystem {
			continue
		}
		for _, value := range event.Data {
			switch value {
			case '\r', '\n', 0x03, 0x15:
				pending = false
			case '\b', 0x7f:
				// Without a terminal emulator ChannelTerm cannot know whether one
				// backspace emptied the line, so remain conservatively pending.
			default:
				pending = true
			}
		}
	}
	return pending, nil
}

func newTerminalCommandToken() (string, error) {
	var tokenBytes [12]byte
	if _, err := rand.Read(tokenBytes[:]); err != nil {
		return "", fmt.Errorf("generate terminal command token: %w", err)
	}
	return hex.EncodeToString(tokenBytes[:]), nil
}

func terminalCommandBootstrap(command, token string) string {
	quotedCommand := quoteTerminalCommand(command)
	quotedToken := quoteSplitTerminalCommandToken(token)
	return "if [ -n \"$BASH_VERSION\" ];then " + bashDeleteCurrentHistoryEntry + ";(ct_done=0;ct_finish(){ [ \"$ct_done\" = 1 ]&&return;ct_done=1;stty echo;printf '\\035\\034%s:%s\\034\\035' " + quotedToken + " \"$1\";};trap 'ct_finish 130' 1 2 15;trap 'ct_finish $?' 0;stty -echo;printf '\\036\\037%s\\037\\036' " + quotedToken + ";\"$BASH\" --noprofile --norc -c " + quotedCommand + ";ct_rc=$?;ct_finish \"$ct_rc\";trap - 0 1 2 15);else printf '\\035\\034%s:UNSUPPORTED\\034\\035' " + quotedToken + ";fi"
}

func quoteTerminalCommand(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

// quoteSplitTerminalCommandToken prevents the complete marker token from
// appearing in the echoed bootstrap while producing one shell word at runtime.
func quoteSplitTerminalCommandToken(token string) string {
	middle := len(token) / 2
	return "'" + token[:middle] + "''" + token[middle:] + "'"
}

func (a *Application) renewTerminalCommandLease(ctx context.Context, cancel context.CancelCauseFunc, sessionID, owner string, done chan<- struct{}) {
	defer close(done)
	ticker := time.NewTicker(terminalCommandRenewInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := a.RenewLease(sessionID, owner); err != nil {
				cancel(fmt.Errorf("renew terminal command lease: %w", err))
				return
			}
		}
	}
}

func (a *Application) recoverTerminalCommand(terminal session.Session, owner, commandID string, protocol *terminalCommandProtocol, result TerminalCommandResult, cause error, cancel context.CancelCauseFunc) (TerminalCommandResult, error) {
	cancel(cause)
	recoveryCtx, stopRecovery := context.WithTimeout(context.Background(), terminalCommandRecoveryTimeout)
	defer stopRecovery()
	_, interruptErr := a.writeSession(recoveryCtx, terminal.ID(), owner, session.WriteRequest{Actor: session.ActorSystem, Data: []byte{0x03}})
	if interruptErr == nil {
		exitCode, outputEnd, markerEnd, waitErr := protocol.waitEnd(recoveryCtx)
		if waitErr == nil {
			result.ExitCode = exitCode
			result.OutputEnd = outputEnd
			a.publishTerminalCommandFailed(terminal, commandID, cause, outputEnd, markerEnd)
			return result, cause
		}
		interruptErr = waitErr
	}
	a.publishTerminalCommandFailed(terminal, commandID, errors.Join(cause, interruptErr), 0, 0)
	_, closeErr := a.serial.CloseSession(terminal.ID())
	a.leases.remove(terminal.ID())
	return result, errors.Join(cause, fmt.Errorf("terminal command recovery failed; Session closed: %w", interruptErr), closeErr)
}

func (a *Application) publishTerminalCommandFailed(terminal session.Session, commandID string, commandErr error, hiddenRange ...session.OutputCursor) {
	metadata := map[string]any{"command_id": commandID}
	if commandErr != nil {
		metadata["error"] = commandErr.Error()
	}
	if len(hiddenRange) == 2 && hiddenRange[1] > hiddenRange[0] {
		metadata["output_cursor"] = uint64(hiddenRange[0])
		metadata["hidden_start"] = uint64(hiddenRange[0])
		metadata["hidden_end"] = uint64(hiddenRange[1])
	}
	terminal.PublishEvent(session.Event{Type: session.EventTerminalCommandFailed, Actor: string(session.ActorSystem), Metadata: metadata})
}

type terminalCommandProtocol struct {
	terminal session.Session
	cursor   session.OutputCursor
	token    string
}

func (p *terminalCommandProtocol) waitStart(ctx context.Context) (session.OutputCursor, error) {
	startMarker := []byte("\x1e\x1f" + p.token + "\x1f\x1e")
	endPrefix := []byte("\x1d\x1c" + p.token + ":")
	pending := make([]byte, 0, len(startMarker)*2)
	pendingStart := p.cursor
	for {
		chunk, err := p.terminal.ReadOutput(ctx, p.cursor, terminalCommandReadSize)
		if err != nil {
			return 0, err
		}
		if chunk.Dropped {
			return 0, fmt.Errorf("%w: Session output was overwritten before command start", ErrTerminalCommandProtocol)
		}
		p.cursor = chunk.Next
		pending = append(pending, chunk.Data...)
		if index := bytes.Index(pending, startMarker); index >= 0 {
			markerEnd := pendingStart + session.OutputCursor(index+len(startMarker))
			// The chunk may already contain command output or even completion.
			// Re-read from the marker boundary so the next phase cannot miss it.
			p.cursor = markerEnd
			return markerEnd, nil
		}
		if index := bytes.Index(pending, endPrefix); index >= 0 {
			return 0, ErrTerminalCommandRequiresBash
		}
		keep := max(len(startMarker), len(endPrefix)) - 1
		if len(pending) > keep {
			dropped := len(pending) - keep
			pending = append(pending[:0], pending[dropped:]...)
			pendingStart += session.OutputCursor(dropped)
		}
	}
}

func (p *terminalCommandProtocol) waitEnd(ctx context.Context) (int, session.OutputCursor, session.OutputCursor, error) {
	prefix := []byte("\x1d\x1c" + p.token + ":")
	suffix := []byte("\x1c\x1d")
	pending := make([]byte, 0, len(prefix)+16)
	pendingStart := p.cursor
	for {
		chunk, err := p.terminal.ReadOutput(ctx, p.cursor, terminalCommandReadSize)
		if err != nil {
			return 0, 0, 0, err
		}
		if chunk.Dropped {
			return 0, 0, 0, fmt.Errorf("%w: Session output was overwritten before command completion", ErrTerminalCommandProtocol)
		}
		p.cursor = chunk.Next
		pending = append(pending, chunk.Data...)
		prefixIndex := bytes.Index(pending, prefix)
		if prefixIndex >= 0 {
			statusStart := prefixIndex + len(prefix)
			if suffixIndex := bytes.Index(pending[statusStart:], suffix); suffixIndex >= 0 {
				status := string(pending[statusStart : statusStart+suffixIndex])
				exitCode, parseErr := strconv.Atoi(status)
				if parseErr != nil || exitCode < 0 || exitCode > 255 {
					return 0, 0, 0, fmt.Errorf("%w: invalid exit status %q", ErrTerminalCommandProtocol, status)
				}
				markerStart := pendingStart + session.OutputCursor(prefixIndex)
				markerEnd := pendingStart + session.OutputCursor(statusStart+suffixIndex+len(suffix))
				return exitCode, markerStart, markerEnd, nil
			}
		}
		keep := len(prefix) + 8
		if len(pending) > keep {
			dropped := len(pending) - keep
			pending = append(pending[:0], pending[dropped:]...)
			pendingStart += session.OutputCursor(dropped)
		}
	}
}
