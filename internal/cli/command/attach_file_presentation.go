package command

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/akira-init1/ChannelTerm/internal/core/session"
)

// fileTransferPresentation gates one attach client's local terminal rendering.
// It deliberately owns no Session cursor or terminal bytes: the caller keeps
// advancing its cursor while this gate suppresses only local presentation.
type fileTransferPresentation struct {
	mu sync.Mutex

	// legacyActive covers old Hosts that do not include cursor metadata with
	// their lease events. Current Hosts use precise raw-output ranges instead.
	legacyActive bool
	openStart    *session.OutputCursor
	ranges       []fileTransferOutputRange
}

type fileTransferOutputRange struct {
	start session.OutputCursor
	end   session.OutputCursor
}

func newFileTransferPresentation() *fileTransferPresentation {
	return &fileTransferPresentation{}
}

// handle applies a structured file-transfer event before optionally rendering
// a concise observer status. The transfer owner renders the richer formatter
// from its local file command, so local is used only to avoid duplicate text.
func (p *fileTransferPresentation) handle(event session.Event, local bool, write func([]byte) error) error {
	var text string
	p.mu.Lock()
	switch event.Type {
	case session.EventLeaseAcquired:
		if event.Metadata["type"] == "file-transfer" {
			if cursor, ok := fileTransferEventCursor(event.Metadata); ok {
				p.openStart = &cursor
				p.legacyActive = false
			} else {
				p.legacyActive = true
			}
		}
	case session.EventLeaseReleased:
		if event.Metadata["type"] == "file-transfer" {
			if p.openStart != nil {
				if cursor, ok := fileTransferEventCursor(event.Metadata); ok && cursor >= *p.openStart {
					outputRange := fileTransferOutputRange{start: *p.openStart, end: cursor}
					if len(p.ranges) == 0 || p.ranges[len(p.ranges)-1] != outputRange {
						p.ranges = append(p.ranges, outputRange)
					}
				}
				p.openStart = nil
			}
			p.legacyActive = false
		}
	case session.EventFileTransferStarted:
		if p.openStart == nil {
			p.legacyActive = true
		}
		if !local {
			text = fileTransferStatusPrefix(event.Timestamp) + "File transfer started: " + fileTransferEventPaths(event.Metadata) + "\r\n"
		}
	case session.EventFileTransferProgress:
		// A retained observer can begin at a progress event when older events
		// have expired. Treat it as active rather than risking raw payload
		// presentation until a terminal transfer event arrives.
		if p.openStart == nil {
			p.legacyActive = true
		}
		if !local {
			text = fmt.Sprintf("[ChannelTerm] File transfer: %.1f%%\r\n", fileTransferEventNumber(event.Metadata, "percent"))
		}
	case session.EventFileTransferCompleted:
		if p.openStart == nil {
			p.legacyActive = false
		}
		if !local {
			text = fileTransferStatusPrefix(event.Timestamp) + "File transfer completed\r\n"
		}
	case session.EventFileTransferFailed:
		if p.openStart == nil {
			p.legacyActive = false
		}
		if !local {
			text = fileTransferStatusPrefix(event.Timestamp)
			if fileTransferEventCancelled(event.Metadata) {
				text += "File transfer cancelled"
			} else {
				text += "File transfer failed"
			}
			if message := fileTransferEventString(event.Metadata, "error"); message != "" && !fileTransferEventCancelled(event.Metadata) {
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

func fileTransferEventCancelled(metadata map[string]any) bool {
	return strings.EqualFold(strings.TrimSpace(fileTransferEventString(metadata, "error")), "context canceled")
}

func fileTransferEventCursor(metadata map[string]any) (session.OutputCursor, bool) {
	value, ok := metadata["output_cursor"]
	if !ok {
		return 0, false
	}
	switch cursor := value.(type) {
	case uint64:
		return session.OutputCursor(cursor), true
	case int64:
		if cursor >= 0 {
			return session.OutputCursor(cursor), true
		}
	case float64:
		if cursor >= 0 && cursor == float64(uint64(cursor)) {
			return session.OutputCursor(cursor), true
		}
	}
	return 0, false
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
	return writeAttachedTerminalOutputAt(presentation, write, session.OutputCursor(len(data)), data)
}

// writeAttachedTerminalOutputAt renders only raw bytes outside semantic
// file-transfer ranges. It never examines terminal content, so marker-shaped
// user output and arbitrary binary remain intact whenever their cursor range is
// not owned by the internal operation.
func writeAttachedTerminalOutputAt(presentation *fileTransferPresentation, write func([]byte) error, next session.OutputCursor, data []byte) (bool, error) {
	if len(data) == 0 {
		return false, nil
	}
	start := next - session.OutputCursor(len(data))
	parts := [][]byte{data}
	if presentation != nil {
		parts = presentation.visibleParts(start, data)
	}
	rendered := false
	for _, part := range parts {
		if len(part) == 0 {
			continue
		}
		if err := write(part); err != nil {
			return false, err
		}
		rendered = true
	}
	return rendered, nil
}

func (p *fileTransferPresentation) visibleParts(start session.OutputCursor, data []byte) [][]byte {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.legacyActive {
		return nil
	}
	parts := make([][]byte, 0, 1)
	visibleStart := -1
	for index := range data {
		cursor := start + session.OutputCursor(index)
		hidden := p.openStart != nil && cursor >= *p.openStart
		if !hidden {
			for _, outputRange := range p.ranges {
				if cursor >= outputRange.start && cursor < outputRange.end {
					hidden = true
					break
				}
			}
		}
		if hidden {
			if visibleStart >= 0 {
				parts = append(parts, data[visibleStart:index])
				visibleStart = -1
			}
			continue
		}
		if visibleStart < 0 {
			visibleStart = index
		}
	}
	if visibleStart >= 0 {
		parts = append(parts, data[visibleStart:])
	}
	return parts
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

// refreshFileTransferPresentation synchronously applies retained lifecycle
// state before raw output is rendered. Lease acquisition is published by the
// Host before the transfer client can submit its first internal write, so this
// closes the race between the independent event and output cursors without
// inspecting terminal content.
func refreshFileTransferPresentation(events attachEventSession, presentation *fileTransferPresentation) {
	if events == nil || presentation == nil {
		return
	}
	chunk, err := events.ReadRecentEvents(session.DefaultEventBufferCapacity)
	if err != nil {
		return
	}
	for _, event := range chunk.Events {
		_ = presentation.handle(event, true, nil)
	}
}
