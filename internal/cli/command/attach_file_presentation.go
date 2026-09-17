package command

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/akira-init1/ChannelTerm/internal/core/session"
)

// fileTransferPresentation gates one attach client's local terminal rendering
// for file transfers and isolated terminal-command bootstraps. It deliberately
// owns no Session cursor or terminal bytes: the caller keeps advancing its
// cursor while this gate suppresses only local presentation.
type fileTransferPresentation struct {
	mu sync.Mutex

	// legacyActive covers old Hosts that do not include cursor metadata with
	// their lease events. Current Hosts use precise raw-output ranges instead.
	legacyActive bool
	openStart    *session.OutputCursor
	ranges       []fileTransferOutputRange

	// Agent commands use the same cursor-based suppression mechanism for their
	// echoed bootstrap only. Command output begins at a structured cursor and
	// remains ordinary raw terminal output.
	commandOpenStart *session.OutputCursor
	commandID        string
	commandActive    bool

	// The observer progress frame ends with a carriage return so later events
	// can overwrite it. Retain its last confirmed state to terminate that line
	// cleanly and to summarize legacy failure events that omit byte metadata.
	progressRendered bool
	// cancelConfirmationPending keeps an acknowledged in-flight block from
	// redrawing over the local [y/N] prompt. Progress state still advances so
	// cancellation reports the last confirmed byte count accurately.
	cancelConfirmationPending bool
	transferred               int64
	total                     int64
	percent                   float64
	speed                     float64
}

type fileTransferOutputRange struct {
	start session.OutputCursor
	end   session.OutputCursor
}

// fileTransferProgressStatusBoundary preserves the completed progress row,
// moves to the status row, and clears that row before writing text. The full
// erase prevents a shorter status from retaining a stale speed or ETA suffix.
const fileTransferProgressStatusBoundary = "\r\n\x1b[2K\r"

func newFileTransferPresentation() *fileTransferPresentation {
	return &fileTransferPresentation{}
}

// beginCancelConfirmation records that the prompt advanced past the in-place
// progress frame and suppresses later progress rendering until it is answered.
func (p *fileTransferPresentation) beginCancelConfirmation() {
	p.mu.Lock()
	p.progressRendered = false
	p.cancelConfirmationPending = true
	p.mu.Unlock()
}

// finishCancelConfirmation resumes normal progress rendering after a negative
// answer or a control error. A terminal transfer event also clears this state.
func (p *fileTransferPresentation) finishCancelConfirmation() {
	p.mu.Lock()
	p.cancelConfirmationPending = false
	p.mu.Unlock()
}

// handle applies a structured file-transfer event before optionally rendering
// a concise observer status. The transfer owner renders the same progress
// format locally, so local is used only to avoid duplicate text.
func (p *fileTransferPresentation) handle(event session.Event, local bool, write func([]byte) error) error {
	var text string
	p.mu.Lock()
	// Keep event-state transitions and their terminal write in one critical
	// section. Otherwise a progress event can prepare its frame, release the
	// lock, and then write after beginCancelConfirmation has displayed [y/N].
	// Serializing the write makes the prompt gate a true presentation boundary:
	// an already prepared frame finishes before the prompt, and later frames are
	// suppressed until the confirmation is resolved.
	defer p.mu.Unlock()
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
		if event.Metadata["type"] == "terminal" {
			p.closeTerminalCommandRange(event.Metadata)
			p.commandActive = false
			p.commandID = ""
		}
	case session.EventTerminalCommandStarted:
		if cursor, ok := fileTransferEventCursor(event.Metadata); ok {
			p.commandOpenStart = &cursor
		}
		p.commandID = fileTransferEventString(event.Metadata, "command_id")
		p.commandActive = true
		if !local {
			text = renderTerminalCommandEvent(event)
		}
	case session.EventTerminalCommandOutputStarted:
		if p.commandID == "" || p.commandID == fileTransferEventString(event.Metadata, "command_id") {
			p.closeTerminalCommandRange(event.Metadata)
		}
	case session.EventTerminalCommandCompleted, session.EventTerminalCommandFailed:
		p.addTerminalCommandHiddenRange(event.Metadata)
	case session.EventFileTransferStarted:
		if p.openStart == nil {
			p.legacyActive = true
		}
		p.progressRendered = false
		p.transferred = 0
		p.total = max(0, fileTransferEventInteger(event.Metadata, "total"))
		p.percent = 0
		p.speed = 0
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
		p.speed = max(0, fileTransferEventNumber(event.Metadata, "speed"))
		if !local && !p.cancelConfirmationPending {
			text = "\r" + formatFileTransferProgress(fileTransferSnapshot{
				transferred: p.transferred,
				total:       p.total,
				percent:     p.percent,
				speed:       p.speed,
			}) + "\x1b[K\r"
			p.progressRendered = true
		}
	case session.EventFileTransferCompleted:
		if p.openStart == nil {
			p.legacyActive = false
		}
		if !local {
			transferred := fileTransferEventTransferred(event.Metadata)
			if transferred == 0 {
				transferred = p.transferred
			}
			total := max(0, fileTransferEventInteger(event.Metadata, "total"))
			if total == 0 {
				total = p.total
			}
			speed := max(0, fileTransferEventNumber(event.Metadata, "speed"))
			if speed == 0 {
				speed = p.speed
			}
			_, eventHasTotal := event.Metadata["total"]
			if p.cancelConfirmationPending {
				// The prompt owns the current row. Finish it before reporting a
				// transfer that completed while the user was deciding.
				text = fileTransferProgressStatusBoundary
			} else if p.progressRendered {
				text = fileTransferProgressStatusBoundary
			} else if p.transferred > 0 || p.total > 0 || eventHasTotal {
				if total > 0 {
					transferred = total
				}
				text = "\r" + formatFileTransferProgress(fileTransferSnapshot{
					transferred: transferred,
					total:       total,
					percent:     100,
					speed:       speed,
				}) + "\x1b[K\r\n"
			} else {
				// A completion may arrive without retained progress. Clear the
				// current row before the status so stale terminal text cannot be
				// mistaken for fields appended to "completed".
				text = "\r\x1b[K"
			}
			text += fileTransferStatusPrefix(event.Timestamp) + "File transfer completed\r\n"
			if localDigest, remoteDigest, ok := fileTransferEventDigests(event.Metadata); ok {
				text += formatFileTransferVerificationSummary(localDigest, remoteDigest, fileTransferEventSavedPath(event.Metadata), "\r\n")
			}
		}
		p.progressRendered = false
		p.cancelConfirmationPending = false
	case session.EventFileTransferCancelled, session.EventFileTransferFailed:
		if p.openStart == nil {
			p.legacyActive = false
		}
		if !local {
			if p.progressRendered {
				text = fileTransferProgressStatusBoundary
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
			if event.Type == session.EventFileTransferCancelled || fileTransferEventCancelled(event.Metadata) {
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
		p.cancelConfirmationPending = false
	}
	if text == "" || write == nil {
		return nil
	}
	return write([]byte(text))
}

func (p *fileTransferPresentation) addTerminalCommandHiddenRange(metadata map[string]any) {
	start, startOK := terminalCommandEventCursor(metadata, "hidden_start")
	end, endOK := terminalCommandEventCursor(metadata, "hidden_end")
	if !startOK || !endOK || end < start {
		return
	}
	outputRange := fileTransferOutputRange{start: start, end: end}
	if len(p.ranges) == 0 || p.ranges[len(p.ranges)-1] != outputRange {
		p.ranges = append(p.ranges, outputRange)
	}
}

func terminalCommandEventCursor(metadata map[string]any, key string) (session.OutputCursor, bool) {
	return fileTransferEventCursor(map[string]any{"output_cursor": metadata[key]})
}

func (p *fileTransferPresentation) closeTerminalCommandRange(metadata map[string]any) {
	if p.commandOpenStart == nil {
		return
	}
	cursor, ok := fileTransferEventCursor(metadata)
	if ok && cursor >= *p.commandOpenStart {
		outputRange := fileTransferOutputRange{start: *p.commandOpenStart, end: cursor}
		if len(p.ranges) == 0 || p.ranges[len(p.ranges)-1] != outputRange {
			p.ranges = append(p.ranges, outputRange)
		}
	}
	p.commandOpenStart = nil
}

func (p *fileTransferPresentation) terminalCommandActive() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.commandActive
}

func renderTerminalCommandEvent(event session.Event) string {
	command := fileTransferEventString(event.Metadata, "command")
	if command == "" {
		return ""
	}
	return fmt.Sprintf("\r\n──────── AI ────────\r\n[%s] >> %s\r\n────────────────────\r\n", event.Timestamp.Format("15:04:05"), command)
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
	sourcePath := fileTransferEventString(metadata, "source_path")
	destinationPath := fileTransferEventString(metadata, "resolved_path")
	if destinationPath == "" {
		destinationPath = fileTransferEventString(metadata, "requested_path")
	}
	if sourcePath == "" || destinationPath == "" {
		return "in progress"
	}
	return sourcePath + " -> " + destinationPath
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

func fileTransferEventDigests(metadata map[string]any) (string, string, bool) {
	localDigest := fileTransferEventString(metadata, "local_sha256")
	remoteDigest := fileTransferEventString(metadata, "remote_sha256")
	if localDigest == "" || remoteDigest == "" {
		return "", "", false
	}
	return localDigest, remoteDigest, true
}

func fileTransferEventSavedPath(metadata map[string]any) string {
	return fileTransferEventString(metadata, "resolved_path")
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
		if !hidden && p.commandOpenStart != nil && cursor >= *p.commandOpenStart {
			hidden = true
		}
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
			if ownsTransfer && local && (event.Type == session.EventFileTransferCompleted || event.Type == session.EventFileTransferCancelled || event.Type == session.EventFileTransferFailed) {
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
