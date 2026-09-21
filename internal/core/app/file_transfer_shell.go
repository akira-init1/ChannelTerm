package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/core/session"
)

const (
	fileTransferShellBootstrapCommand = "if [ -n \"$BASH_VERSION\" ];then " + bashDeleteCurrentHistoryEntry + ";fi;stty -echo;printf '\\036\\037';CTERM_FT=1 sh -c 'while read -r x;do eval \"$x\";done';stty echo;printf '\\035\\034'"
	fileTransferShellEchoHiddenMarker = "\x1e\x1f"
	fileTransferShellExitedMarker     = "\x1d\x1c"
	fileTransferShellIdleTimeout      = 30 * time.Second
	fileTransferShellHeartbeat        = 5 * time.Second
	fileTransferShellWriteTimeout     = 5 * time.Second
)

// WithFileTransferShell runs operation inside one non-interactive target-side
// shell. An interactive Bash parent deletes the bootstrap from its in-memory
// history while other shells still receive one recognizable entry instead of
// one command per bounded payload chunk. Nested calls reuse the active shell.
func WithFileTransferShell(ctx context.Context, terminal FileTransferSession, operation func(FileTransferSession) error) (err error) {
	if terminal == nil {
		return errors.New("file transfer session must not be nil")
	}
	if operation == nil {
		return errors.New("file transfer shell operation must not be nil")
	}
	if _, ok := terminal.(*shellFileTransferSession); ok {
		return operation(terminal)
	}

	shell, err := newShellFileTransferSession(ctx, terminal)
	if err != nil {
		return err
	}
	defer func() {
		closeCtx, cancel := fileTransferRecoveryContext()
		defer cancel()
		if closeErr := shell.close(closeCtx); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	return operation(shell)
}

// shellFileTransferSession forwards raw Session traffic while serializing it
// with idle heartbeats. Protocol commands disarm the remote watchdog until
// their terminal marker arrives; raw payload writes therefore cannot race a
// keepalive line.
type shellFileTransferSession struct {
	terminal FileTransferSession
	token    string

	// writeMu keeps an idle heartbeat from landing between a READY/DATA marker
	// and its raw payload. idle becomes true only after the command's terminal
	// marker, when the child shell is safely reading its next script line.
	writeMu      sync.Mutex
	idle         bool
	started      bool
	closed       bool
	heartbeatErr error
	heartbeatOn  bool
	stopOnce     sync.Once
	stop         chan struct{}
	done         chan struct{}
}

func newShellFileTransferSession(ctx context.Context, terminal FileTransferSession) (*shellFileTransferSession, error) {
	token, err := newFileTransferToken()
	if err != nil {
		return nil, err
	}
	shell := &shellFileTransferSession{
		terminal: terminal,
		token:    token,
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
	}
	if err := shell.start(ctx); err != nil {
		closeCtx, cancel := fileTransferRecoveryContext()
		defer cancel()
		return nil, errors.Join(err, shell.close(closeCtx))
	}
	shell.heartbeatOn = true
	go shell.heartbeatLoop()
	return shell, nil
}

func (s *shellFileTransferSession) start(ctx context.Context) error {
	recent, err := s.terminal.ReadRecent(ctx, 1)
	if err != nil {
		return fmt.Errorf("initialize file-transfer shell cursor: %w", err)
	}
	if _, err := writeFilePayload(ctx, s.terminal, []byte(fileTransferShellBootstrapCommand+"\n")); err != nil {
		return fmt.Errorf("start remote file-transfer shell: %w", err)
	}
	s.started = true
	if err := waitForFileTransferShellMarker(ctx, s.terminal, recent.Next, fileTransferShellEchoHiddenMarker); err != nil {
		return fmt.Errorf("wait for remote file-transfer shell to disable echo: %w", err)
	}
	if _, err := writeFilePayload(ctx, s.terminal, []byte(fileTransferShellInitCommand(s.token)+"\n")); err != nil {
		return fmt.Errorf("initialize remote file-transfer shell: %w", err)
	}
	waiter := &fileProtocol{terminal: s.terminal, token: s.token, cursor: recent.Next}
	event, err := waiter.expectPhase(ctx, "SHELL")
	if err != nil {
		return fmt.Errorf("wait for remote file-transfer shell: %w", err)
	}
	if len(event) == 2 && event[0] == "ERROR" && event[1] == "sleep" {
		if exitErr := waitForFileTransferShellMarker(ctx, s.terminal, recent.Next, fileTransferShellExitedMarker); exitErr != nil {
			return errors.Join(errors.New("remote file transfer failed: sleep command is unavailable"), exitErr)
		}
		s.started = false
		return errors.New("remote file transfer failed: sleep command is unavailable")
	}
	if len(event) == 1 && event[0] == "EXITED" {
		s.started = false
		return errors.New("remote file-transfer shell exited before initialization")
	}
	if len(event) != 1 || event[0] != "READY" {
		return fmt.Errorf("%w: SHELL response is %q, want READY", ErrFileTransferProtocol, event)
	}
	s.writeMu.Lock()
	s.idle = true
	s.writeMu.Unlock()
	return nil
}

// waitForFileTransferShellMarker waits for control bytes that are absent from
// the echoed textual shell commands. The startup marker prevents command input
// from racing stty -echo. The exit marker is emitted by the interactive parent
// only after the child has exited, so the lease's presentation cursor includes
// every earlier bootstrap echo before attach resumes ordinary rendering.
func waitForFileTransferShellMarker(ctx context.Context, terminal FileTransferSession, cursor session.OutputCursor, markerText string) error {
	marker := []byte(markerText)
	pending := make([]byte, 0, len(marker)*2)
	for {
		chunk, err := terminal.ReadOutput(ctx, cursor, fileProtocolReadSize)
		if err != nil {
			return err
		}
		if chunk.Dropped {
			return fmt.Errorf("%w: Session output was overwritten while disabling terminal echo", ErrFileTransferProtocol)
		}
		cursor = chunk.Next
		pending = append(pending, chunk.Data...)
		if bytes.Contains(pending, marker) {
			return nil
		}
		if len(pending) >= len(marker) {
			pending = append(pending[:0], pending[len(pending)-len(marker)+1:]...)
		}
	}
}

func (s *shellFileTransferSession) close(ctx context.Context) error {
	if !s.started {
		return nil
	}
	s.stopOnce.Do(func() { close(s.stop) })
	if s.heartbeatOn {
		select {
		case <-s.done:
		case <-ctx.Done():
			return s.recoveryError(ctx.Err())
		}
	}

	s.writeMu.Lock()
	if s.closed {
		s.writeMu.Unlock()
		return nil
	}
	s.closed = true
	s.idle = false
	s.writeMu.Unlock()

	recent, err := s.terminal.ReadRecent(ctx, 1)
	if err != nil {
		return s.recoveryError(fmt.Errorf("initialize file-transfer shell exit cursor: %w", err))
	}
	if _, err := writeFilePayload(ctx, s.terminal, []byte(fileTransferShellCloseCommand()+"\n")); err != nil {
		return s.recoveryError(fmt.Errorf("close remote file-transfer shell: %w", err))
	}
	if err := waitForFileTransferShellMarker(ctx, s.terminal, recent.Next, fileTransferShellExitedMarker); err != nil {
		return s.recoveryError(fmt.Errorf("confirm remote file-transfer shell exit: %w", err))
	}
	return nil
}

func (s *shellFileTransferSession) heartbeatLoop() {
	defer close(s.done)
	ticker := time.NewTicker(fileTransferShellHeartbeat)
	defer ticker.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-ticker.C:
			s.heartbeat()
		}
	}
}

func (s *shellFileTransferSession) heartbeat() {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if !s.idle || s.closed || s.heartbeatErr != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), fileTransferShellWriteTimeout)
	defer cancel()
	if _, err := writeFilePayload(ctx, s.terminal, []byte("if [ -n \"$CTERM_FT\" ]; then ct_active; ct_idle; fi\n")); err != nil {
		s.heartbeatErr = fmt.Errorf("keep remote file-transfer shell alive: %w", err)
		s.idle = false
	}
}

func (s *shellFileTransferSession) writeCommand(ctx context.Context, command string) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if s.closed {
		return errors.New("remote file-transfer shell is closed")
	}
	if s.heartbeatErr != nil {
		return s.heartbeatErr
	}
	s.idle = false
	wrapped := "if [ -n \"$CTERM_FT\" ]; then ct_active; " + command + "; ct_idle; fi"
	if _, err := writeFilePayload(ctx, s.terminal, []byte(wrapped+"\n")); err != nil {
		return fmt.Errorf("write remote file-transfer command: %w", err)
	}
	return nil
}

func (s *shellFileTransferSession) commandCompleted() {
	s.writeMu.Lock()
	if !s.closed && s.heartbeatErr == nil {
		s.idle = true
	}
	s.writeMu.Unlock()
}

func (s *shellFileTransferSession) ReadRecent(ctx context.Context, limit int) (session.OutputChunk, error) {
	return s.terminal.ReadRecent(ctx, limit)
}

func (s *shellFileTransferSession) ReadOutput(ctx context.Context, cursor session.OutputCursor, limit int) (session.OutputChunk, error) {
	return s.terminal.ReadOutput(ctx, cursor, limit)
}

func (s *shellFileTransferSession) Write(request session.WriteRequest) (int, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if s.closed {
		return 0, errors.New("remote file-transfer shell is closed")
	}
	if s.heartbeatErr != nil {
		return 0, s.heartbeatErr
	}
	return s.terminal.Write(request)
}

func (s *shellFileTransferSession) WriteContext(ctx context.Context, request session.WriteRequest) (int, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if s.closed {
		return 0, errors.New("remote file-transfer shell is closed")
	}
	if s.heartbeatErr != nil {
		return 0, s.heartbeatErr
	}
	if contextual, ok := s.terminal.(interface {
		WriteContext(context.Context, session.WriteRequest) (int, error)
	}); ok {
		return contextual.WriteContext(ctx, request)
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return s.terminal.Write(request)
}

func (s *shellFileTransferSession) FileTransferCancelRequested() bool {
	requester, ok := s.terminal.(fileTransferCancelRequester)
	return ok && requester.FileTransferCancelRequested()
}

func (s *shellFileTransferSession) FileTransferCancellationCheckpoint(ctx context.Context) error {
	checkpoint, ok := s.terminal.(fileTransferCancellationCheckpointer)
	if ok {
		return checkpoint.FileTransferCancellationCheckpoint(ctx)
	}
	requester, ok := s.terminal.(fileTransferCancelRequester)
	if ok && requester.FileTransferCancelRequested() {
		return context.Canceled
	}
	return nil
}

func (s *shellFileTransferSession) recoveryError(err error) error {
	if err == nil || !errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return (&fileProtocol{terminal: s.terminal}).recoveryError(err)
}

func fileTransferShellInitCommand(token string) string {
	seconds := int(fileTransferShellIdleTimeout / time.Second)
	// The watchdog owns its sleep child and forwards termination to it. Without
	// that forwarding, refreshing the watchdog could orphan one sleep process
	// per heartbeat on shells that do not exec the background subshell's sleep.
	return fmt.Sprintf("printf '\\033[1A\\r\\033[2K'; t='%s'; if ! command -v sleep >/dev/null 2>&1; then stty echo; printf '\\n@CTERM:%%s:SHELL:ERROR:sleep\\n' \"$t\"; exit 127; fi; ct_pid=$$; ct_watch=; ct_watchdog(){ trap 'kill \"$ct_sleep\" 2>/dev/null; wait \"$ct_sleep\" 2>/dev/null; exit 0' 1 15; sleep %d & ct_sleep=$!; wait \"$ct_sleep\"; kill -TERM \"$ct_pid\" 2>/dev/null; }; ct_active(){ if [ -n \"$ct_watch\" ]; then kill \"$ct_watch\" 2>/dev/null; wait \"$ct_watch\" 2>/dev/null; ct_watch=; fi; }; ct_idle(){ ct_watchdog & ct_watch=$!; }; ct_timeout(){ stty echo; printf '\\n@CTERM:%%s:SHELL:TIMEOUT\\n' \"$t\"; exit 124; }; trap 'ct_timeout' 1 15; trap 'ct_active' 0; printf '\\n@CTERM:%%s:SHELL:READY\\n' \"$t\"; ct_idle", token, seconds)
}

func fileTransferShellCloseCommand() string {
	return "if [ -n \"$CTERM_FT\" ]; then ct_active; trap - 0 1 15; exit; fi"
}

// isFileTransferCommandComplete keeps the shell busy across each raw block:
// READY and DATA announce that dd still owns the terminal, while every other
// protocol phase is emitted at a safe command boundary.
func isFileTransferCommandComplete(phase string) bool {
	return !strings.EqualFold(phase, "READY") && !strings.EqualFold(phase, "DATA")
}
