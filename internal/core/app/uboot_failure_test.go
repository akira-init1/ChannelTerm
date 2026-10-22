package app

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/core/session"
)

type bootFailureSession struct {
	*fakeConnectedSession
	drop    bool
	partial bool
}

func (s *bootFailureSession) ReadOutput(ctx context.Context, c session.OutputCursor, n int) (session.OutputChunk, error) {
	chunk, err := s.fakeConnectedSession.ReadOutput(ctx, c, n)
	if err == nil && s.drop {
		chunk.Dropped = true
	}
	return chunk, err
}
func (s *bootFailureSession) Write(r session.WriteRequest) (int, error) {
	if s.partial {
		_, _ = s.fakeConnectedSession.Write(session.WriteRequest{Actor: r.Actor, Data: r.Data[:1]})
		return 1, io.ErrClosedPipe
	}
	return s.fakeConnectedSession.Write(r)
}

func TestEnterUBootDroppedOutputAndPartialWrite(t *testing.T) {
	for _, drop := range []bool{false, true} {
		a, s := newSerialTestApplication(t)
		_, _ = a.manager.Remove(s.ID())
		wrapped := &bootFailureSession{fakeConnectedSession: s, drop: drop, partial: !drop}
		if err := a.manager.RegisterWithMetadata(wrapped, session.SessionMetadata{Transport: "serial"}); err != nil {
			t.Fatal(err)
		}
		r := bootTestRequest()
		r.Interrupt = []byte("ab")
		r.Capture = true
		done := make(chan UBootResult, 1)
		go func() { result, _ := a.EnterUBoot(context.Background(), s.ID(), r); done <- result }()
		waitBootReady(t, s)
		s.emitOutput([]byte("Hit any key"))
		result := <-done
		want := "write_failed"
		bytes := 1
		if drop {
			want = "output_lost"
			bytes = 0
		}
		if result.Status != want || result.BytesWritten != bytes || result.Dropped != drop || result.DiagnosticID == "" {
			t.Fatalf("result=%+v", result)
		}
		if _, active, _ := a.LeaseStatus(s.ID()); active {
			t.Fatal("lease retained")
		}
	}
}

func TestEnterUBootRebootRejectsPendingAndUnsupportedShell(t *testing.T) {
	a, s := newSerialTestApplication(t)
	_, _ = s.Write(session.WriteRequest{Actor: session.ActorUser, Data: []byte("unfinished")})
	r := bootTestRequest()
	r.Mode = "reboot"
	r.RebootCommand = "reboot"
	result, err := a.EnterUBoot(context.Background(), s.ID(), r)
	if !errors.Is(err, ErrTerminalCommandInputPending) || result.Status != "input_pending" || string(s.writtenData()) != "unfinished" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	a, s = newSerialTestApplication(t)
	s.onWrite = func(session.WriteRequest) {
		events, _ := s.ReadRecentEvents(1)
		token := strings.TrimPrefix(events.Events[0].Metadata["operation_id"].(string), "BOOT-")
		s.emitOutput([]byte("\x1e" + token + ":unsupported\x1f"))
	}
	result, err = a.EnterUBoot(context.Background(), s.ID(), r)
	if !errors.Is(err, ErrTerminalCommandRequiresBash) || result.Status != "unsupported_shell" || s.State() != session.StateOpen {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestEnterUBootLeaseLossStopsWithoutRecoveryWrites(t *testing.T) {
	a, s := newSerialTestApplication(t)
	a.leases.ttl = 20 * time.Millisecond
	r := bootTestRequest()
	done := make(chan UBootResult, 1)
	go func() { result, _ := a.EnterUBoot(context.Background(), s.ID(), r); done <- result }()
	waitBootReady(t, s)
	// Wait for observable expiry instead of sleeping across the timing boundary.
	deadline := time.After(time.Second)
	for {
		_, active, _ := a.LeaseStatus(s.ID())
		if !active {
			break
		}
		select {
		case <-deadline:
			t.Fatal("lease did not expire")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	s.emitOutput([]byte("Hit any key"))
	result := <-done
	if result.Status != "write_failed" || len(s.writtenData()) != 0 || s.State() != session.StateOpen {
		t.Fatalf("result=%+v writes=%q", result, s.writtenData())
	}
}
