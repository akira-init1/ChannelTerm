package command

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

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

	// The observer progress frame ends with a carriage return so later events
	// can overwrite it. Retain its last confirmed state to terminate that line
	// cleanly and to summarize legacy failure events that omit byte metadata.
	progressRendered bool
	transferred      int64
	total            int64
	percent          float64
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
		p.progressRendered = false
		p.transferred = 0
		p.total = max(0, fileTransferEventInteger(event.Metadata, "total"))
		p.percent = 0
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
		p.transferred = fileTransferEventTransferred(event.Metadata)
		p.total = max(0, fileTransferEventInteger(event.Metadata, "total"))
		p.percent = fileTransferEventPercent(event.Metadata, p.transferred, p.total)
		if !local {
			text = "\r" + fileTransferTransferredText(event.Timestamp, p.transferred, p.total, p.percent) + "\x1b[K\r"
			p.progressRendered = true
		}
	case session.EventFileTransferCompleted:
		if p.openStart == nil {
			p.legacyActive = false
		}
		if !local {
			if p.progressRendered {
				text = "\r\n"
			}
			text += fileTransferStatusPrefix(event.Timestamp) + "File transfer completed\r\n"
			if localDigest, remoteDigest, ok := fileTransferEventDigests(event.Metadata); ok {
				text += "  Local SHA-256 : " + localDigest + "\r\n"
				text += "  Remote SHA-256: " + remoteDigest + "\r\n"
				text += "  Verify        : " + fileTransferVerifyResult(localDigest, remoteDigest) + "\r\n"
			}
		}
		p.progressRendered = false
	case session.EventFileTransferFailed:
		if p.openStart == nil {
			p.legacyActive = false
		}
		if !local {
			if p.progressRendered {
				text = "\r\n"
			}
			transferred := fileTransferEventTransferred(event.Metadata)
			if transferred == 0 && p.transferred > 0 {
				transferred = p.transferred
			}
			total := max(0, fileTransferEventInteger(event.Metadata, "total"))
			if total == 0 && p.total > 0 {
				total = p.total
			}
			percent := fileTransferEventPercent(event.Metadata, transferred, total)
			if percent == 0 && transferred == p.transferred && total == p.total {
				percent = p.percent
			}
			text += fileTransferStatusPrefix(event.Timestamp)
			if fileTransferEventCancelled(event.Metadata) {
				text += "File transfer cancelled\r\n"
				text += fmt.Sprintf("  Transferred: %d/%d bytes (%.1f%%)\r\n", transferred, total, percent)
				text += "  Reason     : cancelled by user\r\n"
			} else {
				text += "File transfer failed\r\n"
				text += fmt.Sprintf("  Transferred: %d/%d bytes (%.1f%%)\r\n", transferred, total, percent)
				text += "  Error      : " + fileTransferEventString(event.Metadata, "error") + "\r\n"
			}
		}
		p.progressRendered = false
	}
	p.mu.Unlock()
	if text == "" || write == nil {
		return nil
	}
	return write([]byte(text))
}

func fileTransferEventCancelled(metadata map[string]any) bool {
	reason := strings.TrimSpace(fileTransferEventString(metadata, "reason"))
	message := strings.TrimSpace(fileTransferEventString(metadata, "error"))
	return strings.EqualFold(reason, fileTransferUserCancelled) ||
		strings.EqualFold(message, fileTransferUserCancelled) ||
		strings.EqualFold(message, "context canceled")
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

func fileTransferEventInteger(metadata map[string]any, key string) int64 {
	value, ok := metadata[key]
	if !ok {
		return 0
	}
	switch number := value.(type) {
	case float64:
		return int64(number)
	case float32:
		return int64(number)
	case int:
		return int64(number)
	case int64:
		return number
	case uint64:
		if number <= uint64(^uint64(0)>>1) {
			return int64(number)
		}
	}
	return 0
}

func fileTransferEventTransferred(metadata map[string]any) int64 {
	for _, key := range []string{"sent", "received", "transferred"} {
		if _, ok := metadata[key]; ok {
			return max(0, fileTransferEventInteger(metadata, key))
		}
	}
	return 0
}

func fileTransferEventPercent(metadata map[string]any, transferred, total int64) float64 {
	percent := fileTransferEventNumber(metadata, "percent")
	if _, ok := metadata["percent"]; !ok && total > 0 {
		percent = float64(transferred) * 100 / float64(total)
	}
	return min(100, max(0, percent))
}

const attachedFileTransferProgressBarWidth = 30

func fileTransferTransferredText(timestamp time.Time, transferred, total int64, percent float64) string {
	filled := int(percent * attachedFileTransferProgressBarWidth / 100)
	filled = min(attachedFileTransferProgressBarWidth, max(0, filled))
	bar := strings.Repeat("#", filled)
	if filled < attachedFileTransferProgressBarWidth {
		bar += ">" + strings.Repeat(".", attachedFileTransferProgressBarWidth-filled-1)
	}
	return fmt.Sprintf("%sTransferred %d/%d bytes (%.1f%%) [%s]", fileTransferStatusPrefix(timestamp), transferred, total, percent, bar)
}

func fileTransferEventDigests(metadata map[string]any) (string, string, bool) {
	localDigest := fileTransferEventString(metadata, "local_sha256")
	remoteDigest := fileTransferEventString(metadata, "remote_sha256")
	if localDigest == "" || remoteDigest == "" {
		return "", "", false
	}
	return localDigest, remoteDigest, true
}

func fileTransferVerifyResult(localDigest, remoteDigest string) string {
	if strings.EqualFold(localDigest, remoteDigest) {
		return "MATCH"
	}
	return "MISMATCH"
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
