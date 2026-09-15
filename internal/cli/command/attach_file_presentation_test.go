package command

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/core/session"
)

func TestFileTransferPresentationSuppressesRawTransferDataAndRestoresOutput(t *testing.T) {
	presentation := newFileTransferPresentation()
	var output bytes.Buffer
	write := func(data []byte) error {
		_, err := output.Write(data)
		return err
	}
	writeRaw := func(data []byte) {
		rendered, err := writeAttachedTerminalOutput(presentation, write, data)
		if err != nil {
			t.Fatalf("writeAttachedTerminalOutput() error = %v", err)
		}
		if !rendered {
			t.Logf("suppressed raw data of %d bytes", len(data))
		}
	}

	writeRaw([]byte("root@board:~# ls\r\n"))
	if err := presentation.handle(session.Event{Type: session.EventFileTransferStarted, Metadata: map[string]any{
		"direction": "send", "local_path": "system.dts", "requested_path": "/tmp/system.dts",
	}}, false, write); err != nil {
		t.Fatalf("handle(started) error = %v", err)
	}
	writeRaw([]byte("t='saved'; stty raw -echo; dd of=/tmp/system.dts\r\n"))
	writeRaw([]byte("@CTERM:token:READY:32768\r\n"))
	writeRaw([]byte{0x00, 0xff, 0x80, 0x01, 0x02})
	if err := presentation.handle(session.Event{Type: session.EventFileTransferProgress, Metadata: map[string]any{"sent": 16384, "total": 32768, "percent": 50.0}}, false, write); err != nil {
		t.Fatalf("handle(progress) error = %v", err)
	}
	if err := presentation.handle(session.Event{Type: session.EventFileTransferCompleted}, false, write); err != nil {
		t.Fatalf("handle(completed) error = %v", err)
	}
	writeRaw([]byte("root@board:~# \r\n"))

	got := output.String()
	for _, hidden := range []string{"stty raw", "dd of=", "@CTERM", string([]byte{0x00, 0xff, 0x80, 0x01, 0x02})} {
		if bytes.Contains(output.Bytes(), []byte(hidden)) {
			t.Errorf("output unexpectedly contains suppressed transfer data %q: %q", hidden, got)
		}
	}
	for _, wanted := range []string{
		"root@board:~# ls", "File transfer started: system.dts -> /tmp/system.dts", "[##########----------] 50.0%  16 KiB / 32 KiB  0 B/s  ETA --", "File transfer completed", "root@board:~# ",
	} {
		if !bytes.Contains(output.Bytes(), []byte(wanted)) {
			t.Errorf("output = %q, want %q", got, wanted)
		}
	}
}

func TestFileTransferPresentationRendersCompletedChecksumSummary(t *testing.T) {
	presentation := newFileTransferPresentation()
	var output bytes.Buffer
	write := func(data []byte) error {
		_, err := output.Write(data)
		return err
	}
	progressTime := time.Date(2026, time.September, 15, 9, 24, 57, 0, time.Local)
	completedTime := progressTime.Add(time.Second)
	digest := strings.Repeat("7d", 32)
	if err := presentation.handle(session.Event{Timestamp: progressTime, Type: session.EventFileTransferProgress, Metadata: map[string]any{
		"sent": 65536, "total": 65536, "percent": 100.0,
	}}, false, write); err != nil {
		t.Fatal(err)
	}
	if err := presentation.handle(session.Event{Timestamp: completedTime, Type: session.EventFileTransferCompleted, Metadata: map[string]any{
		"direction": "send", "resolved_path": "/tmp/firmware.bin", "local_sha256": digest, "remote_sha256": digest,
	}}, false, write); err != nil {
		t.Fatal(err)
	}
	want := "\r[####################] 100.0%  64 KiB / 64 KiB  0 B/s  ETA 0s\x1b[K\r" +
		fileTransferProgressStatusBoundary + "[09:24:58] [ChannelTerm] File transfer completed\r\n" +
		"  Local SHA-256 : " + digest + "\r\n" +
		"  Remote SHA-256: " + digest + "\r\n" +
		"  Verify        : MATCH\r\n" +
		"  Saved         : /tmp/firmware.bin\r\n"
	if got := output.String(); got != want {
		t.Errorf("completed presentation = %q, want %q", got, want)
	}
}

func TestFileTransferEventSavedPathUsesTransferDirection(t *testing.T) {
	for _, tt := range []struct {
		name     string
		metadata map[string]any
		want     string
	}{
		{name: "send", metadata: map[string]any{"direction": "send", "local_path": "firmware.bin", "resolved_path": "/tmp/firmware.bin"}, want: "/tmp/firmware.bin"},
		{name: "receive", metadata: map[string]any{"direction": "receive", "local_path": "firmware.bin", "remote_path": "/tmp/firmware.bin"}, want: "firmware.bin"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := fileTransferEventSavedPath(tt.metadata); got != tt.want {
				t.Errorf("saved path = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFileTransferPresentationCompletionReplacesStaleFrameBeforeStatus(t *testing.T) {
	presentation := newFileTransferPresentation()
	var output bytes.Buffer
	write := func(data []byte) error {
		_, err := output.Write(data)
		return err
	}
	if err := presentation.handle(session.Event{Type: session.EventFileTransferProgress, Metadata: map[string]any{
		"sent": int64(56 * 1024), "total": int64(64 * 1024), "percent": 87.5, "speed": float64(8 * 1024),
	}}, false, write); err != nil {
		t.Fatal(err)
	}
	// A declined prompt ends the old progress row, but the transfer can finish
	// before another ordinary progress event redraws it.
	presentation.beginCancelConfirmation()
	presentation.finishCancelConfirmation()
	completedAt := time.Date(2026, time.September, 15, 9, 24, 58, 0, time.Local)
	if err := presentation.handle(session.Event{Timestamp: completedAt, Type: session.EventFileTransferCompleted, Metadata: map[string]any{
		"sent": int64(64 * 1024), "total": int64(64 * 1024), "percent": 100.0,
	}}, false, write); err != nil {
		t.Fatal(err)
	}
	got := output.String()
	if !strings.Contains(got, "\r[####################] 100.0%  64 KiB / 64 KiB  8 KiB/s  ETA 0s\x1b[K\r\n[09:24:58] [ChannelTerm] File transfer completed\r\n") {
		t.Errorf("completed presentation = %q, want a complete replacement frame before status", got)
	}
	if strings.Contains(got, "File transfer completed8 KiB/s") {
		t.Errorf("completed presentation = %q, contains stale progress fields after status", got)
	}
}

func TestFileTransferPresentationRefreshesOneLineAndResumesAfterDeclinedCancellation(t *testing.T) {
	presentation := newFileTransferPresentation()
	var output bytes.Buffer
	write := func(data []byte) error {
		_, err := output.Write(data)
		return err
	}
	progress := func(sent int64, percent, speed float64) {
		t.Helper()
		if err := presentation.handle(session.Event{Type: session.EventFileTransferProgress, Metadata: map[string]any{
			"sent": sent, "total": int64(100 * 1024), "percent": percent, "speed": speed,
		}}, false, write); err != nil {
			t.Fatal(err)
		}
	}

	progress(10*1024, 10, 1024)
	progress(20*1024, 20, 2*1024)
	if got := output.String(); strings.Contains(got, "\n") || strings.Count(got, "\r") != 4 {
		t.Fatalf("dynamic updates = %q, want two carriage-return-only frames", got)
	}

	presentation.beginCancelConfirmation()
	beforePrompt := output.String()
	progress(30*1024, 30, 3*1024)
	if got := output.String(); got != beforePrompt {
		t.Fatalf("progress during confirmation = %q, want no redraw", got)
	}

	presentation.finishCancelConfirmation()
	progress(40*1024, 40, 4*1024)
	if got := output.String(); strings.Contains(got, "\n") || strings.Count(got, "\r") != 6 || !strings.Contains(got, "[########------------] 40.0%") {
		t.Errorf("progress after declined cancellation = %q, want resumed one-line refresh", got)
	}
}

func TestFileTransferPresentationTerminatesProgressBeforeEveryTerminalStatus(t *testing.T) {
	for _, tt := range []struct {
		name  string
		event session.Event
		want  string
	}{
		{name: "normal completion", event: session.Event{Type: session.EventFileTransferCompleted}, want: "File transfer completed"},
		{name: "confirmed cancellation", event: session.Event{Type: session.EventFileTransferCancelled, Metadata: map[string]any{"reason": "user_cancelled"}}, want: "File transfer cancelled"},
		{name: "context cancellation", event: session.Event{Type: session.EventFileTransferFailed, Metadata: map[string]any{"error": "context canceled"}}, want: "File transfer cancelled"},
		{name: "other failure", event: session.Event{Type: session.EventFileTransferFailed, Metadata: map[string]any{"error": "serial connection lost"}}, want: "File transfer failed"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			presentation := newFileTransferPresentation()
			var output bytes.Buffer
			write := func(data []byte) error {
				_, err := output.Write(data)
				return err
			}
			if err := presentation.handle(session.Event{Type: session.EventFileTransferProgress, Metadata: map[string]any{
				"sent": int64(32 * 1024), "total": int64(64 * 1024), "percent": 50.0, "speed": float64(8 * 1024),
			}}, false, write); err != nil {
				t.Fatal(err)
			}
			if err := presentation.handle(tt.event, false, write); err != nil {
				t.Fatal(err)
			}
			got := output.String()
			if !strings.Contains(got, "\x1b[K\r\r\n") || !strings.Contains(got, tt.want) {
				t.Errorf("terminal transition = %q, want one completed progress line before %q", got, tt.want)
			}
		})
	}
}

func TestFileTransferPresentationDoesNotRedrawOverCancelConfirmation(t *testing.T) {
	presentation := newFileTransferPresentation()
	var output bytes.Buffer
	write := func(data []byte) error {
		_, err := output.Write(data)
		return err
	}
	first := time.Date(2026, time.September, 15, 7, 17, 45, 0, time.Local)
	if err := presentation.handle(session.Event{Timestamp: first, Type: session.EventFileTransferProgress, Metadata: map[string]any{
		"sent": int64(16384), "total": int64(524288), "percent": 3.125,
	}}, false, write); err != nil {
		t.Fatal(err)
	}
	presentation.beginCancelConfirmation()
	beforePendingProgress := output.String()
	if err := presentation.handle(session.Event{Timestamp: first.Add(time.Second), Type: session.EventFileTransferProgress, Metadata: map[string]any{
		"sent": int64(24576), "total": int64(524288), "percent": 4.6875,
	}}, false, write); err != nil {
		t.Fatal(err)
	}
	if got := output.String(); got != beforePendingProgress {
		t.Fatalf("output while confirmation pending = %q, want no progress redraw", got)
	}
	if err := presentation.handle(session.Event{Timestamp: first.Add(2 * time.Second), Type: session.EventFileTransferCancelled, Metadata: map[string]any{
		"transferred": int64(24576), "total": int64(524288), "percent": 4.6875, "reason": "user_cancelled",
	}}, false, write); err != nil {
		t.Fatal(err)
	}
	got := output.String()
	if strings.Count(got, "Transferred 24576/524288") != 0 {
		t.Fatalf("output = %q, pending progress frame must remain hidden", got)
	}
	if !strings.Contains(got, "File transfer cancelled\r\n  Transferred: 24576/524288 bytes (4.7%)") {
		t.Fatalf("output = %q, want cancellation summary with latest confirmed progress", got)
	}
}

func TestFileTransferPresentationRendersCancellationAndFailureSummaries(t *testing.T) {
	timestamp := time.Date(2026, time.September, 15, 9, 24, 58, 0, time.Local)
	for _, tt := range []struct {
		name     string
		metadata map[string]any
		want     string
	}{
		{
			name:     "cancelled",
			metadata: map[string]any{"sent": 24576, "total": 65536, "percent": 37.5, "error": "user_cancelled", "reason": "user_cancelled"},
			want: "[09:24:58] [ChannelTerm] File transfer cancelled\r\n" +
				"  Transferred: 24576/65536 bytes (37.5%)\r\n" +
				"  Reason     : cancelled by user\r\n",
		},
		{
			name:     "failed",
			metadata: map[string]any{"received": 24576, "total": 65536, "percent": 37.5, "error": "serial connection lost"},
			want: "[09:24:58] [ChannelTerm] File transfer failed\r\n" +
				"  Transferred: 24576/65536 bytes (37.5%)\r\n" +
				"  Error      : serial connection lost\r\n",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			presentation := newFileTransferPresentation()
			var output bytes.Buffer
			if err := presentation.handle(session.Event{Timestamp: timestamp, Type: session.EventFileTransferFailed, Metadata: tt.metadata}, false, func(data []byte) error {
				_, err := output.Write(data)
				return err
			}); err != nil {
				t.Fatal(err)
			}
			if got := output.String(); got != tt.want {
				t.Errorf("terminal presentation = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFileTransferPresentationFailedTransferRestoresOutputWithoutReplay(t *testing.T) {
	presentation := newFileTransferPresentation()
	var output bytes.Buffer
	write := func(data []byte) error {
		_, err := output.Write(data)
		return err
	}
	if err := presentation.handle(session.Event{Type: session.EventFileTransferStarted}, false, write); err != nil {
		t.Fatal(err)
	}
	if rendered, err := writeAttachedTerminalOutput(presentation, write, []byte("@CTERM:token:DATA\x00\xff")); err != nil || rendered {
		t.Fatalf("transfer raw = rendered=%t, err=%v; want suppressed", rendered, err)
	}
	if err := presentation.handle(session.Event{Type: session.EventFileTransferFailed, Metadata: map[string]any{"error": "context canceled"}}, false, write); err != nil {
		t.Fatal(err)
	}
	if rendered, err := writeAttachedTerminalOutput(presentation, write, []byte("root@board:~# ")); err != nil || !rendered {
		t.Fatalf("normal output after failure = rendered=%t, err=%v; want rendered", rendered, err)
	}
	if got := output.String(); bytes.Contains(output.Bytes(), []byte("@CTERM")) || !bytes.Contains(output.Bytes(), []byte("File transfer cancelled")) || !bytes.Contains(output.Bytes(), []byte("root@board:~# ")) {
		t.Errorf("output = %q, want cancelled status and restored prompt without replay", got)
	}
}

func TestFileTransferPresentationUsesIndependentGatesForMultipleObservers(t *testing.T) {
	observers := []*fileTransferPresentation{newFileTransferPresentation(), newFileTransferPresentation()}
	for index, presentation := range observers {
		var output bytes.Buffer
		write := func(data []byte) error {
			_, err := output.Write(data)
			return err
		}
		if err := presentation.handle(session.Event{Type: session.EventFileTransferStarted}, false, write); err != nil {
			t.Fatalf("observer %d handle(started) error = %v", index, err)
		}
		if rendered, err := writeAttachedTerminalOutput(presentation, write, []byte("@CTERM:token:ACK\x00")); err != nil || rendered {
			t.Fatalf("observer %d transfer raw = rendered=%t, err=%v; want suppressed", index, rendered, err)
		}
		if err := presentation.handle(session.Event{Type: session.EventFileTransferCompleted}, false, write); err != nil {
			t.Fatalf("observer %d handle(completed) error = %v", index, err)
		}
		if rendered, err := writeAttachedTerminalOutput(presentation, write, []byte("ready\r\n")); err != nil || !rendered {
			t.Fatalf("observer %d normal raw = rendered=%t, err=%v; want rendered", index, rendered, err)
		}
		if got := output.String(); bytes.Contains(output.Bytes(), []byte("@CTERM")) || !bytes.Contains(output.Bytes(), []byte("ready")) {
			t.Errorf("observer %d output = %q, want restored clean output", index, got)
		}
	}
}

func TestFileTransferPresentationDoesNotDuplicateOwnerProgress(t *testing.T) {
	presentation := newFileTransferPresentation()
	var output bytes.Buffer
	write := func(data []byte) error {
		_, err := output.Write(data)
		return err
	}
	for _, event := range []session.Event{
		{Type: session.EventFileTransferStarted},
		{Type: session.EventFileTransferProgress, Metadata: map[string]any{"percent": 50.0}},
		{Type: session.EventFileTransferCompleted},
	} {
		if err := presentation.handle(event, true, write); err != nil {
			t.Fatal(err)
		}
	}
	if got := output.String(); got != "" {
		t.Errorf("owner event output = %q, want no duplicate local transfer presentation", got)
	}
}

func TestFileTransferPresentationSuppressesLeaseRangeWithoutFilteringUserContent(t *testing.T) {
	presentation := newFileTransferPresentation()
	var output bytes.Buffer
	write := func(data []byte) error {
		_, err := output.Write(data)
		return err
	}
	if err := presentation.handle(session.Event{Type: session.EventLeaseAcquired, Metadata: map[string]any{
		"type": "file-transfer", "output_cursor": uint64(6),
	}}, false, write); err != nil {
		t.Fatal(err)
	}
	if err := presentation.handle(session.Event{Type: session.EventFileTransferStarted, Metadata: map[string]any{
		"local_path": "local.bin", "requested_path": "/tmp/remote.bin",
	}}, false, write); err != nil {
		t.Fatal(err)
	}
	if rendered, err := writeAttachedTerminalOutputAt(presentation, write, 20, []byte("before@CTERM:payload")); err != nil || !rendered {
		t.Fatalf("mixed range = rendered=%t, err=%v; want visible bytes before internal range", rendered, err)
	}
	if err := presentation.handle(session.Event{Type: session.EventFileTransferCompleted}, false, write); err != nil {
		t.Fatal(err)
	}
	if err := presentation.handle(session.Event{Type: session.EventLeaseReleased, Metadata: map[string]any{
		"type": "file-transfer", "output_cursor": uint64(20),
	}}, false, write); err != nil {
		t.Fatal(err)
	}
	// This marker-shaped board output is outside the internal cursor range and
	// must remain visible; presentation never filters raw content by text.
	if rendered, err := writeAttachedTerminalOutputAt(presentation, write, 34, []byte("@CTERM:board\r\n")); err != nil || !rendered {
		t.Fatalf("board output = rendered=%t, err=%v; want rendered", rendered, err)
	}
	if got := output.String(); strings.Contains(got, "payload") || !strings.Contains(got, "before") || !strings.Contains(got, "@CTERM:board") {
		t.Errorf("output = %q, want visible bytes outside the internal range", got)
	}
}

func TestFileTransferPresentationRestoresAIActivityAfterEveryTerminalState(t *testing.T) {
	states := []struct {
		name  string
		event session.Event
	}{
		{name: "completed", event: session.Event{Type: session.EventFileTransferCompleted}},
		{name: "failed", event: session.Event{Type: session.EventFileTransferFailed, Metadata: map[string]any{"error": "remote error"}}},
		{name: "cancelled", event: session.Event{Type: session.EventFileTransferFailed, Metadata: map[string]any{"error": "context canceled"}}},
	}
	for _, tt := range states {
		t.Run(tt.name, func(t *testing.T) {
			presentation := newFileTransferPresentation()
			var output bytes.Buffer
			write := func(data []byte) error {
				_, err := output.Write(data)
				return err
			}
			before := renderAgentActivity(session.SessionEvent{Timestamp: time.Date(2026, time.August, 23, 15, 46, 0, 0, time.Local), Actor: session.ActorAgent, Operation: session.OperationWrite, Data: []byte("uname -a")})
			if err := write(before); err != nil {
				t.Fatal(err)
			}
			if err := presentation.handle(session.Event{Type: session.EventLeaseAcquired, Metadata: map[string]any{"type": "file-transfer", "output_cursor": uint64(10)}}, false, write); err != nil {
				t.Fatal(err)
			}
			if rendered, err := writeAttachedTerminalOutputAt(presentation, write, 20, []byte("stty raw\x00\xff")); err != nil || rendered {
				t.Fatalf("internal raw = rendered=%t, err=%v; want suppressed", rendered, err)
			}
			if err := presentation.handle(tt.event, false, write); err != nil {
				t.Fatal(err)
			}
			if err := presentation.handle(session.Event{Type: session.EventLeaseReleased, Metadata: map[string]any{"type": "file-transfer", "output_cursor": uint64(20)}}, false, write); err != nil {
				t.Fatal(err)
			}
			after := renderAgentActivity(session.SessionEvent{Timestamp: time.Date(2026, time.August, 23, 15, 47, 0, 0, time.Local), Actor: session.ActorAgent, Operation: session.OperationWrite, Data: []byte("uname -a")})
			if err := write(after); err != nil {
				t.Fatal(err)
			}
			if rendered, err := writeAttachedTerminalOutputAt(presentation, write, 33, []byte("Linux board\r\n")); err != nil || !rendered {
				t.Fatalf("post-transfer board output = rendered=%t, err=%v; want rendered", rendered, err)
			}
			got := output.String()
			if strings.Count(got, "──────── AI ────────") != 2 || strings.Contains(got, "stty raw") || !strings.Contains(got, "Linux board") {
				t.Errorf("output = %q, want AI blocks before and after with restored board output", got)
			}
		})
	}
}
