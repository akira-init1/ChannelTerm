package command

import (
	"bytes"
	"testing"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/core/session"
)

func TestPresentationLineStateSeparatesStatusFromUnterminatedBoardPrompt(t *testing.T) {
	state := newPresentationLineState()
	prompt := []byte("root@board:~# ")
	state.observe(prompt)
	status := fileTransferStartedText(time.Date(2026, time.September, 14, 16, 8, 1, 0, time.Local), "send", "go.mod", "/tmp/go.mod")
	got := append(append([]byte(nil), prompt...), state.lineStartPrefix()...)
	got = append(got, status...)
	if bytes.Contains(got, []byte("root@board:~# [")) {
		t.Errorf("status stuck to board prompt: %q", got)
	}
	want := "root@board:~# \r\n[16:08:01] [ChannelTerm] File transfer started: go.mod -> /tmp/go.mod\r\n[ChannelTerm] Terminal input locked. Ctrl+C to cancel.\r\n"
	if string(got) != want {
		t.Errorf("presentation = %q, want %q", got, want)
	}
}

func TestPresentationLineStateDoesNotAddBlankLineAfterTerminatedOutput(t *testing.T) {
	for _, ending := range [][]byte{[]byte("hello\n"), []byte("hello\r\n"), []byte("hello\r")} {
		state := newPresentationLineState()
		state.observe(ending)
		if prefix := state.lineStartPrefix(); len(prefix) != 0 {
			t.Errorf("line ending %q prefix = %q, want none", ending, prefix)
		}
	}
}

func TestFileTransferEventStatusTimestampsTerminalEventsOnly(t *testing.T) {
	timestamp := time.Date(2026, time.September, 14, 16, 8, 12, 0, time.Local)
	presentation := newFileTransferPresentation()
	for _, tt := range []struct {
		name  string
		event session.Event
		want  string
	}{
		{name: "started", event: session.Event{Timestamp: timestamp, Type: session.EventFileTransferStarted, Metadata: map[string]any{"local_path": "go.mod", "remote_path": "/tmp/go.mod"}}, want: "[16:08:12] [ChannelTerm] File transfer started: go.mod -> /tmp/go.mod\r\n"},
		{name: "completed", event: session.Event{Timestamp: timestamp, Type: session.EventFileTransferCompleted}, want: "\r\x1b[K[16:08:12] [ChannelTerm] File transfer completed\r\n"},
		{name: "failed", event: session.Event{Timestamp: timestamp, Type: session.EventFileTransferFailed, Metadata: map[string]any{"error": "checksum mismatch"}}, want: "[16:08:12] [ChannelTerm] File transfer failed\r\n  Transferred: 0/0 bytes (0.0%)\r\n  Error      : checksum mismatch\r\n"},
		{name: "cancelled", event: session.Event{Timestamp: timestamp, Type: session.EventFileTransferCancelled, Metadata: map[string]any{"reason": "user_cancelled"}}, want: "[16:08:12] [ChannelTerm] File transfer cancelled\r\n  Transferred: 0/0 bytes (0.0%)\r\n  Reason     : cancelled by user\r\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var output bytes.Buffer
			if err := presentation.handle(tt.event, false, func(data []byte) error {
				_, err := output.Write(data)
				return err
			}); err != nil {
				t.Fatal(err)
			}
			if got := output.String(); got != tt.want {
				t.Errorf("status = %q, want %q", got, tt.want)
			}
		})
	}

	var output bytes.Buffer
	if err := presentation.handle(session.Event{Timestamp: timestamp, Type: session.EventFileTransferProgress, Metadata: map[string]any{"sent": 16384, "total": 65536, "percent": 25.0}}, false, func(data []byte) error {
		_, err := output.Write(data)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if got, want := output.String(), "\r[#####---------------] 25.0%  16 KiB / 64 KiB  0 B/s  ETA --\x1b[K\r"; got != want {
		t.Errorf("progress = %q, want %q", got, want)
	}
}

func TestPresentationLineStateTerminatesProgressBeforeCompletion(t *testing.T) {
	state := newPresentationLineState()
	progress := []byte("\r[##########----------] 50.0%\x1b[K")
	state.observe(progress)
	completion := fileTransferCompletedText(time.Date(2026, time.September, 14, 16, 8, 12, 0, time.Local))
	got := append(append([]byte(nil), progress...), state.lineStartPrefix()...)
	got = append(got, completion...)
	if bytes.Contains(got, []byte("\x1b[K[16:08:12]")) {
		t.Errorf("completion stuck to progress: %q", got)
	}
}

func TestPresentationLineStateSeparatesConsecutiveTransfersWithoutBlankLineAccumulation(t *testing.T) {
	state := newPresentationLineState()
	var output bytes.Buffer
	writeBoard := func(data string) {
		output.WriteString(data)
		state.observe([]byte(data))
	}
	writeStatus := func(data []byte) {
		if prefix := state.lineStartPrefix(); len(prefix) > 0 {
			output.Write(prefix)
			state.observe(prefix)
		}
		output.Write(data)
		state.observe(data)
	}

	writeBoard("root@board:~# ")
	writeStatus(fileTransferCompletedText(time.Date(2026, time.September, 14, 16, 8, 2, 0, time.Local)))
	writeBoard("root@board:~# ")
	writeStatus(fileTransferStartedText(time.Date(2026, time.September, 14, 16, 8, 5, 0, time.Local), "send", "next.bin", "/tmp/next.bin"))

	got := output.String()
	if bytes.Contains(output.Bytes(), []byte("root@board:~# [")) || bytes.Contains(output.Bytes(), []byte("\r\n\r\n[16:")) {
		t.Errorf("consecutive transfer presentation = %q, want one boundary per unterminated prompt", got)
	}
}
