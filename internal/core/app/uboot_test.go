package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/core/session"
)

func bootTestRequest() UBootRequest {
	return UBootRequest{Mode: "wait_reset", CountdownPattern: "Hit any key", PromptPattern: "Zynq> ", BootStartedPatterns: []string{"Starting kernel"}, Interrupt: []byte(" "), Timeout: 2 * time.Second}
}

func waitBootReady(t *testing.T, s *fakeConnectedSession) {
	t.Helper()
	deadline := time.After(3 * time.Second)
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		events, _ := s.ReadRecentEvents(100)
		for _, e := range events.Events {
			if e.Type == session.EventOperationState && e.Metadata["status"] == "ready" {
				return
			}
		}
		select {
		case <-deadline:
			t.Fatal("operation did not become ready")
		case <-ticker.C:
		}
	}
}

func TestEnterUBootNoiseSplitCountdownAndFreshPrompt(t *testing.T) {
	a, s := newSerialTestApplication(t)
	s.emitOutput([]byte("Hit any key\r\nZynq> ")) // Old evidence is not eligible.
	s.onWrite = func(r session.WriteRequest) {
		if string(r.Data) != " " {
			t.Errorf("unexpected write %q", r.Data)
		}
		s.emitOutput([]byte("\r\nZynq> "))
	}
	request := bootTestRequest()
	request.Capture = true
	type outcome struct {
		r   UBootResult
		err error
	}
	done := make(chan outcome, 1)
	go func() { r, e := a.EnterUBoot(context.Background(), "SER-1", request); done <- outcome{r, e} }()
	waitBootReady(t, s)
	lease, active, err := a.LeaseStatus("SER-1")
	if err != nil || !active {
		t.Fatalf("lease missing: %v", err)
	}
	events, _ := s.ReadRecentEvents(1)
	publicToken := strings.TrimPrefix(events.Events[0].Metadata["operation_id"].(string), "BOOT-")
	if strings.Contains(lease.Owner, publicToken) {
		t.Fatal("public operation ID reveals lease capability")
	}
	if len(s.writtenData()) != 0 {
		t.Fatal("old countdown caused a write")
	}
	for _, b := range []byte("\xff\xd3\r\nHit any key to stop autoboot: 2") {
		s.emitOutput([]byte{b})
	}
	out := <-done
	if out.err != nil || out.r.Status != "entered" || out.r.BytesWritten != 1 || out.r.DiagnosticID == "" || out.r.PromptCursor <= out.r.OutputStart {
		t.Fatalf("result=%+v err=%v", out.r, out.err)
	}
	if _, active, _ := a.LeaseStatus("SER-1"); active {
		t.Fatal("lease retained")
	}
	if s.State() != session.StateOpen {
		t.Fatal("shared Session closed")
	}
	completed, _ := s.ReadRecentEvents(1)
	if completed.Events[0].Metadata["diagnostic_id"] != out.r.DiagnosticID {
		t.Fatal("completion event lost archive ID")
	}
}

func TestEnterUBootConfiguredMarkersAndInterruptSequences(t *testing.T) {
	for _, tc := range []struct {
		name, marker, countdown, prompt, interrupt, newline string
	}{
		{"escape", "Autoboot in ", "Autoboot in 3 seconds", "=> ", "\x1b", "\r\n"},
		{"control_c", "Press Ctrl+C", "Press Ctrl+C to stop", "board# ", "\x03", "\n"},
		{"stop_string", "Autoboot [stop]", "Autoboot [stop] in 2 seconds", "custom> ", "stop", "\r"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, s := newSerialTestApplication(t)
			request := bootTestRequest()
			request.CountdownPattern = tc.marker
			request.PromptPattern = tc.prompt
			request.Interrupt = []byte(tc.interrupt)
			s.onWrite = func(r session.WriteRequest) {
				if string(r.Data) != tc.interrupt {
					t.Errorf("interrupt=%q, want exact bytes %q", r.Data, tc.interrupt)
				}
				s.emitOutput([]byte(tc.newline + tc.prompt))
			}
			type outcome struct {
				result UBootResult
				err    error
			}
			done := make(chan outcome, 1)
			go func() {
				r, err := a.EnterUBoot(context.Background(), "SER-1", request)
				done <- outcome{r, err}
			}()
			waitBootReady(t, s)
			s.emitOutput([]byte(tc.newline + tc.countdown))
			out := <-done
			if out.err != nil || out.result.Status != "entered" || out.result.BytesWritten != len(tc.interrupt) {
				t.Fatalf("result=%+v err=%v", out.result, out.err)
			}
			if got := string(s.writtenData()); got != tc.interrupt {
				t.Fatalf("writes=%q, want one exact interrupt %q without appended bytes", got, tc.interrupt)
			}
		})
	}
}

func TestEnterUBootRejectsBacklogAndFalsePrompt(t *testing.T) {
	for _, tc := range []struct {
		name, output, status string
		writes               int
	}{
		{"booted", "Hit any key\r\nStarting kernel\r\nZynq> ", "boot_started", 0},
		{"prompt_substring", "Hit any key\r\nlog: Zynq> ", "timeout", 1},
		{"no_prompt", "Hit any key\r\n", "timeout", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, s := newSerialTestApplication(t)
			request := bootTestRequest()
			request.Timeout = 150 * time.Millisecond
			done := make(chan UBootResult, 1)
			go func() { r, _ := a.EnterUBoot(context.Background(), "SER-1", request); done <- r }()
			waitBootReady(t, s)
			s.emitOutput([]byte(tc.output))
			r := <-done
			if r.Status != tc.status || len(s.writtenData()) != tc.writes {
				t.Fatalf("result=%+v writes=%q", r, s.writtenData())
			}
		})
	}
}

func TestEnterUBootCancellationCloseAndBusy(t *testing.T) {
	for _, closeSession := range []bool{false, true} {
		t.Run(fmt.Sprint(closeSession), func(t *testing.T) {
			a, s := newSerialTestApplication(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan UBootResult, 1)
			go func() { r, _ := a.EnterUBoot(ctx, "SER-1", bootTestRequest()); done <- r }()
			waitBootReady(t, s)
			if _, e := a.WriteSession(context.Background(), "SER-1", session.WriteRequest{Actor: session.ActorUser, Data: []byte("x")}); !errors.Is(e, ErrSessionBusy) {
				t.Fatalf("concurrent write: %v", e)
			}
			if closeSession {
				_ = s.Close()
			} else {
				cancel()
			}
			r := <-done
			want := "cancelled"
			if closeSession {
				want = "session_closed"
			}
			if r.Status != want || len(s.writtenData()) != 0 {
				t.Fatalf("result=%+v writes=%q", r, s.writtenData())
			}
			if _, active, _ := a.LeaseStatus("SER-1"); active {
				t.Fatal("lease retained")
			}
		})
	}
	a, s := newSerialTestApplication(t)
	if _, err := a.AcquireLease("SER-1", "other", LeaseTypeFileTransfer); err != nil {
		t.Fatal(err)
	}
	_, err := a.EnterUBoot(context.Background(), "SER-1", bootTestRequest())
	if !errors.Is(err, ErrSessionBusy) || len(s.writtenData()) != 0 {
		t.Fatalf("busy result: %v", err)
	}
	_ = a.ReleaseLease("SER-1", "other")
}

func TestEnterUBootRebootDoesNotWaitForShellEndMarker(t *testing.T) {
	a, s := newSerialTestApplication(t)
	s.onWrite = func(r session.WriteRequest) {
		if string(r.Data) == " " {
			s.emitOutput([]byte("\r\nZynq> "))
			return
		}
		events, _ := s.ReadRecentEvents(1)
		token := strings.TrimPrefix(events.Events[0].Metadata["operation_id"].(string), "BOOT-")
		if strings.Contains(string(r.Data), "stty") || !strings.Contains(string(r.Data), bashDeleteCurrentHistoryEntry) {
			t.Errorf("unsafe reboot bootstrap: %q", r.Data)
		}
		s.emitOutput(append(append([]byte{}, r.Data...), []byte("\x1e"+token+"\x1f\xd3\r\nHit any key")...))
	}
	request := bootTestRequest()
	request.Mode = "reboot"
	request.RebootCommand = "sync && reboot"
	r, err := a.EnterUBoot(context.Background(), "SER-1", request)
	if err != nil || r.Status != "entered" || s.State() != session.StateOpen {
		t.Fatalf("result=%+v err=%v", r, err)
	}
}

func TestEnterUBootValidatesBeforeWriting(t *testing.T) {
	for _, mutate := range []func(*UBootRequest){
		func(r *UBootRequest) { r.Mode = "" }, func(r *UBootRequest) { r.Interrupt = nil },
		func(r *UBootRequest) { r.CountdownPattern = "" }, func(r *UBootRequest) { r.BootStartedPatterns = nil },
		func(r *UBootRequest) { r.Timeout = 4 * time.Minute }, func(r *UBootRequest) { r.RebootCommand = "reboot" },
	} {
		a, s := newSerialTestApplication(t)
		r := bootTestRequest()
		mutate(&r)
		if _, err := a.EnterUBoot(context.Background(), "SER-1", r); !errors.Is(err, ErrInvalidUBootRequest) {
			t.Fatalf("error=%v", err)
		}
		if len(s.writtenData()) != 0 {
			t.Fatal("invalid request wrote bytes")
		}
	}
}

func TestBootMatcherAllChunkBoundaries(t *testing.T) {
	data := []byte("\xd3\r\nHit any key to stop\r\nZynq> ")
	for split := 1; split < len(data); split++ {
		m := bootMatcher{request: bootTestRequest()}
		m.feed(data[:split], 100)
		m.feed(data[split:], uint64(100+split))
		if !m.countdown || !m.prompt || m.booted || m.countdownCursor != 103 {
			t.Fatalf("split %d: %+v", split, m)
		}
	}
}
