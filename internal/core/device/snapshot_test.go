package device

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestSnapshotPreservesFailedEvidenceAndRecovers(t *testing.T) {
	now := time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC)
	fail := false
	endpoint := "COM8"
	registry, err := newRegistry(ScannerFunc(func(context.Context) ([]Endpoint, error) {
		if fail {
			return nil, errors.New("enumeration unavailable")
		}
		return []Endpoint{{Transport: "serial", Endpoint: endpoint}}, nil
	}), time.Second, 8, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	if got := registry.Snapshot(); got.State != ScanUnknown || !got.LastSuccessfulScan.IsZero() || len(got.Devices) != 0 {
		t.Fatalf("uninitialized = %#v", got)
	}
	if err := registry.scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	initial := registry.Snapshot()
	if initial.State != ScanCurrent || len(initial.Devices) != 1 || !initial.LastSuccessfulScan.Equal(now) {
		t.Fatalf("initial = %#v", initial)
	}
	initial.Devices[0].Endpoint = "caller mutation"
	if registry.List()[0].Endpoint != "COM8" {
		t.Fatal("snapshot shares storage")
	}
	now = now.Add(3 * time.Second)
	if registry.Snapshot().State != ScanCurrent {
		t.Fatal("boundary prematurely stale")
	}
	now = now.Add(time.Nanosecond)
	if registry.Snapshot().State != ScanStale {
		t.Fatal("stalled scan looks current")
	}
	fail = true
	if err := registry.scan(context.Background()); err == nil {
		t.Fatal("wanted scan failure")
	}
	failed := registry.Snapshot()
	if failed.State != ScanFailed || failed.Devices[0].Endpoint != "COM8" || !failed.LastSuccessfulScan.Equal(initial.LastSuccessfulScan) {
		t.Fatalf("failed = %#v", failed)
	}
	if chunk, err := registry.ReadRecent(8); err != nil || len(chunk.Events) != 0 {
		t.Fatalf("failed scan emitted events: %#v, %v", chunk, err)
	}
	fail, endpoint = false, "COM11"
	if err := registry.scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := registry.Snapshot(); got.State != ScanCurrent || len(got.Devices) != 1 || got.Devices[0].Endpoint != "COM11" || !got.LastSuccessfulScan.Equal(now) {
		t.Fatalf("recovered = %#v", got)
	}
	registry.Close()
	registry.Close()
	if registry.Snapshot().State != ScanStale {
		t.Fatal("closed scanner looks current")
	}
}

func TestSnapshotFirstFailureHasNoSuccessfulTimestamp(t *testing.T) {
	registry := newTestRegistry(t, ScannerFunc(func(context.Context) ([]Endpoint, error) { return nil, errors.New("unavailable") }))
	if err := registry.scan(context.Background()); err == nil {
		t.Fatal("wanted scan failure")
	}
	if got := registry.Snapshot(); got.State != ScanFailed || !got.LastSuccessfulScan.IsZero() || len(got.Devices) != 0 {
		t.Fatalf("failed initial = %#v", got)
	}
}
