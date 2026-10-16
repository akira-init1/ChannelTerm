package command

import (
	"fmt"

	"github.com/akira-init1/ChannelTerm/internal/core/session"
)

// renderDebugClientReport renders explicitly supplied observations, never RSP
// packets. All caller text is bounded/escaped and labelled as an incomplete
// Codex report; reported states are not Host-verified lifecycle transitions.
func renderDebugClientReport(event session.Event, historical bool) []byte {
	if event.Type != session.EventDebugClientReport || event.Metadata["reported_by"] != "codex" {
		return nil
	}
	text := fileTransferEventString(event.Metadata, "text")
	preview, truncated := previewTerminalText(text, 1024, 12)
	bridge, _ := previewTerminalText(fileTransferEventString(event.Metadata, "bridge_id"), 96, 1)
	kind, _ := previewTerminalText(fileTransferEventString(event.Metadata, "kind"), 32, 1)
	history := ""
	if historical {
		history = " [history]"
	}
	if truncated {
		preview += "\r\n[preview truncated; retained text: terminal_session_events]"
	}
	return []byte(fmt.Sprintf("[%s] [Codex report; not a complete GDB audit]%s %s #%d %s\r\n%s\r\n",
		event.Timestamp.Local().Format("15:04:05"), history, bridge,
		fileTransferEventInteger(event.Metadata, "sequence"), kind, preview))
}

// writeDebugReportHistory replays up to twenty retained reports for a new attach
// client. Older reports remain available through the independent event cursor.
func writeDebugReportHistory(events []session.Event, write func([]byte) error) error {
	start, count := 0, 0
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Type == session.EventDebugClientReport {
			count++
			if count > 20 {
				start = i + 1
				break
			}
		}
	}
	if count > 20 {
		if err := write([]byte("[ChannelTerm] Showing the last 20 retained Codex reports; use terminal_session_events for earlier retained entries.\r\n")); err != nil {
			return err
		}
	}
	for _, event := range events[start:] {
		if text := renderDebugClientReport(event, true); len(text) > 0 {
			if err := write(text); err != nil {
				return err
			}
		}
	}
	return nil
}
