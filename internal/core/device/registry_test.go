package device

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"
)

func TestRegistryBaselinesInitialDevicesWithoutAppearedEvents(t *testing.T) {
	scanner := &fakeScanner{results: [][]Endpoint{{{Transport: "serial", Endpoint: "COM6"}, {Transport: "serial", Endpoint: "COM8"}}}}
	registry := newTestRegistry(t, scanner)
	if err := registry.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(registry.Close)

	got := registry.List()
	if len(got) != 2 || got[0].Endpoint != "COM6" || got[1].Endpoint != "COM8" {
		t.Fatalf("List() = %#v, want current COM6 and COM8", got)
	}
	if got[0].State != StatePresent || got[0].FirstSeen.IsZero() || got[0].LastSeen.IsZero() {
		t.Errorf("initial device = %#v, want populated present record", got[0])
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := registry.Read(ctx, 0, 1); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Read(initial cursor) error = %v, want deadline exceeded without startup appeared event", err)
	}
}

func TestRegistryListsBestEffortSerialMetadataWithoutChangingPresence(t *testing.T) {
	scanner := &fakeScanner{results: [][]Endpoint{{
		{
			Transport: "serial",
			Endpoint:  "COM6",
			Metadata: SerialMetadata{
				VID:          "0403",
				PID:          "6010",
				USBSerial:    "ABC123",
				Manufacturer: "FTDI",
				Product:      "FT232H",
				USBPath:      "PCIROOT(0)#USBROOT(0)#USB(2)",
			},
		},
		{Transport: "serial", Endpoint: "COM8", Metadata: SerialMetadata{VID: "1a86", PID: "7523"}},
		{Transport: "serial", Endpoint: "/dev/ttyS0"},
	}}}
	registry := newTestRegistry(t, scanner)
	if err := registry.scan(context.Background()); err != nil {
		t.Fatalf("initial scan error = %v", err)
	}

	devices := registry.List()
	if len(devices) != 3 {
		t.Fatalf("List() = %#v, want three present endpoints", devices)
	}
	if got := devices[0]; got.Endpoint != "/dev/ttyS0" || got.State != StatePresent || got.Metadata != (SerialMetadata{}) {
		t.Errorf("non-USB device = %#v, want present with empty metadata", got)
	}
	if got := devices[1]; got.Endpoint != "COM6" || got.Metadata.USBSerial != "ABC123" || got.Metadata.USBPath == "" {
		t.Errorf("full metadata device = %#v, want complete COM6 metadata", got)
	}
	if got := devices[2]; got.Endpoint != "COM8" || got.State != StatePresent || got.Metadata.USBSerial != "" || got.Metadata.VID != "1a86" {
		t.Errorf("missing-serial device = %#v, want present COM8 with retained VID/PID", got)
	}
}

func TestRegistryEmitsTransitionsOnceAndRetainsInitialFirstSeen(t *testing.T) {
	now := time.Date(2026, time.August, 23, 10, 0, 0, 0, time.UTC)
	scanner := &fakeScanner{results: [][]Endpoint{
		{{Transport: "serial", Endpoint: "COM8"}},
		{{Transport: "serial", Endpoint: "COM8"}, {Transport: "serial", Endpoint: "COM11"}},
		{{Transport: "serial", Endpoint: "COM8"}, {Transport: "serial", Endpoint: "COM11"}},
		{{Transport: "serial", Endpoint: "COM8"}},
		{{Transport: "serial", Endpoint: "COM8"}, {Transport: "serial", Endpoint: "COM11"}},
	}}
	registry, err := newRegistry(scanner, time.Hour, 8, func() time.Time {
		now = now.Add(time.Second)
		return now
	})
	if err != nil {
		t.Fatalf("newRegistry() error = %v", err)
	}
	if err := registry.scan(context.Background()); err != nil {
		t.Fatalf("initial scan error = %v", err)
	}
	initial := registry.List()[0]
	for range 4 {
		if err := registry.scan(context.Background()); err != nil {
			t.Fatalf("scan() error = %v", err)
		}
	}
	chunk, err := registry.ReadRecent(8)
	if err != nil {
		t.Fatalf("ReadRecent() error = %v", err)
	}
	got := make([]struct {
		Type     EventType
		Endpoint string
	}, len(chunk.Events))
	for i, event := range chunk.Events {
		got[i] = struct {
			Type     EventType
			Endpoint string
		}{event.Type, event.Endpoint}
	}
	want := []struct {
		Type     EventType
		Endpoint string
	}{
		{EventAppeared, "COM11"},
		{EventDisappeared, "COM11"},
		{EventAppeared, "COM11"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("events = %#v, want %#v", got, want)
	}
	devices := registry.List()
	if len(devices) != 2 || devices[0].Endpoint != "COM11" || devices[0].FirstSeen.Before(initial.FirstSeen) {
		t.Errorf("List() = %#v, want present COM8 and reappeared COM11", devices)
	}
}

func TestRegistryFailedScanDoesNotCreateDisappearance(t *testing.T) {
	scanner := &fakeScanner{results: [][]Endpoint{{{Transport: "serial", Endpoint: "COM8"}}, nil}, errors: map[int]error{1: errors.New("temporary enumeration failure")}}
	registry := newTestRegistry(t, scanner)
	if err := registry.scan(context.Background()); err != nil {
		t.Fatalf("initial scan error = %v", err)
	}
	if err := registry.scan(context.Background()); err == nil {
		t.Fatal("failed scan error = nil, want enumeration error")
	}
	if got := registry.List(); len(got) != 1 || got[0].Endpoint != "COM8" {
		t.Errorf("List() after failed scan = %#v, want unchanged COM8", got)
	}
	if chunk, err := registry.ReadRecent(1); err != nil || len(chunk.Events) != 0 {
		t.Errorf("ReadRecent() = %#v, %v, want no false event", chunk, err)
	}
}

func TestRegistryReadReportsEventOverflow(t *testing.T) {
	scanner := &fakeScanner{results: [][]Endpoint{{}, {{Transport: "serial", Endpoint: "COM1"}}, {}}}
	registry, err := newRegistry(scanner, time.Hour, 1, time.Now)
	if err != nil {
		t.Fatalf("newRegistry() error = %v", err)
	}
	for range 3 {
		if err := registry.scan(context.Background()); err != nil {
			t.Fatalf("scan() error = %v", err)
		}
	}
	chunk, err := registry.Read(context.Background(), 0, 1)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if !chunk.Dropped || len(chunk.Events) != 1 || chunk.Events[0].Type != EventDisappeared || chunk.Next != 2 {
		t.Errorf("Read() = %#v, want dropped newest disappeared event", chunk)
	}
}

func newTestRegistry(t *testing.T, scanner Scanner) *Registry {
	t.Helper()
	registry, err := newRegistry(scanner, time.Hour, 8, time.Now)
	if err != nil {
		t.Fatalf("newRegistry() error = %v", err)
	}
	return registry
}

type fakeScanner struct {
	mu      sync.Mutex
	results [][]Endpoint
	errors  map[int]error
	calls   int
}

func (s *fakeScanner) Scan(context.Context) ([]Endpoint, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	index := s.calls
	s.calls++
	if err := s.errors[index]; err != nil {
		return nil, err
	}
	if index >= len(s.results) {
		return nil, nil
	}
	return append([]Endpoint(nil), s.results[index]...), nil
}
