package app

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/akira-init1/ChannelTerm/internal/core/config"
	"github.com/akira-init1/ChannelTerm/internal/core/connectionpolicy"
	"github.com/akira-init1/ChannelTerm/internal/core/device"
	"github.com/akira-init1/ChannelTerm/internal/core/session"
	serialtransport "github.com/akira-init1/ChannelTerm/internal/core/transport/serial"
)

// TestOpenSerialCreatesRegistersAndReusesSession verifies that the application
// use case is the one place where profile resolution, connection, wake, and
// Manager ownership are coordinated.
func TestOpenSerialCreatesRegistersAndReusesSession(t *testing.T) {
	manager := session.NewManager()
	var created int
	var terminal *fakeConnectedSession
	service, err := NewSerialServiceWithDependencies(manager, SerialDependencies{
		ConfigPath: func() (string, error) { return "test.toml", nil },
		LoadConfig: func(string) (config.File, error) {
			return config.File{Serial: config.Serial{Profiles: map[string]config.SerialProfile{
				"board": {Port: "COM7", BaudRate: 57600, DataBits: 7, Parity: "even", StopBits: "2", Wake: true},
			}}}, nil
		},
		NewSessionID: func() (string, error) { return "session-1", nil },
		NewSession: func(id string, got serialtransport.Config) (ConnectedSession, error) {
			created++
			if want := (serialtransport.Config{Port: "COM7", BaudRate: 57600, DataBits: 7, Parity: serialtransport.ParityEven, StopBits: serialtransport.StopBitsTwo, FlowControl: serialtransport.FlowControlNone}); got != want {
				t.Errorf("serial configuration = %+v, want %+v", got, want)
			}
			terminal = newFakeConnectedSession(id)
			return terminal, nil
		},
	})
	if err != nil {
		t.Fatalf("NewSerialServiceWithDependencies() error = %v", err)
	}

	first, err := service.OpenSerial(context.Background(), OpenSerialRequest{Profile: "board", Label: "left"})
	if err != nil {
		t.Fatalf("OpenSerial() error = %v", err)
	}
	second, err := service.OpenSerial(context.Background(), OpenSerialRequest{Profile: "board", Label: "ignored-on-reuse"})
	if err != nil {
		t.Fatalf("second OpenSerial() error = %v", err)
	}
	if first.Reused || !second.Reused || created != 1 {
		t.Errorf("reuse results = first:%t second:%t created:%d, want false/true/1", first.Reused, second.Reused, created)
	}
	if first.Info.Metadata != (session.SessionMetadata{Transport: "serial", Endpoint: "COM7", Label: "left", Reference: "SER-1"}) {
		t.Errorf("metadata = %#v, want serial COM7 with first label", first.Info.Metadata)
	}
	if !terminal.connected || string(terminal.writtenData()) != "\r" {
		t.Errorf("connected=%t wake=%q, want connected with one carriage return", terminal.connected, terminal.writtenData())
	}
	if closed, err := service.CloseSession(first.Info.ID); err != nil || !closed {
		t.Fatalf("CloseSession() = %t, %v, want true, nil", closed, err)
	}
	if !terminal.closed {
		t.Error("CloseSession() did not close the Manager-owned Session")
	}
}

// TestOpenSerialIgnoresPreferences verifies that the application uses only a
// resolved connection profile to construct the Serial Transport. This prevents
// future display preferences from becoming an implicit serial-open input.
func TestOpenSerialIgnoresPreferences(t *testing.T) {
	manager := session.NewManager()
	var got serialtransport.Config
	service, err := NewSerialServiceWithDependencies(manager, SerialDependencies{
		ConfigPath: func() (string, error) { return "test.toml", nil },
		LoadConfig: func(string) (config.File, error) {
			return config.File{
				Serial: config.Serial{Profiles: map[string]config.SerialProfile{
					"board": {Port: "COM13", BaudRate: 57600},
				}},
				Preferences: config.DefaultPreferences(),
			}, nil
		},
		NewSessionID: func() (string, error) { return "session-preferences", nil },
		NewSession: func(id string, value serialtransport.Config) (ConnectedSession, error) {
			got = value
			return newFakeConnectedSession(id), nil
		},
	})
	if err != nil {
		t.Fatalf("NewSerialServiceWithDependencies() error = %v", err)
	}
	result, err := service.OpenSerial(context.Background(), OpenSerialRequest{Profile: "board"})
	if err != nil {
		t.Fatalf("OpenSerial() error = %v", err)
	}
	if result.Profile.Port != "COM13" || got != (serialtransport.Config{
		Port:        "COM13",
		BaudRate:    57600,
		DataBits:    8,
		Parity:      serialtransport.ParityNone,
		StopBits:    serialtransport.StopBitsOne,
		FlowControl: serialtransport.FlowControlNone,
	}) {
		t.Errorf("OpenSerial() profile/config = %+v/%+v, want COM13 serial profile defaults", result.Profile, got)
	}
	if _, err := service.CloseSession(result.Info.ID); err != nil {
		t.Fatalf("CloseSession() error = %v", err)
	}
}

// TestApplicationRoutesCoreUseCases verifies that adapters can use one
// UI-independent facade for Session, port, device, and connection-policy work
// without taking ownership of the Manager or Registry internals.
func TestApplicationRoutesCoreUseCases(t *testing.T) {
	manager := session.NewManager()
	registry, err := device.NewRegistry(device.ScannerFunc(func(context.Context) ([]device.Endpoint, error) {
		return []device.Endpoint{{Transport: "serial", Endpoint: "COM8"}}, nil
	}))
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	defer registry.Close()
	if err := registry.Start(context.Background()); err != nil {
		t.Fatalf("Registry.Start() error = %v", err)
	}

	application, err := New(Dependencies{
		Manager: manager,
		Devices: registry,
		Policy:  connectionpolicy.PolicyAuto,
		ListSerialPorts: func() ([]serialtransport.Port, error) {
			return []serialtransport.Port{{Name: "COM8"}}, nil
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	terminal := newFakeConnectedSession("session-8")
	if err := manager.RegisterWithMetadata(terminal, session.SessionMetadata{Transport: "serial", Endpoint: "COM8"}); err != nil {
		t.Fatalf("RegisterWithMetadata() error = %v", err)
	}

	ports, err := application.ListSerialPorts(context.Background())
	if err != nil || len(ports) != 1 || ports[0].Name != "COM8" {
		t.Fatalf("ListSerialPorts() = %#v, %v", ports, err)
	}
	devices, err := application.ListDevices(context.Background())
	if err != nil || len(devices) != 1 || devices[0].Endpoint != "COM8" {
		t.Fatalf("ListDevices() = %#v, %v", devices, err)
	}
	decision, err := application.ConnectionDecision(context.Background(), "serial", "COM8")
	if err != nil {
		t.Fatalf("ConnectionDecision() error = %v", err)
	}
	if !decision.Present || !decision.Connected || decision.Action != connectionpolicy.ActionNone || decision.SessionReference != "SER-1" {
		t.Errorf("ConnectionDecision() = %#v, want present active Session with none action", decision)
	}
	if written, err := application.WriteSession(context.Background(), "SER-1", session.WriteRequest{Actor: session.ActorAgent, Data: []byte("status\n")}); err != nil || written != 7 {
		t.Fatalf("WriteSession() = %d, %v", written, err)
	}
	if got := string(terminal.writtenData()); got != "status\n" {
		t.Errorf("written data = %q, want status command", got)
	}
	closed, err := application.CloseSession("SER-1")
	if err != nil || closed.ID != "session-8" || !terminal.closed {
		t.Errorf("CloseSession() = %#v, %v, closed=%t", closed, err, terminal.closed)
	}
}

func TestApplicationResolveSerialTargetUsesNativePortName(t *testing.T) {
	application, err := New(Dependencies{
		Manager: session.NewManager(),
		ListSerialPorts: func() ([]serialtransport.Port, error) {
			return []serialtransport.Port{{Name: "/dev/ttyUSB0"}, {Name: "COM50"}}, nil
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	for _, target := range []string{"/dev/ttyUSB0", "COM50"} {
		got, resolveErr := application.ResolveSerialTarget(context.Background(), target)
		if resolveErr != nil {
			t.Fatalf("ResolveSerialTarget(%q) error = %v", target, resolveErr)
		}
		if got != target {
			t.Errorf("ResolveSerialTarget(%q) = %q, want %q", target, got, target)
		}
	}
	if _, resolveErr := application.ResolveSerialTarget(context.Background(), "SER-COM50"); resolveErr == nil || !strings.Contains(resolveErr.Error(), "is not present") {
		t.Fatalf("ResolveSerialTarget(legacy target) error = %v, want not present", resolveErr)
	}
}

func TestApplicationWriteSessionBoundsPayloadSize(t *testing.T) {
	manager := session.NewManager()
	application, err := New(Dependencies{Manager: manager})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	terminal := newFakeConnectedSession("session-1")
	if err := manager.Register(terminal); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	allowed := make([]byte, MaxSessionWriteBytes)
	if written, err := application.WriteSession(context.Background(), "session-1", session.WriteRequest{Actor: session.ActorAgent, Data: allowed}); err != nil || written != len(allowed) {
		t.Fatalf("WriteSession(maximum) = (%d, %v), want (%d, nil)", written, err, len(allowed))
	}
	before := len(terminal.writtenData())
	tooLarge := make([]byte, MaxSessionWriteBytes+1)
	written, err := application.WriteSession(context.Background(), "session-1", session.WriteRequest{Actor: session.ActorAgent, Data: tooLarge})
	if !errors.Is(err, ErrWritePayloadTooLarge) || written != 0 {
		t.Fatalf("WriteSession(too large) = (%d, %v), want (0, ErrWritePayloadTooLarge)", written, err)
	}
	if got := len(terminal.writtenData()); got != before {
		t.Errorf("oversized write changed terminal bytes from %d to %d", before, got)
	}
}

// fakeConnectedSession is a minimal application-boundary fake that records
// connection, wake, and cleanup without constructing a physical serial port.
type fakeConnectedSession struct {
	mu        sync.Mutex
	id        string
	state     session.SessionState
	connected bool
	closed    bool
	writes    []byte
	activity  []session.SessionEvent
	output    []byte
	outputNew chan struct{}
	events    []session.Event
	onWrite   func(session.WriteRequest)
}

func newFakeConnectedSession(id string) *fakeConnectedSession {
	return &fakeConnectedSession{id: id, state: session.StateNew, outputNew: make(chan struct{})}
}

func (s *fakeConnectedSession) ID() string { return s.id }

func (s *fakeConnectedSession) State() session.SessionState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

func (s *fakeConnectedSession) Connect(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.connected = true
	s.state = session.StateOpen
	return nil
}

func (s *fakeConnectedSession) ReadOutput(ctx context.Context, next session.OutputCursor, maxBytes int) (session.OutputChunk, error) {
	for {
		s.mu.Lock()
		if int(next) < len(s.output) {
			end := min(len(s.output), int(next)+maxBytes)
			chunk := session.OutputChunk{Data: append([]byte(nil), s.output[next:end]...), Next: session.OutputCursor(end)}
			s.mu.Unlock()
			return chunk, nil
		}
		notify := s.outputNew
		closed := s.closed
		s.mu.Unlock()
		if closed {
			return session.OutputChunk{}, session.ErrNotOpen
		}
		select {
		case <-ctx.Done():
			return session.OutputChunk{}, ctx.Err()
		case <-notify:
		}
	}
}

func (s *fakeConnectedSession) ReadRecent(maxBytes int) (session.OutputChunk, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	start := max(0, len(s.output)-maxBytes)
	return session.OutputChunk{Data: append([]byte(nil), s.output[start:]...), Next: session.OutputCursor(len(s.output))}, nil
}

func (*fakeConnectedSession) ReadActivity(context.Context, session.ActivityCursor, int) (session.ActivityChunk, error) {
	return session.ActivityChunk{}, nil
}

func (s *fakeConnectedSession) ReadRecentActivity(limit int) (session.ActivityChunk, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	start := max(0, len(s.activity)-limit)
	return session.ActivityChunk{Events: append([]session.SessionEvent(nil), s.activity[start:]...), Next: session.ActivityCursor(len(s.activity))}, nil
}

func (s *fakeConnectedSession) ReadEvents(_ context.Context, next session.EventCursor, limit int) (session.EventChunk, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if limit <= 0 {
		return session.EventChunk{}, session.ErrInvalidEventReadLimit
	}
	if int(next) >= len(s.events) {
		return session.EventChunk{Next: session.EventCursor(len(s.events))}, nil
	}
	end := min(len(s.events), int(next)+limit)
	return session.EventChunk{Events: append([]session.Event(nil), s.events[next:end]...), Next: session.EventCursor(end)}, nil
}

func (s *fakeConnectedSession) ReadRecentEvents(limit int) (session.EventChunk, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if limit <= 0 {
		return session.EventChunk{}, session.ErrInvalidEventReadLimit
	}
	start := max(0, len(s.events)-limit)
	return session.EventChunk{Events: append([]session.Event(nil), s.events[start:]...), Next: session.EventCursor(len(s.events))}, nil
}

func (s *fakeConnectedSession) PublishEvent(event session.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	event.ID = uint64(len(s.events))
	event.SessionID = s.id
	s.events = append(s.events, event)
}

func (s *fakeConnectedSession) Write(request session.WriteRequest) (int, error) {
	s.mu.Lock()
	s.writes = append(s.writes, request.Data...)
	s.activity = append(s.activity, session.SessionEvent{Actor: request.Actor, Operation: session.OperationWrite, Data: append([]byte(nil), request.Data...)})
	onWrite := s.onWrite
	s.mu.Unlock()
	if onWrite != nil {
		onWrite(request)
	}
	return len(request.Data), nil
}

func (*fakeConnectedSession) Resize(uint16, uint16) error { return nil }

func (s *fakeConnectedSession) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	s.state = session.StateClosed
	close(s.outputNew)
	return nil
}

func (s *fakeConnectedSession) emitOutput(data []byte) {
	s.mu.Lock()
	s.output = append(s.output, data...)
	close(s.outputNew)
	s.outputNew = make(chan struct{})
	s.mu.Unlock()
}

func (s *fakeConnectedSession) writtenData() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]byte(nil), s.writes...)
}

var _ ConnectedSession = (*fakeConnectedSession)(nil)
