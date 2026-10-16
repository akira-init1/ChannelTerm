package app

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/core/debugger"
	"github.com/akira-init1/ChannelTerm/internal/core/session"
)

func TestRSPBridgeReportsDoNotControlRelay(t *testing.T) {
	a, s := bridgeFixture(t)
	start, err := a.StartRSPBridge(context.Background(), bridgeRequest())
	if err != nil {
		t.Fatal(err)
	}
	conn, err := debugger.DialBridge(context.Background(), *start.Connection)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	before := waitBridgeState(t, a, start.Status.BridgeID, "connected")
	q := BridgeReportRequest{BridgeID: before.BridgeID, Owner: bridgeTestOwner, Sequence: 1, Kind: "command", Text: "break main"}
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := a.ReportRSPBridge(context.Background(), q); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	for _, change := range []func(*BridgeReportRequest){
		func(q *BridgeReportRequest) { q.Owner = "wrong" },
		func(q *BridgeReportRequest) { q.Sequence = 0 },
		func(q *BridgeReportRequest) { q.Sequence = 1 << 53 },
		func(q *BridgeReportRequest) { q.Text = "different retry" },
		func(q *BridgeReportRequest) { q.Sequence = 2; q.Text = strings.Repeat("x", 4097) },
		func(q *BridgeReportRequest) { q.Sequence = 2; q.Text = "\xff" },
		func(q *BridgeReportRequest) { q.Sequence = 2; q.Text = " \n " },
		func(q *BridgeReportRequest) { q.Sequence = 2; q.Kind = "rsp" },
		func(q *BridgeReportRequest) { q.Sequence = 2; q.Text = "token=" + start.Connection.Token },
		func(q *BridgeReportRequest) { q.Sequence = 2; q.Text = "owner=" + bridgeTestOwner },
		func(q *BridgeReportRequest) { q.Sequence = 2; q.Text = a.bridges.runs[q.BridgeID].owner },
	} {
		bad := q
		change(&bad)
		if _, err := a.ReportRSPBridge(context.Background(), bad); err == nil {
			t.Fatal("invalid report accepted")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := a.ReportRSPBridge(ctx, q); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled report: %v", err)
	}
	q.Sequence, q.Kind, q.Text = 3, "state", "shell_verified (caller claim only)"
	if _, err := a.ReportRSPBridge(context.Background(), q); err != nil {
		t.Fatal(err)
	}
	q.Sequence = 1
	if _, err := a.ReportRSPBridge(context.Background(), q); err == nil {
		t.Fatal("old report accepted")
	}
	after, _ := a.RSPBridgeStatus(before.BridgeID, bridgeTestOwner)
	if before != after || len(s.writtenData()) != 0 {
		t.Fatalf("report changed bridge/TTY: before=%+v after=%+v", before, after)
	}
	output, _ := s.ReadRecent(4096)
	if len(output.Data) != 0 {
		t.Fatal("report entered shell output")
	}
	for range 2 { // Independent readers see the same retained reports.
		chunk, _ := s.ReadEvents(context.Background(), 0, 100)
		var reports []session.Event
		for _, event := range chunk.Events {
			if event.Type == session.EventDebugClientReport {
				reports = append(reports, event)
			}
		}
		if len(reports) != 2 || reports[0].Metadata["text"] != "break main" || reports[1].Metadata["sequence"] != uint64(3) || reports[0].Metadata["complete_audit"] != false || reports[0].Metadata["reported_by"] != "codex" {
			t.Fatalf("unexpected report history: %+v", reports)
		}
	}
	// Reporting errors and absence of observers leave both relay directions live.
	payload := []byte{0, 0xff, '$', '+', 3}
	conn.SetDeadline(time.Now().Add(time.Second))
	s.emitOutput(payload)
	got := make([]byte, len(payload))
	if _, err := io.ReadFull(conn, got); err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("report failure affected target relay: %x %v", got, err)
	}
	if _, err := conn.Write(payload); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for !bytes.Equal(s.writtenData(), payload) {
		if time.Now().After(deadline) {
			t.Fatal("report failure affected client relay")
		}
		time.Sleep(time.Millisecond)
	}
	if _, err := a.CloseRSPBridge(before.BridgeID, bridgeTestOwner); err != nil {
		t.Fatal(err)
	}
	q.Sequence = 4
	if _, err := a.ReportRSPBridge(context.Background(), q); err == nil {
		t.Fatal("new report on a closed bridge accepted")
	}
}

func TestRSPBridgeReportDoesNotRenewExpiredHeartbeat(t *testing.T) {
	a, _ := bridgeFixture(t)
	start, err := a.StartRSPBridge(context.Background(), bridgeRequest())
	if err != nil {
		t.Fatal(err)
	}
	r := a.bridges.runs[start.Status.BridgeID]
	r.mu.Lock()
	r.status.ExpiresAt = time.Now().Add(-time.Second)
	r.mu.Unlock()
	if _, err := a.ReportRSPBridge(context.Background(), BridgeReportRequest{BridgeID: r.id, Owner: bridgeTestOwner, Sequence: 1, Kind: "result", Text: "sum = 7"}); err == nil {
		t.Fatal("expired heartbeat accepted a new report")
	}
	if time.Now().Before(r.snapshot().ExpiresAt) {
		t.Fatal("report extended heartbeat")
	}
}
