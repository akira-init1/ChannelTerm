package command

import (
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
	choice, cancelled, ok := readShortcutLine(pump, writeLocal)
	if !ok {
		return false
	}
	if cancelled || strings.TrimSpace(choice) == "" {
		_ = writeLocal(fileTransferCancelledText)
		return true
	}
	switch strings.ToLower(strings.TrimSpace(choice)) {
	case "s":
		return runAttachSendShortcut(ctx, pump, attached, writeLocal)
	case "r":
		return runAttachReceiveShortcut(ctx, pump, attached, writeLocal)
	default:
		_ = writeLocal([]byte("\r\n[ChannelTerm] Unknown file transfer option.\r\n"))
		return true
	}
}

func runAttachSendShortcut(ctx context.Context, pump *attachInputPump, attached attachSession, writeLocal func([]byte) error) bool {
	if writeLocal([]byte("\r\nLocal file: ")) != nil {
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
	if writeLocal([]byte("\r\nRemote file: ")) != nil {
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
	if direction == "send" {
		if writeLocal([]byte("\r\nSending "+filepath.Base(firstPath)+" -> "+secondPath+"\r\nCtrl+C to cancel\r\n")) != nil {
			return false
		}
	} else if writeLocal([]byte("\r\nReceiving "+firstPath+" -> "+secondPath+"\r\nCtrl+C to cancel\r\n")) != nil {
		return false
	}
	transferCtx, transferCancel := context.WithCancel(ctx)
	defer transferCancel()
	done := make(chan error, 1)
	go func() {
		dependencies := fileCommandDependencies{
			newAttach: func(context.Context, string, string) (attachSession, error) {
				return nonClosingAttachSession{attachSession: attached}, nil
			},
			listSessions: func(context.Context, string) ([]mcpListedSession, error) {
				return nil, errors.New("unexpected Session listing")
			},
		}
		if direction == "send" {
			done <- runFileSend(transferCtx, []string{firstPath, secondPath, "--session", "attached"}, localOutputWriter{write: writeLocal}, dependencies)
			return
		}
		done <- runFileReceive(transferCtx, []string{firstPath, secondPath, "--session", "attached"}, localOutputWriter{write: writeLocal}, dependencies)
	}()
	for {
		if result, ok := pump.takePending(); ok {
			if strings.ContainsRune(string(result.data), 0x03) {
				transferCancel()
				<-done
				_ = writeLocal(fileTransferCancelledText)
				return true
			}
			continue
		}
		select {
		case err := <-done:
			if err != nil {
				if errors.Is(err, context.Canceled) {
					_ = writeLocal(fileTransferCancelledText)
				} else {
					_ = writeLocal([]byte("\r\n[ChannelTerm] File transfer failed: " + err.Error() + "\r\n"))
				}
			}
			return true
		case result, ok := <-pump.results:
			if !ok || result.err != nil {
				transferCancel()
				<-done
				return false
			}
			if strings.ContainsRune(string(result.data), 0x03) {
				transferCancel()
				<-done
				_ = writeLocal(fileTransferCancelledText)
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

// nonClosingAttachSession lets the existing file CLI implementation operate
// on the current attachment without turning a completed shortcut into a detach.
type nonClosingAttachSession struct{ attachSession }

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

var fileTransferMenuText = []byte("\r\nFile transfer:\r\n  s  Send PC -> Board\r\n  r  Receive Board -> PC\r\n  Esc  Cancel\r\n")
var fileTransferCancelledText = []byte("\r\n[ChannelTerm] File transfer cancelled\r\n")
