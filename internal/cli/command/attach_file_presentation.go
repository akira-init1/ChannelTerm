package command

import (
	"context"
	"fmt"
	"sync"

	"github.com/akira-init1/ChannelTerm/internal/core/session"
)

// fileTransferPresentation gates one attach client's local terminal rendering.
// It deliberately owns no Session cursor or terminal bytes: the caller keeps
// advancing its cursor while this gate suppresses only local presentation.
type fileTransferPresentation struct {
	mu     sync.Mutex
	active bool
}

func newFileTransferPresentation() *fileTransferPresentation {
	return &fileTransferPresentation{}
}

func (p *fileTransferPresentation) suppressRaw() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.active
}

// handle applies a structured file-transfer event before optionally rendering
// a concise observer status. The transfer owner renders the richer formatter
// from its local file command, so local is used only to avoid duplicate text.
func (p *fileTransferPresentation) handle(event session.Event, local bool, write func([]byte) error) error {
	var text string
	p.mu.Lock()
	switch event.Type {
	case session.EventFileTransferStarted:
		p.active = true
		if !local {
			text = "[ChannelTerm] File transfer started: " + fileTransferEventPaths(event.Metadata) + "\r\n"
		}
	case session.EventFileTransferProgress:
		// A retained observer can begin at a progress event when older events
		// have expired. Treat it as active rather than risking raw payload
		// presentation until a terminal transfer event arrives.
		p.active = true
		if !local {
			text = fmt.Sprintf("[ChannelTerm] File transfer: %.1f%%\r\n", fileTransferEventNumber(event.Metadata, "percent"))
		}
	case session.EventFileTransferCompleted:
		p.active = false
		if !local {
			text = "[ChannelTerm] File transfer completed\r\n"
		}
	case session.EventFileTransferFailed:
		p.active = false
		if !local {
			text = "[ChannelTerm] File transfer failed"
			if message := fileTransferEventString(event.Metadata, "error"); message != "" {
				text += ": " + message
			}
			text += "\r\n"
		}
	}
	p.mu.Unlock()
	if text == "" || write == nil {
		return nil
	}
	return write([]byte(text))
}

func fileTransferEventPaths(metadata map[string]any) string {
	localPath := fileTransferEventString(metadata, "local_path")
	remotePath := fileTransferEventString(metadata, "remote_path")
	if metadata["direction"] == "receive" {
		localPath, remotePath = remotePath, localPath
	}
	if localPath == "" || remotePath == "" {
		return "in progress"
	}
	return localPath + " -> " + remotePath
}

func fileTransferEventString(metadata map[string]any, key string) string {
	value, ok := metadata[key]
	if !ok {
		return ""
	}
	text, ok := value.(string)
	if !ok {
		return ""
	}
	return text
}

func fileTransferEventNumber(metadata map[string]any, key string) float64 {
	value, ok := metadata[key]
	if !ok {
		return 0
	}
	switch number := value.(type) {
	case float64:
		return number
	case float32:
		return float64(number)
	case int:
		return float64(number)
	case int64:
		return float64(number)
	}
	return 0
}

// writeAttachedTerminalOutput preserves normal raw rendering unless a current
// file-transfer event has gated this attachment's local presentation.
func writeAttachedTerminalOutput(presentation *fileTransferPresentation, write func([]byte) error, data []byte) (bool, error) {
	if presentation != nil && presentation.suppressRaw() {
		return false, nil
	}
	if err := write(data); err != nil {
		return false, err
	}
	return true, nil
}

// forwardFileTransferEvents observes the same structured event stream used by
// MCP clients. It never reads or transforms terminal bytes, and each attach
// owns its own event cursor.
func forwardFileTransferEvents(ctx context.Context, events attachEventSession, cursor session.EventCursor, attached attachSession, presentation *fileTransferPresentation, write func([]byte) error) {
	for {
		chunk, err := events.ReadEvents(ctx, cursor, 32)
		if err != nil {
			return
		}
		cursor = chunk.Next
		for _, event := range chunk.Events {
			local := false
			owner, ownsTransfer := attached.(localFileTransferPresentation)
			if ownsTransfer {
				local = owner.fileTransferPresentationIsLocal()
			}
			if presentation.handle(event, local, write) != nil {
				return
			}
			if ownsTransfer && local && (event.Type == session.EventFileTransferCompleted || event.Type == session.EventFileTransferFailed) {
				owner.fileTransferPresentationEnded()
			}
		}
	}
}
