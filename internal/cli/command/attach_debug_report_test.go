package command

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/core/session"
)

func debugReportFixture(text string) session.Event {
	return session.Event{Type: session.EventDebugClientReport, Actor: "agent", Metadata: map[string]any{
		"bridge_id": "RSP-example", "sequence": uint64(1), "kind": "result", "text": text, "reported_by": "codex", "complete_audit": false,
	}}
}

func TestDebugReportPresentationBoundsEscapesAndHistory(t *testing.T) {
	event := debugReportFixture("sum = 7\n\x1b[2J" + strings.Repeat("X", 4090))
	got := renderDebugClientReport(event, true)
	for _, want := range []string{"Codex report; not a complete GDB audit", "[history]", "sum = 7", `\x1b[2J`, "preview truncated"} {
		if !strings.Contains(string(got), want) {
			t.Fatalf("missing %q: %s", want, got)
		}
	}
	if len(got) > 1500 || bytes.ContainsRune(got, '\x1b') {
		t.Fatal("unbounded or executable report text")
	}
	var out bytes.Buffer
	events := make([]session.Event, 25)
	for i := range events {
		events[i] = debugReportFixture("retained result")
	}
	if err := writeDebugReportHistory(events, func(b []byte) error { _, err := out.Write(b); return err }); err != nil {
		t.Fatal(err)
	}
	if strings.Count(out.String(), "[history]") != 20 || !strings.Contains(out.String(), "last 20") {
		t.Fatalf("unbounded retained replay: %s", out.String())
	}
}

func TestDebugReportsReachMultipleAttachmentsAndReplayHistory(t *testing.T) {
	host := newAttachTestHost(t)
	defer host.close()
	managed, _ := host.manager.Get("board")
	managed.PublishEvent(debugReportFixture("retained result: sum = 7"))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	outputs := []*lockedBuffer{{}, {}}
	done := make(chan error, len(outputs))
	for _, output := range outputs {
		input, inputWriter := io.Pipe()
		defer input.Close()
		defer inputWriter.Close()
		go func() {
			done <- runAttachSession(ctx, []string{"--endpoint", host.server.URL, "board"}, input, output, newMCPAttachSession)
		}()
	}
	waitForText := func(want string) {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		for _, output := range outputs {
			for !strings.Contains(output.String(), want) {
				if time.Now().After(deadline) {
					t.Fatalf("missing %q in attach output: %s", want, output.String())
				}
				time.Sleep(time.Millisecond)
			}
		}
	}
	waitForText("[history]")
	managed.PublishEvent(session.Event{Type: session.EventLeaseAcquired, Metadata: map[string]any{"type": "debug", "output_cursor": uint64(0)}})
	host.device.emit([]byte("$RAW-RSP-MUST-STAY-HIDDEN#00"))
	managed.PublishEvent(debugReportFixture("live result: stopped at main"))
	waitForText("live result: stopped at main")
	cancel()
	for range outputs {
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("report observer did not stop")
		}
	}
	for _, output := range outputs {
		got := output.String()
		if strings.Count(got, "retained result: sum = 7") != 1 || strings.Count(got, "live result: stopped at main") != 1 || strings.Contains(got, "$RAW-RSP") {
			t.Fatalf("duplicated/missing reports or exposed RSP: %s", got)
		}
	}
	if managed.State() != session.StateOpen {
		t.Fatal("stopping report observers closed the Session")
	}
}
