package command

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	posixpath "path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/cli/interactive"
	"github.com/akira-init1/ChannelTerm/internal/core/session"
)

const (
	mcpRemoteFileDirectory  = "/tmp/cterm/mcp-files"
	userRemoteFileDirectory = "/tmp/cterm/user-files"
)

// attachInputPump gives the attach controller exclusive, ordered ownership of
// raw console input. During a file transfer it continues receiving bytes so a
// local Ctrl+C can cancel only that transfer without ending the attachment.
type attachInputPump struct {
	results <-chan attachInputResult
	mu      sync.Mutex
	pending []byte
}

type attachInputResult struct {
	data []byte
	err  error
}

func newAttachInputPump(input io.Reader) attachInputPump {
	return newAttachInputPumpContext(context.Background(), input)
}

// newAttachInputPumpContext isolates a potentially blocking Reader while
// allowing result delivery to stop with its owning command Context.
func newAttachInputPumpContext(ctx context.Context, input io.Reader) attachInputPump {
	results := make(chan attachInputResult)
	go func() {
		defer close(results)
		buffer := make([]byte, 4*1024)
		for {
			n, err := input.Read(buffer)
			if n > 0 {
				data := append([]byte(nil), buffer[:n]...)
				if !sendAttachInputResult(ctx, results, attachInputResult{data: data}) {
					return
				}
			}
			if err != nil || n == 0 {
				sendAttachInputResult(ctx, results, attachInputResult{err: err})
				return
			}
		}
	}()
	return attachInputPump{results: results}
}

// sendAttachInputResult prevents a completed attachment or direct serial
// command from leaving its input reader blocked while publishing a final
// result. The underlying Read may remain blocked for an arbitrary io.Reader,
// but after it returns the pump no longer touches command-owned state.
func sendAttachInputResult(ctx context.Context, results chan<- attachInputResult, result attachInputResult) bool {
	select {
	case results <- result:
		return true
	case <-ctx.Done():
		return false
	}
}

func (p *attachInputPump) next(ctx context.Context) (attachInputResult, bool) {
	if result, ok := p.takePending(); ok {
		return result, true
	}
	select {
	case <-ctx.Done():
		return attachInputResult{err: ctx.Err()}, false
	case result, ok := <-p.results:
		return result, ok
	}
}

func (p *attachInputPump) takePending() (attachInputResult, bool) {
	p.mu.Lock()
	if len(p.pending) > 0 {
		data := p.pending
		p.pending = nil
		p.mu.Unlock()
		return attachInputResult{data: data}, true
	}
	p.mu.Unlock()
	return attachInputResult{}, false
}

func (p *attachInputPump) prepend(data []byte) {
	if len(data) == 0 {
		return
	}
	p.mu.Lock()
	p.pending = append(append([]byte(nil), data...), p.pending...)
	p.mu.Unlock()
}

// forwardAttachInput extends the normal local escape controller with the
// attach-only file shortcut. It batches ordinary remote bytes from each input
// read, while stopping at local actions so their following bytes stay local.
func forwardAttachInput(ctx context.Context, input io.Reader, terminal attachSession, writeLocal func([]byte) error, togglePromptTimestamp func() error, cancel context.CancelFunc) {
	forwardAttachInputWithInterrupts(ctx, input, terminal, writeLocal, togglePromptTimestamp, cancel, nil)
}

// forwardAttachInputWithInterrupts routes both raw console bytes and optional
// process-level Console interruptions through one attach input dispatcher.
func forwardAttachInputWithInterrupts(ctx context.Context, input io.Reader, terminal attachSession, writeLocal func([]byte) error, togglePromptTimestamp func() error, cancel context.CancelFunc, interrupts <-chan os.Signal) {
	forwardAttachInputWithPresentation(ctx, input, terminal, writeLocal, writeLocal, togglePromptTimestamp, cancel, interrupts)
}

// forwardAttachInputWithPresentation routes input while keeping ordinary local
// UI text distinct from status blocks that must begin at a line boundary.
func forwardAttachInputWithPresentation(ctx context.Context, input io.Reader, terminal attachSession, writeLocal func([]byte) error, writeStatus func([]byte) error, togglePromptTimestamp func() error, cancel context.CancelFunc, interrupts <-chan os.Signal) {
	pump := newAttachInputPumpContext(ctx, input)
	dispatcher := newAttachInputDispatcherWithPumpAndInterrupts(ctx, &pump, terminal, writeLocal, togglePromptTimestamp, cancel, runAttachShortcutFileTransfer, interrupts)
	dispatcher.writeStatus = writeStatus
	dispatcher.run()
}

func processAttachInput(ctx context.Context, controller *interactive.Controller, data []byte, pump *attachInputPump, terminal attachSession, writeLocal func([]byte) error, togglePromptTimestamp func() error, cancel context.CancelFunc) bool {
	remote := make([]byte, 0, len(data))
	flushRemote := func() bool {
		if len(remote) == 0 {
			return true
		}
		if err := writeSession(terminal, session.ActorUser, remote); err != nil {
			return writeLocal != nil && writeLocal(writeFailureText(err)) == nil
		}
		remote = remote[:0]
		return true
	}
	for index, value := range data {
		for _, action := range controller.Process([]byte{value}) {
			switch action.Kind {
			case interactive.ActionRemote:
				remote = append(remote, action.Data...)
			case interactive.ActionEscapePending:
				if !flushRemote() || writeLocal == nil || writeLocal(escapePendingText) != nil {
					cancel()
					return false
				}
			case interactive.ActionCancelEscape:
				if !flushRemote() || writeLocal == nil || writeLocal(escapeCancelledText) != nil {
					cancel()
					return false
				}
			case interactive.ActionQuit:
				if !flushRemote() {
					cancel()
				}
				cancel()
				return false
			case interactive.ActionHelp:
				if !flushRemote() || writeLocal == nil || writeLocal(escapeHelpText) != nil {
					cancel()
					return false
				}
			case interactive.ActionTogglePromptTimestamp:
				if !flushRemote() || togglePromptTimestamp == nil || togglePromptTimestamp() != nil {
					cancel()
					return false
				}
			case interactive.ActionFileTransfer:
				pump.prepend(data[index+1:])
				if !flushRemote() || !runAttachFileShortcut(ctx, pump, terminal, writeLocal) {
					return false
				}
				return true
			case interactive.ActionUnknownEscape:
				if !flushRemote() || writeLocal == nil || writeLocal(unknownEscapeText(action.Command)) != nil {
					cancel()
					return false
				}
			}
		}
	}
	return flushRemote()
}

func runAttachFileShortcut(ctx context.Context, pump *attachInputPump, attached attachSession, writeLocal func([]byte) error) bool {
	if writeLocal == nil || writeLocal(fileTransferMenuText) != nil {
		return false
	}
	choice, cancelled, ok := readFileTransferChoice(pump, writeLocal)
	if !ok {
		return false
	}
	if cancelled {
		_ = writeLocal(fileTransferCancelledText(time.Now()))
		return true
	}
	switch choice {
	case "s":
		return runAttachSendShortcut(ctx, pump, attached, writeLocal)
	case "r":
		return runAttachReceiveShortcut(ctx, pump, attached, writeLocal)
	}
	return true
}

// readFileTransferChoice reads the attach shortcut's local single-key menu.
// It intentionally consumes invalid keys without echoing or forwarding them to
// the Session, while retaining bytes after a completed selection for its path
// prompts.
func readFileTransferChoice(pump *attachInputPump, writeLocal func([]byte) error) (string, bool, bool) {
	for {
		result, ok := pump.next(context.Background())
		if !ok || result.err != nil {
			return "", false, false
		}
		for index, value := range result.data {
			switch value {
			case 0x1b:
				pump.prepend(result.data[index+1:])
				return "", true, true
			case 's', 'S':
				if writeLocal([]byte{value}) != nil {
					return "", false, false
				}
				pump.prepend(result.data[index+1:])
				return "s", false, true
			case 'r', 'R':
				if writeLocal([]byte{value}) != nil {
					return "", false, false
				}
				pump.prepend(result.data[index+1:])
				return "r", false, true
			}
		}
	}
}

func runAttachSendShortcut(ctx context.Context, pump *attachInputPump, attached attachSession, writeLocal func([]byte) error) bool {
	if writeLocal([]byte("\r\nLocal path: ")) != nil {
		return false
	}
	localPath, cancelled, ok := readShortcutLine(pump, writeLocal)
	if !ok {
		return false
	}
	if cancelled || strings.TrimSpace(localPath) == "" {
		_ = writeLocal(fileTransferCancelledText(time.Now()))
		return true
	}
	defaultRemote := defaultRemoteTransferPath(localPath)
	if writeLocal([]byte("Remote path ["+defaultRemote+"]: ")) != nil {
		return false
	}
	remotePath, cancelled, ok := readShortcutLine(pump, writeLocal)
	if !ok {
		return false
	}
	if cancelled {
		_ = writeLocal(fileTransferCancelledText(time.Now()))
		return true
	}
	if strings.TrimSpace(remotePath) == "" {
		remotePath = defaultRemote
	}
	return runAttachShortcutTransfer(ctx, pump, attached, writeLocal, "send", localPath, remotePath)
}

func runAttachReceiveShortcut(ctx context.Context, pump *attachInputPump, attached attachSession, writeLocal func([]byte) error) bool {
	if writeLocal([]byte("\r\nRemote path: ")) != nil {
		return false
	}
	remotePath, cancelled, ok := readShortcutLine(pump, writeLocal)
	if !ok {
		return false
	}
	if cancelled || strings.TrimSpace(remotePath) == "" {
		_ = writeLocal(fileTransferCancelledText(time.Now()))
		return true
	}
	defaultLocal := defaultLocalTransferPath(remotePath)
	if writeLocal([]byte("Local path ["+defaultLocal+"]: ")) != nil {
		return false
	}
	localPath, cancelled, ok := readShortcutLine(pump, writeLocal)
	if !ok {
		return false
	}
	if cancelled {
		_ = writeLocal(fileTransferCancelledText(time.Now()))
		return true
	}
	if strings.TrimSpace(localPath) == "" {
		localPath = defaultLocal
	}
	return runAttachShortcutTransfer(ctx, pump, attached, writeLocal, "receive", remotePath, localPath)
}

func runAttachShortcutTransfer(ctx context.Context, pump *attachInputPump, attached attachSession, writeLocal func([]byte) error, direction, firstPath, secondPath string) bool {
	return runAttachShortcutTransferWithRunner(ctx, pump, attached, writeLocal, direction, firstPath, secondPath, func(workerCtx context.Context, transfer attachSession, output io.Writer) error {
		cancelRequested := func() bool { return false }
		if requester, ok := transfer.(interface{ FileTransferCancelRequested() bool }); ok {
			cancelRequested = requester.FileTransferCancelRequested
		}
		return runAttachShortcutFileTransfer(workerCtx, transfer, output, cancelRequested, direction, firstPath, secondPath)
	})
}

// runAttachShortcutTransferWithRunner owns the local cancellation lifecycle
// around one file-transfer worker. Ctrl+C requests a safe stop at a chunk
// boundary; it never cancels the attachment context that owns the MCP client.
func runAttachShortcutTransferWithRunner(ctx context.Context, pump *attachInputPump, attached attachSession, writeLocal func([]byte) error, direction, firstPath, secondPath string, runner func(context.Context, attachSession, io.Writer) error) bool {
	if direction == "send" {
		if writeLocal([]byte("\r\nSending "+filepath.Base(firstPath)+" -> "+secondPath+"\r\nCtrl+C to cancel\r\n")) != nil {
			return false
		}
	} else if writeLocal([]byte("\r\nReceiving "+firstPath+" -> "+secondPath+"\r\nCtrl+C to cancel\r\n")) != nil {
		return false
	}
	cancellation := newFileTransferCancellation()
	done := make(chan error, 1)
	go func() {
		done <- runner(ctx, nonClosingAttachSession{attachSession: attached, cancellation: cancellation}, localOutputWriter{write: writeLocal})
	}()
	requestCancellation := func() {
		cancellation.Request()
	}
	for {
		if result, ok := pump.takePending(); ok {
			if index := bytes.IndexByte(result.data, 0x03); index >= 0 {
				pump.prepend(result.data[index+1:])
				requestCancellation()
				<-done
				return true
			}
			continue
		}
		select {
		case err := <-done:
			if err != nil {
				if !cancellation.Requested() && !errors.Is(err, context.Canceled) {
					_ = writeLocal([]byte("\r\n[ChannelTerm] File transfer failed: " + err.Error() + "\r\n"))
				}
			}
			return true
		case result, ok := <-pump.results:
			if !ok || result.err != nil {
				requestCancellation()
				<-done
				return false
			}
			if index := bytes.IndexByte(result.data, 0x03); index >= 0 {
				pump.prepend(result.data[index+1:])
				requestCancellation()
				<-done
				return true
			}
		}
	}
}

func readShortcutLine(pump *attachInputPump, writeLocal func([]byte) error) (string, bool, bool) {
	var line []byte
	for {
		result, ok := pump.next(context.Background())
		if !ok {
			return "", false, false
		}
		if result.err != nil {
			return "", false, false
		}
		for index, value := range result.data {
			switch value {
			case 0x1b:
				pump.prepend(result.data[index+1:])
				return "", true, true
			case '\r', '\n':
				_ = writeLocal([]byte("\r\n"))
				pump.prepend(result.data[index+1:])
				return string(line), false, true
			case 0x7f, '\b':
				if len(line) > 0 {
					line = line[:len(line)-1]
					_ = writeLocal([]byte("\b \b"))
				}
			default:
				line = append(line, value)
				_ = writeLocal([]byte{value})
			}
		}
	}
}

func defaultRemoteTransferPath(localPath string) string {
	return defaultUserRemoteTransferPath(localPath)
}

func defaultMCPRemoteTransferPath(localPath string) string {
	return posixpath.Join(mcpRemoteFileDirectory, filepath.Base(localPath))
}

func defaultUserRemoteTransferPath(localPath string) string {
	return posixpath.Join(userRemoteFileDirectory, filepath.Base(localPath))
}

func defaultLocalTransferPath(remotePath string) string {
	return "." + string(filepath.Separator) + posixpath.Base(remotePath)
}

type localOutputWriter struct {
	write                          func([]byte) error
	suppressFileTransferResultText bool
	deferredSummary                *fileTransferSummaryBuffer
}

// fileTransferSummaryBuffer transfers a successful regular-file summary from
// the worker to the input dispatcher. The worker completion channel provides
// ordering; the mutex also keeps test and future callers safe if that changes.
type fileTransferSummaryBuffer struct {
	mu   sync.Mutex
	text string
}

func (b *fileTransferSummaryBuffer) store(text string) {
	b.mu.Lock()
	b.text = text
	b.mu.Unlock()
}

func (b *fileTransferSummaryBuffer) take() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	text := b.text
	b.text = ""
	return text
}

func (w localOutputWriter) Write(data []byte) (int, error) {
	// The attached terminal can be in a mode where LF advances a row without
	// returning to column zero. File-transfer output follows an in-place
	// progress frame, so make its ordinary line endings explicit before writing
	// them locally. Preserve existing CRLF sequences from callers.
	local := make([]byte, 0, len(data)+2)
	for index, value := range data {
		if value == '\n' && (index == 0 || data[index-1] != '\r') {
			local = append(local, '\r')
		}
		local = append(local, value)
	}
	if err := w.write(local); err != nil {
		return 0, err
	}
	return len(data), nil
}

func (w localOutputWriter) suppressFileTransferResult() bool {
	return w.suppressFileTransferResultText
}

func (w localOutputWriter) deferFileTransferVerificationSummary(text string) bool {
	if w.deferredSummary == nil {
		return false
	}
	w.deferredSummary.store(text)
	return true
}

// fileTransferCancellation coordinates a local confirmation prompt with the
// file-transfer worker. Requested blocks at a safe transfer boundary while a
// decision is pending, without cancelling the attachment context or MCP client.
type fileTransferCancellation struct {
	mu           sync.Mutex
	state        fileTransferCancellationState
	stateChanged chan struct{}
	done         sync.Once
	ch           chan struct{}
	progress     fileTransferCancellationSnapshot
}

type fileTransferCancellationSnapshot struct {
	transferred int64
	total       int64
	percent     float64
}

type fileTransferCancellationState uint8

const (
	fileTransferCancellationRunning fileTransferCancellationState = iota
	fileTransferCancellationConfirming
	fileTransferCancellationCancelled
)

func newFileTransferCancellation() *fileTransferCancellation {
	return &fileTransferCancellation{ch: make(chan struct{}), stateChanged: make(chan struct{})}
}

// BeginConfirmation asks boundary probes to pause until ResolveConfirmation.
func (c *fileTransferCancellation) BeginConfirmation() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.state != fileTransferCancellationRunning {
		return false
	}
	c.state = fileTransferCancellationConfirming
	c.signalStateChangeLocked()
	return true
}

// ResolveConfirmation either converts a pending confirmation into a durable
// cancellation or lets boundary probes continue the transfer.
func (c *fileTransferCancellation) ResolveConfirmation(cancel bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.state != fileTransferCancellationConfirming {
		return
	}
	if cancel {
		c.state = fileTransferCancellationCancelled
		c.done.Do(func() { close(c.ch) })
	} else {
		c.state = fileTransferCancellationRunning
	}
	c.signalStateChangeLocked()
}

// Request forces cancellation for attachment shutdown and input failure paths
// where waiting for an interactive confirmation is no longer possible.
func (c *fileTransferCancellation) Request() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.state == fileTransferCancellationCancelled {
		return
	}
	c.state = fileTransferCancellationCancelled
	c.done.Do(func() { close(c.ch) })
	c.signalStateChangeLocked()
}

// Requested waits out a pending confirmation and reports the chosen result.
func (c *fileTransferCancellation) Requested() bool {
	c.mu.Lock()
	for c.state == fileTransferCancellationConfirming {
		changed := c.stateChanged
		c.mu.Unlock()
		<-changed
		c.mu.Lock()
	}
	cancelled := c.state == fileTransferCancellationCancelled
	c.mu.Unlock()
	return cancelled
}

// Cancelled reports the terminal state without blocking on a pending prompt.
func (c *fileTransferCancellation) Cancelled() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state == fileTransferCancellationCancelled
}

// ObserveProgress retains the most recent confirmed byte counts for the local
// cancellation result. It does not affect the transfer decision state.
func (c *fileTransferCancellation) ObserveProgress(metadata map[string]any) {
	transferred := fileTransferEventTransferred(metadata)
	total := max(0, fileTransferEventInteger(metadata, "total"))
	percent := fileTransferEventPercent(metadata, transferred, total)
	c.mu.Lock()
	c.progress = fileTransferCancellationSnapshot{transferred: transferred, total: total, percent: percent}
	c.mu.Unlock()
}

// Progress returns the latest confirmed transfer snapshot without blocking on
// a pending confirmation.
func (c *fileTransferCancellation) Progress() fileTransferCancellationSnapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.progress
}

func (c *fileTransferCancellation) signalStateChangeLocked() {
	close(c.stateChanged)
	c.stateChanged = make(chan struct{})
}

// nonClosingAttachSession lets the existing file CLI implementation operate
// on the current attachment without turning a completed shortcut into a detach.
type nonClosingAttachSession struct {
	attachSession
	cancellation *fileTransferCancellation
}

func (nonClosingAttachSession) Close() error { return nil }

func (s nonClosingAttachSession) WriteContext(ctx context.Context, request session.WriteRequest) (int, error) {
	if contextual, ok := s.attachSession.(interface {
		WriteContext(context.Context, session.WriteRequest) (int, error)
	}); ok {
		return contextual.WriteContext(ctx, request)
	}
	return s.attachSession.Write(request)
}

func (s nonClosingAttachSession) AcquireFileTransferLease(ctx context.Context) error {
	lease, ok := s.attachSession.(fileLeaseSession)
	if !ok {
		return errors.New("attached Session does not support file transfer leases")
	}
	return lease.AcquireFileTransferLease(ctx)
}

func (s nonClosingAttachSession) ReleaseFileTransferLease(ctx context.Context) error {
	lease, ok := s.attachSession.(fileLeaseSession)
	if !ok {
		return nil
	}
	return lease.ReleaseFileTransferLease(ctx)
}

func (s nonClosingAttachSession) ReportFileTransferEvent(ctx context.Context, typ session.EventType, metadata map[string]any) error {
	if s.cancellation != nil {
		s.cancellation.ObserveProgress(metadata)
	}
	reporter, ok := s.attachSession.(fileTransferEventReporter)
	if !ok {
		return nil
	}
	return reporter.ReportFileTransferEvent(ctx, typ, metadata)
}

// FileTransferCancelRequested lets Core stop before starting a new regular
// file chunk after the local transfer worker has completed the current one.
func (s nonClosingAttachSession) FileTransferCancelRequested() bool {
	if s.cancellation != nil && s.cancellation.Requested() {
		return true
	}
	requester, ok := s.attachSession.(interface{ FileTransferCancelRequested() bool })
	return ok && requester.FileTransferCancelRequested()
}

// FileTransferCancellationCheckpoint combines the local shortcut prompt with
// Host control requests made by other attachments without losing MCP errors.
func (s nonClosingAttachSession) FileTransferCancellationCheckpoint(ctx context.Context) error {
	if s.cancellation != nil && s.cancellation.Requested() {
		return context.Canceled
	}
	if checkpoint, ok := s.attachSession.(interface {
		FileTransferCancellationCheckpoint(context.Context) error
	}); ok {
		return checkpoint.FileTransferCancellationCheckpoint(ctx)
	}
	if requester, ok := s.attachSession.(interface{ FileTransferCancelRequested() bool }); ok && requester.FileTransferCancelRequested() {
		return context.Canceled
	}
	return nil
}

var fileTransferMenuText = []byte("\r\nFile transfer:\r\n  s  Send PC -> Board\r\n  r  Receive Board -> PC\r\n  Esc  Cancel\r\nSelect: ")
var fileTransferInputIgnoredText = []byte("\r\n[ChannelTerm] Input ignored during file transfer. Ctrl+C to cancel.\r\n")

var terminalCommandInputIgnoredText = []byte("\r\n[ChannelTerm] Input ignored while an AI command is running. Cancel it from the MCP client.\r\n")
var fileTransferCancelConfirmationText = []byte("\r\n[ChannelTerm] Cancel file transfer? [y/N]: ")
var fileTransferResumedText = []byte("\r\n[ChannelTerm] File transfer resumed\r\n")

func fileTransferStatusPrefix(now time.Time) string {
	return "[" + now.In(time.Local).Format("15:04:05") + "] [ChannelTerm] "
}

func fileTransferCancelledText(now time.Time) []byte {
	return []byte(fileTransferStatusPrefix(now) + "File transfer cancelled\r\n")
}

func fileTransferCancelledSummaryText(now time.Time, progress fileTransferCancellationSnapshot) []byte {
	return []byte(fmt.Sprintf("%sFile transfer cancelled\r\n  Transferred: %d/%d bytes (%.1f%%)\r\n  Reason     : cancelled by user\r\n", fileTransferStatusPrefix(now), progress.transferred, progress.total, progress.percent))
}

func fileTransferCompletedText(now time.Time) []byte {
	return []byte(fileTransferStatusPrefix(now) + "File transfer completed\r\n")
}

func fileTransferFailedText(now time.Time, err error) []byte {
	return []byte(fileTransferStatusPrefix(now) + "File transfer failed: " + err.Error() + "\r\n")
}

// fileTransferStartedText reports the local input-lock boundary before the
// worker begins protocol I/O. It is CLI presentation only and never enters the
// Session byte stream.
func fileTransferStartedText(now time.Time, direction, firstPath, secondPath string) []byte {
	localPath, remotePath := firstPath, secondPath
	if direction == "receive" {
		localPath, remotePath = secondPath, firstPath
	}
	return []byte(fileTransferStatusPrefix(now) + "File transfer started: " + localPath + " -> " + remotePath + "\r\n[ChannelTerm] Terminal input locked. Ctrl+C to cancel.\r\n")
}
