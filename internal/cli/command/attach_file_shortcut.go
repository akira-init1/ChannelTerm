package command

import (
	"bytes"
	"context"
	"errors"
	"io"
	posixpath "path"
	"path/filepath"
	"strings"
	"sync"

	"github.com/akira-init1/ChannelTerm/internal/cli/interactive"
	"github.com/akira-init1/ChannelTerm/internal/core/session"
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
	results := make(chan attachInputResult)
	go func() {
		defer close(results)
		buffer := make([]byte, 4*1024)
		for {
			n, err := input.Read(buffer)
			if n > 0 {
				data := append([]byte(nil), buffer[:n]...)
				results <- attachInputResult{data: data}
			}
			if err != nil || n == 0 {
				results <- attachInputResult{err: err}
				return
			}
		}
	}()
	return attachInputPump{results: results}
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
	pump := newAttachInputPump(input)
	controller := interactive.NewController(interactive.DefaultEscapeByte)
	for {
		result, ok := pump.next(ctx)
		if !ok || result.err != nil {
			return
		}
		if !processAttachInput(ctx, controller, result.data, &pump, terminal, writeLocal, togglePromptTimestamp, cancel) {
			return
		}
	}
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
		_ = writeLocal(fileTransferCancelledText)
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
		_ = writeLocal(fileTransferCancelledText)
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
		_ = writeLocal(fileTransferCancelledText)
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
		_ = writeLocal(fileTransferCancelledText)
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
		_ = writeLocal(fileTransferCancelledText)
		return true
	}
	if strings.TrimSpace(localPath) == "" {
		localPath = defaultLocal
	}
	return runAttachShortcutTransfer(ctx, pump, attached, writeLocal, "receive", remotePath, localPath)
}

func runAttachShortcutTransfer(ctx context.Context, pump *attachInputPump, attached attachSession, writeLocal func([]byte) error, direction, firstPath, secondPath string) bool {
	return runAttachShortcutTransferWithRunner(ctx, pump, attached, writeLocal, direction, firstPath, secondPath, func(workerCtx context.Context, transfer attachSession, output io.Writer) error {
		return runAttachShortcutFileTransfer(workerCtx, transfer, output, direction, firstPath, secondPath)
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

func runAttachShortcutFileTransfer(ctx context.Context, attached attachSession, output io.Writer, direction, firstPath, secondPath string) error {
	dependencies := fileCommandDependencies{
		newAttach: func(context.Context, string, string) (attachSession, error) {
			return attached, nil
		},
		listSessions: func(context.Context, string) ([]mcpListedSession, error) {
			return nil, errors.New("unexpected Session listing")
		},
	}
	if direction == "send" {
		return runFileSend(ctx, []string{firstPath, secondPath, "--session", "attached"}, output, dependencies)
	}
	return runFileReceive(ctx, []string{firstPath, secondPath, "--session", "attached"}, output, dependencies)
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
	return posixpath.Join("/tmp", filepath.Base(localPath))
}

func defaultLocalTransferPath(remotePath string) string {
	return "." + string(filepath.Separator) + posixpath.Base(remotePath)
}

type localOutputWriter struct{ write func([]byte) error }

func (w localOutputWriter) Write(data []byte) (int, error) {
	if err := w.write(data); err != nil {
		return 0, err
	}
	return len(data), nil
}

// fileTransferCancellation coordinates a local Ctrl+C with the file-transfer
// worker without cancelling the attachment context or its MCP client.
type fileTransferCancellation struct {
	done sync.Once
	ch   chan struct{}
}

func newFileTransferCancellation() *fileTransferCancellation {
	return &fileTransferCancellation{ch: make(chan struct{})}
}

func (c *fileTransferCancellation) Request() {
	c.done.Do(func() { close(c.ch) })
}

func (c *fileTransferCancellation) Requested() bool {
	select {
	case <-c.ch:
		return true
	default:
		return false
	}
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
	reporter, ok := s.attachSession.(fileTransferEventReporter)
	if !ok {
		return nil
	}
	return reporter.ReportFileTransferEvent(ctx, typ, metadata)
}

// FileTransferCancelRequested lets Core stop before starting a new regular
// file chunk after the local transfer worker has completed the current one.
func (s nonClosingAttachSession) FileTransferCancelRequested() bool {
	return s.cancellation != nil && s.cancellation.Requested()
}

var fileTransferMenuText = []byte("\r\nFile transfer:\r\n  s  Send PC -> Board\r\n  r  Receive Board -> PC\r\n  Esc  Cancel\r\nSelect: ")
var fileTransferCancelledText = []byte("\r\n[ChannelTerm] File transfer cancelled\r\n")
