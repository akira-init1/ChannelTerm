package command

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/cli/interactive"
	"github.com/akira-init1/ChannelTerm/internal/core/session"
)

// attachInputMode defines the one local interpretation of console input while
// an attachment is active. Only attachInputDispatcher changes modes, so a byte
// cannot be forwarded to the board and used as a local transfer command.
type attachInputMode uint8

const (
	attachInputModeAttach attachInputMode = iota
	attachInputModeFileMenu
	attachInputModeSendLocalPath
	attachInputModeSendRemotePath
	attachInputModeReceiveRemotePath
	attachInputModeReceiveLocalPath
	attachInputModeFileTransfer
	attachInputModeFileTransferCancelConfirm
)

const controlCRecoveryWindow = 500 * time.Millisecond

// attachTransferRunner performs one attach shortcut transfer after the input
// dispatcher has entered file-transfer mode.
type attachTransferRunner func(context.Context, attachSession, io.Writer, func() bool, string, string, string) error

type fileTransferCancelController interface {
	BeginFileTransferCancel(context.Context) (requestID string, active bool, err error)
	ResolveFileTransferCancel(context.Context, string, bool) (state string, err error)
}

type fileTransferCancelPromptPresenter interface {
	fileTransferCancelPromptStarted()
	fileTransferCancelPromptFinished()
}

// attachInputDispatcher owns all local interpretation of one attach console.
// attachInputPump is its sole stdin/Console reader; menu, path, and transfer
// modes consume only the pump's ordered results and never read stdin directly.
type attachInputDispatcher struct {
	ctx                   context.Context
	pump                  *attachInputPump
	interrupts            <-chan os.Signal
	terminal              attachSession
	writeLocal            func([]byte) error
	writeStatus           func([]byte) error
	togglePromptTimestamp func() error
	stopAttach            context.CancelFunc
	controller            *interactive.Controller
	mode                  attachInputMode
	line                  []byte
	skipLineFeed          bool
	localPath             string
	remotePath            string
	transferDone          <-chan error
	transferCancellation  *fileTransferCancellation
	externalCancelRequest string
	inputIgnoredAnnounced bool
	// ignoreControlCUntil prevents keyboard auto-repeat and the duplicate
	// Windows CTRL_C_EVENT from escaping transfer mode after a cancellation.
	// Those events can otherwise reach the board after the worker has restored
	// its TTY, or write through a lease whose MCP client is closing.
	ignoreControlCUntil time.Time
	transferRunner      attachTransferRunner
}

func newAttachInputDispatcherWithPump(ctx context.Context, pump *attachInputPump, terminal attachSession, writeLocal func([]byte) error, togglePromptTimestamp func() error, cancel context.CancelFunc, runner attachTransferRunner) *attachInputDispatcher {
	return newAttachInputDispatcherWithPumpAndInterrupts(ctx, pump, terminal, writeLocal, togglePromptTimestamp, cancel, runner, nil)
}

func newAttachInputDispatcherWithPumpAndInterrupts(ctx context.Context, pump *attachInputPump, terminal attachSession, writeLocal func([]byte) error, togglePromptTimestamp func() error, cancel context.CancelFunc, runner attachTransferRunner, interrupts <-chan os.Signal) *attachInputDispatcher {
	return &attachInputDispatcher{
		ctx:                   ctx,
		pump:                  pump,
		interrupts:            interrupts,
		terminal:              terminal,
		writeLocal:            writeLocal,
		writeStatus:           writeLocal,
		togglePromptTimestamp: togglePromptTimestamp,
		stopAttach:            cancel,
		controller:            interactive.NewController(interactive.DefaultEscapeByte),
		mode:                  attachInputModeAttach,
		transferRunner:        runner,
	}
}

// run keeps dispatching input while a transfer worker runs independently. It
// is the only consumer of attachInputPump results for the production attach
// path, and therefore the only place that routes Ctrl+C.
func (d *attachInputDispatcher) run() {
	defer d.abandonExternalTransferCancellation()
	for {
		if d.transferDone != nil {
			select {
			case err := <-d.transferDone:
				d.finishTransfer(err)
				continue
			case result, ok := <-d.pump.results:
				if !ok || result.err != nil {
					d.requestTransferCancellation()
					<-d.transferDone
					return
				}
				if !d.dispatch(result.data) {
					return
				}
			case interrupt := <-d.interrupts:
				if !d.dispatchInterrupt(interrupt) {
					return
				}
			case <-d.ctx.Done():
				// A process-level interruption must not abandon an active raw
				// chunk. Request the same safe cancellation used for byte 0x03.
				d.requestTransferCancellation()
				<-d.transferDone
				return
			}
			continue
		}

		if result, ok := d.pump.takePending(); ok {
			if result.err != nil || !d.dispatch(result.data) {
				return
			}
			continue
		}
		select {
		case interrupt := <-d.interrupts:
			if !d.dispatchInterrupt(interrupt) {
				return
			}
		case <-d.ctx.Done():
			return
		case result, ok := <-d.pump.results:
			if !ok || result.err != nil || !d.dispatch(result.data) {
				return
			}
		}
	}
}

// dispatchInterrupt routes a process-level Console interruption through the
// same mode decision used for byte 0x03. This covers Windows hosts that emit
// CTRL_C_EVENT instead of (or in addition to) a raw console key event.
func (d *attachInputDispatcher) dispatchInterrupt(interrupt os.Signal) bool {
	if interrupt == nil {
		return true
	}
	if interrupt != os.Interrupt {
		d.cancelAttach()
		return false
	}
	return d.handleControlC()
}

// handleControlC is the single mode-aware Ctrl+C decision for both a raw byte
// and a process-level Console interruption.
func (d *attachInputDispatcher) handleControlC() bool {
	if d.mode != attachInputModeFileTransfer && time.Now().Before(d.ignoreControlCUntil) {
		return true
	}
	switch d.mode {
	case attachInputModeAttach:
		if d.beginExternalTransferCancellationConfirmation() {
			return true
		}
		return d.dispatchAttach([]byte{0x03})
	case attachInputModeFileTransfer:
		d.beginTransferCancellationConfirmation()
	case attachInputModeFileTransferCancelConfirm:
		// A duplicate raw ETX or Windows CTRL_C_EVENT belongs to the gesture
		// that opened this prompt. Only an explicit y/Y confirms cancellation.
		return true
	}
	return true
}

func (d *attachInputDispatcher) dispatch(data []byte) bool {
	// A Windows Ctrl+C can leave both an ETX and a plain C key record queued
	// behind the CTRL_C_EVENT. While confirmation or cancellation is active,
	// neither representation is user input for the remote shell.
	// Discard the whole queued result during the short recovery window so a
	// delayed C cannot be forwarded through a lease that is being released.
	if d.mode == attachInputModeAttach && time.Now().Before(d.ignoreControlCUntil) {
		return true
	}
	if d.mode == attachInputModeFileTransferCancelConfirm && time.Now().Before(d.ignoreControlCUntil) && len(data) > 0 && (data[0] == 'c' || data[0] == 'C') {
		return true
	}
	for len(data) > 0 {
		if data[0] == 0x03 {
			if !d.handleControlC() {
				return false
			}
			data = data[1:]
			continue
		}
		switch d.mode {
		case attachInputModeAttach:
			return d.dispatchAttach(data)
		case attachInputModeFileMenu:
			d.dispatchFileMenuByte(data[0])
		case attachInputModeSendLocalPath, attachInputModeSendRemotePath, attachInputModeReceiveRemotePath, attachInputModeReceiveLocalPath:
			if !d.dispatchPathByte(data[0]) {
				return false
			}
		case attachInputModeFileTransfer:
			if !d.ignoreTransferInput() {
				return false
			}
		case attachInputModeFileTransferCancelConfirm:
			d.resolveTransferCancellationConfirmation(data[0] == 'y' || data[0] == 'Y')
			// One key answers the prompt. Discard any CR/LF or pasted tail in
			// this input batch rather than treating it as transfer input.
			return true
		}
		data = data[1:]
	}
	return true
}

func (d *attachInputDispatcher) dispatchAttach(data []byte) bool {
	remote := make([]byte, 0, len(data))
	flushRemote := func() bool {
		if len(remote) == 0 {
			return true
		}
		if err := writeSession(d.terminal, session.ActorUser, remote); err != nil {
			return d.writeLocal != nil && d.writeLocal(writeFailureText(err)) == nil
		}
		remote = remote[:0]
		return true
	}
	for index, value := range data {
		for _, action := range d.controller.Process([]byte{value}) {
			switch action.Kind {
			case interactive.ActionRemote:
				remote = append(remote, action.Data...)
			case interactive.ActionEscapePending:
				if !flushRemote() || d.writeLocal == nil || d.writeLocal(escapePendingText) != nil {
					d.cancelAttach()
					return false
				}
			case interactive.ActionCancelEscape:
				if !flushRemote() || d.writeLocal == nil || d.writeLocal(escapeCancelledText) != nil {
					d.cancelAttach()
					return false
				}
			case interactive.ActionQuit:
				if !flushRemote() {
					d.cancelAttach()
				}
				d.cancelAttach()
				return false
			case interactive.ActionHelp:
				if !flushRemote() || d.writeLocal == nil || d.writeLocal(escapeHelpText) != nil {
					d.cancelAttach()
					return false
				}
			case interactive.ActionTogglePromptTimestamp:
				if !flushRemote() || d.togglePromptTimestamp == nil || d.togglePromptTimestamp() != nil {
					d.cancelAttach()
					return false
				}
			case interactive.ActionFileTransfer:
				if !flushRemote() || !d.enterFileMenu() {
					return false
				}
				return d.dispatch(data[index+1:])
			case interactive.ActionUnknownEscape:
				if !flushRemote() || d.writeLocal == nil || d.writeLocal(unknownEscapeText(action.Command)) != nil {
					d.cancelAttach()
					return false
				}
			}
		}
	}
	return flushRemote()
}

func (d *attachInputDispatcher) enterFileMenu() bool {
	d.mode = attachInputModeFileMenu
	return d.writeLocal != nil && d.writeLocal(fileTransferMenuText) == nil
}

func (d *attachInputDispatcher) dispatchFileMenuByte(value byte) {
	switch value {
	case 0x1b:
		d.cancelFileShortcut()
	case 's', 'S':
		if d.writeLocal == nil || d.writeLocal([]byte{value}) != nil {
			d.cancelAttach()
			return
		}
		d.mode = attachInputModeSendLocalPath
		d.line = d.line[:0]
		if d.writeLocal([]byte("\r\nLocal path: ")) != nil {
			d.cancelAttach()
		}
	case 'r', 'R':
		if d.writeLocal == nil || d.writeLocal([]byte{value}) != nil {
			d.cancelAttach()
			return
		}
		d.mode = attachInputModeReceiveRemotePath
		d.line = d.line[:0]
		if d.writeLocal([]byte("\r\nRemote path: ")) != nil {
			d.cancelAttach()
		}
	}
}

func (d *attachInputDispatcher) dispatchPathByte(value byte) bool {
	if d.skipLineFeed {
		d.skipLineFeed = false
		if value == '\n' {
			return true
		}
	}
	switch value {
	case 0x1b:
		d.cancelFileShortcut()
	case '\r', '\n':
		if value == '\r' {
			d.skipLineFeed = true
		}
		if d.writeLocal == nil || d.writeLocal([]byte("\r\n")) != nil {
			return false
		}
		d.finishPathLine()
	case 0x7f, '\b':
		if len(d.line) > 0 {
			d.line = d.line[:len(d.line)-1]
			if d.writeLocal == nil || d.writeLocal([]byte("\b \b")) != nil {
				return false
			}
		}
	default:
		d.line = append(d.line, value)
		if d.writeLocal == nil || d.writeLocal([]byte{value}) != nil {
			return false
		}
	}
	return true
}

func (d *attachInputDispatcher) finishPathLine() {
	value := string(d.line)
	d.line = d.line[:0]
	switch d.mode {
	case attachInputModeSendLocalPath:
		if strings.TrimSpace(value) == "" {
			d.cancelFileShortcut()
			return
		}
		d.localPath = value
		d.mode = attachInputModeSendRemotePath
		defaultRemote := defaultRemoteTransferPath(value)
		if d.writeLocal == nil || d.writeLocal([]byte("Remote path ["+defaultRemote+"]: ")) != nil {
			d.cancelAttach()
		}
	case attachInputModeSendRemotePath:
		if strings.TrimSpace(value) == "" {
			value = defaultRemoteTransferPath(d.localPath)
		}
		d.startTransfer("send", d.localPath, value)
	case attachInputModeReceiveRemotePath:
		if strings.TrimSpace(value) == "" {
			d.cancelFileShortcut()
			return
		}
		d.remotePath = value
		d.mode = attachInputModeReceiveLocalPath
		defaultLocal := defaultLocalTransferPath(value)
		if d.writeLocal == nil || d.writeLocal([]byte("Local path ["+defaultLocal+"]: ")) != nil {
			d.cancelAttach()
		}
	case attachInputModeReceiveLocalPath:
		if strings.TrimSpace(value) == "" {
			value = defaultLocalTransferPath(d.remotePath)
		}
		d.startTransfer("receive", d.remotePath, value)
	}
}

func (d *attachInputDispatcher) startTransfer(direction, firstPath, secondPath string) {
	// The mode changes before the worker starts, so a Ctrl+C read concurrently
	// with setup is always a transfer cancellation and is never sent remotely.
	d.mode = attachInputModeFileTransfer
	d.ignoreControlCUntil = time.Time{}
	d.inputIgnoredAnnounced = false
	d.transferCancellation = newFileTransferCancellation()
	if d.writeLocal == nil {
		d.cancelAttach()
		return
	}
	if d.writeTransferStatus(fileTransferStartedText(time.Now(), direction, firstPath, secondPath)) != nil {
		d.cancelAttach()
		return
	}
	done := make(chan error, 1)
	d.transferDone = done
	cancellation := d.transferCancellation
	go func() {
		// Detaching still requests the same safe cancellation through the latch,
		// but an explicit confirmed cancellation also closes the worker Context.
		// This releases protocol-marker waits that would otherwise never reach
		// their next polling boundary. Raw-block error paths retain independent
		// cleanup contexts so they can restore the remote TTY before lease release.
		workerCtx, cancelWorker := context.WithCancel(context.WithoutCancel(d.ctx))
		cancelWatchDone := make(chan struct{})
		go func() {
			select {
			case <-cancellation.ch:
				cancelWorker()
			case <-cancelWatchDone:
			}
		}()
		err := d.transferRunner(workerCtx, nonClosingAttachSession{attachSession: d.terminal, cancellation: cancellation}, localOutputWriter{write: d.writeLocal, suppressFileTransferResultText: true}, cancellation.Requested, direction, firstPath, secondPath)
		close(cancelWatchDone)
		cancelWorker()
		done <- err
	}()
}

// beginTransferCancellationConfirmation pauses the worker at its next safe
// block boundary and asks locally before converting that pause into a cancel.
// The file-transfer lease remains held while the answer is pending.
func (d *attachInputDispatcher) beginTransferCancellationConfirmation() {
	if d.transferCancellation == nil || !d.transferCancellation.BeginConfirmation() {
		return
	}
	d.mode = attachInputModeFileTransferCancelConfirm
	d.ignoreControlCUntil = time.Now().Add(controlCRecoveryWindow)
	if d.writeLocal == nil || d.writeLocal(fileTransferCancelConfirmationText) != nil {
		d.requestTransferCancellation()
		d.cancelAttach()
	}
}

// beginExternalTransferCancellationConfirmation handles a transfer owned by
// another MCP/CLI process. The Host checks the active lease atomically, so an
// ordinary terminal Ctrl+C still reaches the remote endpoint as byte 0x03.
func (d *attachInputDispatcher) beginExternalTransferCancellationConfirmation() bool {
	controller, ok := d.terminal.(fileTransferCancelController)
	if !ok {
		return false
	}
	requestID, active, err := controller.BeginFileTransferCancel(d.ctx)
	if err != nil {
		if d.writeLocal != nil {
			_ = d.writeLocal([]byte("\r\n[ChannelTerm] File transfer cancellation check failed: " + err.Error() + "\r\n"))
		}
		return true
	}
	if !active {
		return false
	}
	d.externalCancelRequest = requestID
	d.mode = attachInputModeFileTransferCancelConfirm
	d.ignoreControlCUntil = time.Now().Add(controlCRecoveryWindow)
	if d.writeLocal == nil || d.writeLocal(fileTransferCancelConfirmationText) != nil {
		d.abandonExternalTransferCancellation()
		d.cancelAttach()
	} else if presenter, ok := d.terminal.(fileTransferCancelPromptPresenter); ok {
		presenter.fileTransferCancelPromptStarted()
	}
	return true
}

// resolveTransferCancellationConfirmation consumes exactly one local answer.
// Only y/Y requests cancellation; every other byte resumes the paused worker.
func (d *attachInputDispatcher) resolveTransferCancellationConfirmation(cancel bool) {
	if d.mode != attachInputModeFileTransferCancelConfirm {
		return
	}
	if d.transferCancellation == nil {
		d.resolveExternalTransferCancellationConfirmation(cancel)
		return
	}
	d.mode = attachInputModeFileTransfer
	d.ignoreControlCUntil = time.Time{}
	d.transferCancellation.ResolveConfirmation(cancel)
	if d.writeLocal == nil {
		return
	}
	if cancel {
		_ = d.writeLocal([]byte("\r\n"))
		return
	}
	_ = d.writeLocal(fileTransferResumedText)
}

func (d *attachInputDispatcher) resolveExternalTransferCancellationConfirmation(cancel bool) {
	controller, ok := d.terminal.(fileTransferCancelController)
	requestID := d.externalCancelRequest
	if !ok || requestID == "" {
		d.mode = attachInputModeAttach
		return
	}
	if d.writeLocal != nil && cancel {
		_ = d.writeLocal([]byte("\r\n"))
	}
	state, err := controller.ResolveFileTransferCancel(d.ctx, requestID, cancel)
	d.externalCancelRequest = ""
	d.mode = attachInputModeAttach
	d.ignoreControlCUntil = time.Now().Add(controlCRecoveryWindow)
	if d.writeLocal == nil {
		return
	}
	if err != nil {
		if presenter, ok := d.terminal.(fileTransferCancelPromptPresenter); ok {
			presenter.fileTransferCancelPromptFinished()
		}
		_ = d.writeLocal([]byte("\r\n[ChannelTerm] File transfer cancellation failed: " + err.Error() + "\r\n"))
		return
	}
	if !cancel && state == "resumed" {
		_ = d.writeLocal(fileTransferResumedText)
		if presenter, ok := d.terminal.(fileTransferCancelPromptPresenter); ok {
			presenter.fileTransferCancelPromptFinished()
		}
	}
}

func (d *attachInputDispatcher) abandonExternalTransferCancellation() {
	if d.externalCancelRequest == "" {
		return
	}
	if controller, ok := d.terminal.(fileTransferCancelController); ok {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_, _ = controller.ResolveFileTransferCancel(ctx, d.externalCancelRequest, false)
		cancel()
	}
	if presenter, ok := d.terminal.(fileTransferCancelPromptPresenter); ok {
		presenter.fileTransferCancelPromptFinished()
	}
	d.externalCancelRequest = ""
}

// ignoreTransferInput keeps ordinary keyboard bytes out of the normal
// terminal_write path while this attachment owns a file-transfer lease. The
// one-time local hint confirms the lock without obscuring transfer progress.
func (d *attachInputDispatcher) ignoreTransferInput() bool {
	if d.inputIgnoredAnnounced {
		return true
	}
	d.inputIgnoredAnnounced = true
	return d.writeLocal != nil && d.writeLocal(fileTransferInputIgnoredText) == nil
}

func (d *attachInputDispatcher) requestTransferCancellation() {
	if (d.mode != attachInputModeFileTransfer && d.mode != attachInputModeFileTransferCancelConfirm) || d.transferCancellation == nil {
		return
	}
	d.transferCancellation.Request()
}

func (d *attachInputDispatcher) finishTransfer(err error) {
	cancelled := d.transferCancellation != nil && d.transferCancellation.Cancelled()
	confirmationPending := d.mode == attachInputModeFileTransferCancelConfirm
	progress := fileTransferCancellationSnapshot{}
	if d.transferCancellation != nil {
		progress = d.transferCancellation.Progress()
		// A transfer can finish its final verification before observing a newly
		// opened prompt. Wake any waiter and discard that stale confirmation.
		d.transferCancellation.ResolveConfirmation(false)
	}
	d.transferDone = nil
	d.transferCancellation = nil
	d.inputIgnoredAnnounced = false
	d.mode = attachInputModeAttach
	// Start the recovery window only after the worker has completed its raw
	// block and released its lease. On slow links the active 8 KiB block can
	// outlast the window that started when Ctrl+C was first received. The same
	// guard consumes a pending prompt answer when the transfer ended first.
	if cancelled || confirmationPending {
		d.ignoreControlCUntil = time.Now().Add(controlCRecoveryWindow)
	}
	if d.writeLocal == nil {
		return
	}
	if err == nil {
		_ = d.writeTransferStatus(fileTransferCompletedText(time.Now()))
		return
	}
	if cancelled || errors.Is(err, context.Canceled) {
		_ = d.writeTransferStatus(fileTransferCancelledSummaryText(time.Now(), progress))
		return
	}
	_ = d.writeTransferStatus(fileTransferFailedText(time.Now(), err))
}

func (d *attachInputDispatcher) cancelFileShortcut() {
	d.mode = attachInputModeAttach
	d.line = d.line[:0]
	if d.writeLocal != nil {
		_ = d.writeTransferStatus(fileTransferCancelledText(time.Now()))
	}
}

func (d *attachInputDispatcher) writeTransferStatus(data []byte) error {
	if d.writeStatus != nil {
		return d.writeStatus(data)
	}
	if d.writeLocal != nil {
		return d.writeLocal(data)
	}
	return nil
}

func (d *attachInputDispatcher) cancelAttach() {
	d.abandonExternalTransferCancellation()
	if d.stopAttach != nil {
		d.stopAttach()
	}
}

// runAttachShortcutFileTransfer adapts the existing file command to the
// dispatcher's transfer-runner contract without opening a second attachment.
func runAttachShortcutFileTransfer(ctx context.Context, attached attachSession, output io.Writer, cancelRequested func() bool, direction, firstPath, secondPath string) error {
	dependencies := fileCommandDependencies{
		newAttach: func(context.Context, string, string) (attachSession, error) {
			return attached, nil
		},
		listSessions: func(context.Context, string) ([]mcpListedSession, error) {
			return nil, errors.New("unexpected Session listing")
		},
		transferCancelRequested: cancelRequested,
	}
	if direction == "send" {
		return runFileSend(ctx, []string{firstPath, secondPath, "--session", "attached"}, output, dependencies)
	}
	if direction == "receive" {
		return runFileReceive(ctx, []string{firstPath, secondPath, "--session", "attached"}, output, dependencies)
	}
	return fmt.Errorf("unsupported attach shortcut transfer direction %q", direction)
}
