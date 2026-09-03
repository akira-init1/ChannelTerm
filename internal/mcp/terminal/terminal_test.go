package terminal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/core/app"
	"github.com/akira-init1/ChannelTerm/internal/core/config"
	"github.com/akira-init1/ChannelTerm/internal/core/connectionpolicy"
	"github.com/akira-init1/ChannelTerm/internal/core/device"
	"github.com/akira-init1/ChannelTerm/internal/core/session"
	"github.com/akira-init1/ChannelTerm/internal/core/tool"
	serialtransport "github.com/akira-init1/ChannelTerm/internal/core/transport/serial"
)

func TestOpenSerialCreatesRegisteredSessionAndReturnsID(t *testing.T) {
	manager := session.NewManager()
	var gotConfig serialtransport.Config
	var created *fakeSession
	tools := newTestTools(t, manager, config.File{
		Serial: config.Serial{Profiles: map[string]config.SerialProfile{
			"board": {Port: "COM7", BaudRate: 57600, DataBits: 7, Parity: "even", StopBits: "2", Wake: true},
		}},
	}, func(id string, serialConfig serialtransport.Config) (connectableSession, error) {
		gotConfig = serialConfig
		created = newFakeSession(id)
		return created, nil
	})

	result, err := callTool(tools, "terminal_open_serial", context.Background(), `{"profile":"board","label":"imx6ull-left"}`)
	if err != nil {
		t.Fatalf("terminal_open_serial error = %v", err)
	}
	if got := result["session_id"]; got != "session-1" {
		t.Errorf("session_id = %v, want session-1", got)
	}
	if got := result["session_ref"]; got != "SER-1" {
		t.Errorf("session_ref = %v, want SER-1", got)
	}
	if _, ok := manager.Get("session-1"); !ok {
		t.Error("open_serial did not register the created session")
	}
	infos := manager.ListInfo()
	if len(infos) != 1 || infos[0].Metadata != (session.SessionMetadata{Transport: "serial", Endpoint: "COM7", Label: "imx6ull-left", Reference: "SER-1"}) {
		t.Errorf("Session metadata = %#v, want serial COM7 with label", infos)
	}
	if !created.connected {
		t.Error("open_serial did not connect the created session")
	}
	if gotConfig != (serialtransport.Config{Port: "COM7", BaudRate: 57600, DataBits: 7, Parity: serialtransport.ParityEven, StopBits: serialtransport.StopBitsTwo, FlowControl: serialtransport.FlowControlNone}) {
		t.Errorf("serial config = %+v, want resolved profile settings", gotConfig)
	}
	if got := string(created.writtenData()); got != "\r" {
		t.Errorf("wake data = %q, want carriage return", got)
	}
	if got := created.writeActors(); !reflect.DeepEqual(got, []session.Actor{session.ActorSystem}) {
		t.Errorf("wake actors = %v, want [system]", got)
	}
}

func TestOpenSerialReusesActiveSessionForSameEndpoint(t *testing.T) {
	manager := session.NewManager()
	created := 0
	tools := newTestTools(t, manager, config.File{}, func(id string, _ serialtransport.Config) (connectableSession, error) {
		created++
		return newFakeSession(id), nil
	})
	first, err := callTool(tools, "terminal_open_serial", context.Background(), `{"port":"COM8","label":"first"}`)
	if err != nil {
		t.Fatalf("first terminal_open_serial error = %v", err)
	}
	second, err := callTool(tools, "terminal_open_serial", context.Background(), `{"port":"COM8","label":"second"}`)
	if err != nil {
		t.Fatalf("second terminal_open_serial error = %v", err)
	}
	if created != 1 {
		t.Errorf("created sessions = %d, want 1", created)
	}
	if first["session_id"] != second["session_id"] || first["session_ref"] != second["session_ref"] {
		t.Errorf("open results = %#v and %#v, want one shared session", first, second)
	}
	if got, ok := second["reused"].(bool); !ok || !got {
		t.Errorf("second reused = %#v, want true", second["reused"])
	}
	infos := manager.ListInfo()
	if len(infos) != 1 || infos[0].Metadata.Label != "first" {
		t.Errorf("Session metadata = %#v, want original shared Session", infos)
	}
}

func TestOpenSerialSavesResolvedProfileBeforeOpening(t *testing.T) {
	manager := session.NewManager()
	path := filepath.Join(t.TempDir(), "serial.toml")
	tools, err := newSerialTools(
		manager,
		app.SerialDependencies{
			ConfigPath:   func() (string, error) { return path, nil },
			LoadConfig:   config.LoadOrCreate,
			NewSession:   func(id string, _ serialtransport.Config) (connectableSession, error) { return newFakeSession(id), nil },
			NewSessionID: func() (string, error) { return "saved-session", nil },
		},
		func() ([]serialtransport.Port, error) { return nil, nil },
	)
	if err != nil {
		t.Fatalf("newSerialTools() error = %v", err)
	}
	if _, err := callTool(tools, "terminal_open_serial", context.Background(), `{"port":"COM8","baud":921600,"save":"board"}`); err != nil {
		t.Fatalf("terminal_open_serial error = %v", err)
	}
	file, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	profile, ok := file.Serial.Profiles["board"]
	if !ok || profile.Port != "COM8" || profile.BaudRate != 921600 || file.Serial.Default != "board" {
		t.Errorf("saved config = %#v, want board profile and default", file)
	}
}

func TestOpenSerialAppliesExplicitProfileOverrides(t *testing.T) {
	manager := session.NewManager()
	var gotConfig serialtransport.Config
	var created *fakeSession
	tools := newTestTools(t, manager, config.File{
		Serial: config.Serial{Profiles: map[string]config.SerialProfile{
			"board": {Port: "COM7", BaudRate: 57600, DataBits: 7, Parity: "even", StopBits: "2", Wake: true},
		}},
	}, func(id string, serialConfig serialtransport.Config) (connectableSession, error) {
		gotConfig = serialConfig
		created = newFakeSession(id)
		return created, nil
	})

	_, err := callTool(tools, "terminal_open_serial", context.Background(), `{"profile":"board","port":"COM9","baud":115200,"data_bits":8,"parity":"none","stop_bits":"1","flow_control":"hardware","wake":false}`)
	if err != nil {
		t.Fatalf("terminal_open_serial error = %v", err)
	}
	if gotConfig != (serialtransport.Config{Port: "COM9", BaudRate: 115200, DataBits: 8, Parity: serialtransport.ParityNone, StopBits: serialtransport.StopBitsOne, FlowControl: serialtransport.FlowControlHardware}) {
		t.Errorf("serial config = %+v, want explicit override settings", gotConfig)
	}
	if got := created.writtenData(); len(got) != 0 {
		t.Errorf("wake data = %q, want no data after wake=false override", got)
	}
	infos := manager.ListInfo()
	if len(infos) != 1 || infos[0].Metadata != (session.SessionMetadata{Transport: "serial", Endpoint: "COM9", Label: "", Reference: "SER-1"}) {
		t.Errorf("Session metadata = %#v, want serial COM9 with stable empty label", infos)
	}
}

func TestOpenSerialBindsMetadataToEachSession(t *testing.T) {
	manager := session.NewManager()
	ids := []string{"session-a", "session-b"}
	tools, err := newSerialTools(
		manager,
		app.SerialDependencies{
			ConfigPath: func() (string, error) { return "test.toml", nil },
			LoadConfig: func(string) (config.File, error) { return config.File{}, nil },
			NewSession: func(id string, _ serialtransport.Config) (connectableSession, error) { return newFakeSession(id), nil },
			NewSessionID: func() (string, error) {
				id := ids[0]
				ids = ids[1:]
				return id, nil
			},
		},
		func() ([]serialtransport.Port, error) { return nil, nil },
	)
	if err != nil {
		t.Fatalf("newSerialTools() error = %v", err)
	}
	for _, input := range []string{
		`{"port":"COM8","label":"imx6ull-left"}`,
		`{"port":"COM6","label":"zynq-debug"}`,
	} {
		if _, err := callTool(tools, "terminal_open_serial", context.Background(), input); err != nil {
			t.Fatalf("terminal_open_serial(%s) error = %v", input, err)
		}
	}

	result, err := callTool(tools, "terminal_list_sessions", context.Background(), `{}`)
	if err != nil {
		t.Fatalf("terminal_list_sessions error = %v", err)
	}
	sessions := result["sessions"].([]sessionSummary)
	want := []sessionSummary{
		{ID: "session-a", Reference: "SER-1", Transport: "serial", Endpoint: "COM8", Label: "imx6ull-left", State: "open"},
		{ID: "session-b", Reference: "SER-2", Transport: "serial", Endpoint: "COM6", Label: "zynq-debug", State: "open"},
	}
	if !reflect.DeepEqual(sessions, want) {
		t.Errorf("sessions = %#v, want %#v", sessions, want)
	}
}

func TestConnectionDecisionReturnsPolicyActionsWithoutCreatingSessions(t *testing.T) {
	for _, tt := range []struct {
		name   string
		policy connectionpolicy.Policy
		want   string
	}{
		{name: "ask", policy: connectionpolicy.PolicyAsk, want: "ask"},
		{name: "auto", policy: connectionpolicy.PolicyAuto, want: "connect"},
		{name: "deny", policy: connectionpolicy.PolicyDeny, want: "deny"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			manager := session.NewManager()
			devices := newPresentDeviceRegistry(t, "COM6")
			tools, err := NewConnectionDecisionTools(manager, devices, tt.policy)
			if err != nil {
				t.Fatalf("NewConnectionDecisionTools() error = %v", err)
			}
			result, err := callTool(tools, "terminal_get_connection_decision", context.Background(), `{"transport":"serial","endpoint":"COM6"}`)
			if err != nil {
				t.Fatalf("terminal_get_connection_decision error = %v", err)
			}
			if result["present"] != true || result["connected"] != false || result["policy"] != string(tt.policy) || result["action"] != tt.want {
				t.Errorf("decision = %#v, want present unconnected %s action", result, tt.want)
			}
			if sessions := manager.ListInfo(); len(sessions) != 0 {
				t.Errorf("decision created sessions = %#v, want none", sessions)
			}
		})
	}
}

func TestConnectionDecisionUsesExactActiveSessionMetadata(t *testing.T) {
	manager := session.NewManager()
	active := newFakeSession("session-b")
	if err := active.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	failed := newFakeSession("session-a")
	failed.mu.Lock()
	failed.state = session.StateFailed
	failed.mu.Unlock()
	for _, entry := range []struct {
		terminal *fakeSession
		endpoint string
	}{
		{terminal: active, endpoint: "COM8"},
		{terminal: failed, endpoint: "COM6"},
	} {
		if err := manager.RegisterWithMetadata(entry.terminal, session.SessionMetadata{Transport: "serial", Endpoint: entry.endpoint, Label: "display-only"}); err != nil {
			t.Fatalf("RegisterWithMetadata(%q) error = %v", entry.endpoint, err)
		}
	}
	devices := newPresentDeviceRegistry(t, "COM6", "COM8")
	tools, err := NewConnectionDecisionTools(manager, devices, connectionpolicy.PolicyAsk)
	if err != nil {
		t.Fatalf("NewConnectionDecisionTools() error = %v", err)
	}
	for _, tt := range []struct {
		endpoint      string
		wantConnected bool
		wantAction    string
		wantReason    string
	}{
		{endpoint: "COM8", wantConnected: true, wantAction: "none", wantReason: "already_connected"},
		{endpoint: "COM6", wantConnected: false, wantAction: "ask"},
	} {
		result, err := callTool(tools, "terminal_get_connection_decision", context.Background(), `{"transport":"serial","endpoint":"`+tt.endpoint+`"}`)
		if err != nil {
			t.Fatalf("terminal_get_connection_decision(%s) error = %v", tt.endpoint, err)
		}
		reason, _ := result["reason"].(string)
		if result["connected"] != tt.wantConnected || result["action"] != tt.wantAction || reason != tt.wantReason {
			t.Errorf("decision for %s = %#v, want connected=%t action=%q reason=%q", tt.endpoint, result, tt.wantConnected, tt.wantAction, tt.wantReason)
		}
		if tt.wantConnected && result["session_ref"] != "SER-1" {
			t.Errorf("connected decision = %#v, want session_ref SER-1", result)
		}
	}
}

func TestConnectionDecisionReturnsNoneForMissingDeviceAndDenyDoesNotBlockManualOpen(t *testing.T) {
	manager := session.NewManager()
	devices := newPresentDeviceRegistry(t, "COM6")
	decisionTools, err := NewConnectionDecisionTools(manager, devices, connectionpolicy.PolicyDeny)
	if err != nil {
		t.Fatalf("NewConnectionDecisionTools() error = %v", err)
	}
	missing, err := callTool(decisionTools, "terminal_get_connection_decision", context.Background(), `{"transport":"serial","endpoint":"COM8"}`)
	if err != nil {
		t.Fatalf("missing-device decision error = %v", err)
	}
	if missing["present"] != false || missing["action"] != "none" || missing["reason"] != "device_not_present" {
		t.Errorf("missing-device decision = %#v, want not present and none", missing)
	}
	serialTools := newTestTools(t, manager, config.File{}, nil)
	if _, err := callTool(serialTools, "terminal_open_serial", context.Background(), `{"port":"COM6"}`); err != nil {
		t.Errorf("terminal_open_serial under deny policy error = %v, want explicit manual open to remain allowed", err)
	}
}

// newPresentDeviceRegistry supplies a started Registry whose initial scan
// provides stable presence state without emitting discovery events or opening a
// serial endpoint.
func newPresentDeviceRegistry(t *testing.T, endpoints ...string) *device.Registry {
	t.Helper()
	registry, err := device.NewRegistry(device.ScannerFunc(func(context.Context) ([]device.Endpoint, error) {
		found := make([]device.Endpoint, 0, len(endpoints))
		for _, endpoint := range endpoints {
			found = append(found, device.Endpoint{Transport: "serial", Endpoint: endpoint})
		}
		return found, nil
	}))
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	if err := registry.Start(context.Background()); err != nil {
		t.Fatalf("Registry.Start() error = %v", err)
	}
	t.Cleanup(registry.Close)
	return registry
}

func TestTerminalToolsReadWriteAndCloseSession(t *testing.T) {
	manager := session.NewManager()
	terminal := newFakeSession("session-1")
	terminal.recent = []byte("ready")
	if err := terminal.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	if err := manager.Register(terminal); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	tools := newTestTools(t, manager, config.File{}, nil)

	read, err := callTool(tools, "terminal_read", context.Background(), `{"session_id":"session-1","max_bytes":32}`)
	if err != nil {
		t.Fatalf("terminal_read error = %v", err)
	}
	if got := read["data"]; got != "ready" {
		t.Errorf("read data = %v, want ready", got)
	}

	write, err := callTool(tools, "terminal_write", context.Background(), `{"session_id":"session-1","data":"status\n"}`)
	if err != nil {
		t.Fatalf("terminal_write error = %v", err)
	}
	if got := write["bytes_written"]; got != 7 {
		t.Errorf("bytes_written = %v, want 7", got)
	}
	if got := string(terminal.writtenData()); got != "status\n" {
		t.Errorf("write data = %q, want status newline", got)
	}
	if got := terminal.writeActors(); !reflect.DeepEqual(got, []session.Actor{session.ActorAgent}) {
		t.Errorf("write actors = %v, want [agent]", got)
	}

	closed, err := callTool(tools, "terminal_close", context.Background(), `{"session_id":"session-1"}`)
	if err != nil {
		t.Fatalf("terminal_close error = %v", err)
	}
	if got := closed["closed"]; got != true {
		t.Errorf("closed = %v, want true", got)
	}
	if terminal.State() != session.StateClosed {
		t.Errorf("session state = %s, want closed", terminal.State())
	}
	if _, ok := manager.Get("session-1"); ok {
		t.Error("terminal_close left the session registered")
	}
}

func TestTerminalWriteDecodesEncodingsBeforeWriting(t *testing.T) {
	manager := session.NewManager()
	terminal := newFakeSession("session-1")
	if err := terminal.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	if err := manager.Register(terminal); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	tools := newTestTools(t, manager, config.File{}, nil)

	tests := []struct {
		name  string
		input string
		want  []byte
	}{
		{name: "default utf8", input: `{"session_id":"session-1","data":"A\u0000B"}`, want: []byte{'A', 0, 'B'}},
		{name: "hex uppercase and whitespace", input: `{"session_id":"session-1","encoding":"hex","data":"55 AA\n01 af"}`, want: []byte{0x55, 0xAA, 0x01, 0xAF}},
		{name: "base64", input: `{"session_id":"session-1","encoding":"base64","data":"VaoBAgM="}`, want: []byte{0x55, 0xAA, 0x01, 0x02, 0x03}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := terminal.writtenData()
			result, err := callTool(tools, "terminal_write", context.Background(), tt.input)
			if err != nil {
				t.Fatalf("terminal_write error = %v", err)
			}
			if got := result["bytes_written"]; got != len(tt.want) {
				t.Errorf("bytes_written = %v, want %d", got, len(tt.want))
			}
			if got := terminal.writtenData(); !reflect.DeepEqual(got[len(before):], tt.want) {
				t.Errorf("newly written bytes = %x, want %x", got[len(before):], tt.want)
			}
		})
	}

	for _, input := range []string{
		`{"session_id":"session-1","encoding":"hex","data":"0"}`,
		`{"session_id":"session-1","encoding":"hex","data":"zz"}`,
		`{"session_id":"session-1","encoding":"base64","data":"not/base64"}`,
	} {
		before := terminal.writtenData()
		if _, err := callTool(tools, "terminal_write", context.Background(), input); err == nil {
			t.Errorf("terminal_write(%s) error = nil, want invalid encoding error", input)
		}
		if got := terminal.writtenData(); !reflect.DeepEqual(got, before) {
			t.Errorf("invalid input %s wrote %x, want no change", input, got)
		}
	}
}

func TestTerminalWriteRejectsInvalidActorWithoutWriting(t *testing.T) {
	manager := session.NewManager()
	terminal := newFakeSession("session-1")
	if err := terminal.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	if err := manager.Register(terminal); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	tools := newTestTools(t, manager, config.File{}, nil)

	_, err := callTool(tools, "terminal_write", context.Background(), `{"session_id":"session-1","data":"status","actor":"unknown"}`)
	if !errors.Is(err, session.ErrInvalidActor) {
		t.Errorf("terminal_write error = %v, want ErrInvalidActor", err)
	}
	if got := terminal.writtenData(); len(got) != 0 {
		t.Errorf("invalid actor wrote %q, want no Session data", got)
	}
}

func TestTerminalReadActivityReturnsActorAndLosslessPayload(t *testing.T) {
	manager := session.NewManager()
	terminal := newFakeSession("session-1")
	if err := terminal.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	if err := manager.Register(terminal); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	tools := newTestTools(t, manager, config.File{}, nil)
	if _, err := callTool(tools, "terminal_write", context.Background(), `{"session_id":"session-1","data":"human\u0000input","actor":"user"}`); err != nil {
		t.Fatalf("terminal_write error = %v", err)
	}

	result, err := callTool(tools, "terminal_read_activity", context.Background(), `{"session_id":"session-1"}`)
	if err != nil {
		t.Fatalf("terminal_read_activity error = %v", err)
	}
	events, ok := result["events"].([]activityEventResult)
	if !ok || len(events) != 1 {
		t.Fatalf("events = %#v, want one activityEventResult", result["events"])
	}
	event := events[0]
	if event.Actor != "user" || event.Operation != "write" || event.Encoding != "base64" || event.Data != "aHVtYW4AaW5wdXQ=" || event.Timestamp == "" {
		t.Errorf("event = %+v, want user write Base64 payload and timestamp", event)
	}
	if result["next"] != uint64(1) || result["dropped"] != false {
		t.Errorf("activity cursor result = %#v, want next=1 dropped=false", result)
	}
}

func TestTerminalReadEncodesRawBytesWithoutChangingCursor(t *testing.T) {
	manager := session.NewManager()
	terminal := newFakeSession("session-1")
	terminal.recent = []byte{0x00, 0xFF, 0x55}
	if err := terminal.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	if err := manager.Register(terminal); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	tools := newTestTools(t, manager, config.File{}, nil)

	for _, tt := range []struct {
		encoding string
		want     string
	}{
		{encoding: "hex", want: "00ff55"},
		{encoding: "base64", want: "AP9V"},
	} {
		result, err := callTool(tools, "terminal_read", context.Background(), fmt.Sprintf(`{"session_id":"session-1","encoding":%q}`, tt.encoding))
		if err != nil {
			t.Fatalf("terminal_read(%s) error = %v", tt.encoding, err)
		}
		if result["data"] != tt.want || result["bytes_read"] != 3 || result["next"] != uint64(3) {
			t.Errorf("terminal_read(%s) = %#v, want encoded three-byte chunk with next=3", tt.encoding, result)
		}
	}
	if _, err := callTool(tools, "terminal_read", context.Background(), `{"session_id":"session-1"}`); !errors.Is(err, ErrInvalidUTF8Output) {
		t.Errorf("terminal_read utf8 binary error = %v, want ErrInvalidUTF8Output", err)
	}
}

func TestTerminalWaitTimeoutAndParentCancellation(t *testing.T) {
	manager := session.NewManager()
	terminal := newWaitingSession("session-1")
	if err := terminal.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	if err := manager.Register(terminal); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	tools := newTestTools(t, manager, config.File{}, nil)

	if _, err := callTool(tools, "terminal_read", context.Background(), `{"session_id":"session-1","cursor":0,"timeout_ms":1}`); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("timeout error = %v, want context.DeadlineExceeded", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := callTool(tools, "terminal_read", ctx, `{"session_id":"session-1","cursor":0,"timeout_ms":1000}`); !errors.Is(err, context.Canceled) {
		t.Errorf("parent cancellation error = %v, want context.Canceled", err)
	}
	for _, input := range []string{
		`{"session_id":"session-1","cursor":0,"timeout_ms":0}`,
		`{"session_id":"session-1","cursor":0,"timeout_ms":86400001}`,
		`{"session_id":"session-1","timeout_ms":1}`,
	} {
		if _, err := callTool(tools, "terminal_read", context.Background(), input); err == nil {
			t.Errorf("terminal_read(%s) error = nil, want timeout validation error", input)
		}
	}

	resultCh := make(chan tool.Result, 1)
	errCh := make(chan error, 1)
	go func() {
		result, err := callTool(tools, "terminal_read", context.Background(), `{"session_id":"session-1","cursor":0,"timeout_ms":1000,"encoding":"hex"}`)
		resultCh <- result
		errCh <- err
	}()
	terminal.emit(session.OutputChunk{Data: []byte{0, 0xFF}, Next: 2})
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("data-before-timeout error = %v", err)
		}
		if result := <-resultCh; result["data"] != "00ff" || result["bytes_read"] != 2 {
			t.Errorf("data-before-timeout result = %#v", result)
		}
	case <-time.After(time.Second):
		t.Fatal("terminal wait did not return after data")
	}
	if err := terminal.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if _, err := callTool(tools, "terminal_read", context.Background(), `{"session_id":"session-1","cursor":2,"timeout_ms":1}`); !errors.Is(err, io.EOF) {
		t.Errorf("closed session wait error = %v, want io.EOF", err)
	}
}

func TestTerminalToolsRejectInvalidSessionIDAndHonorCancellation(t *testing.T) {
	manager := session.NewManager()
	terminal := newFakeSession("session-1")
	if err := terminal.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	if err := manager.Register(terminal); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	tools := newTestTools(t, manager, config.File{}, nil)

	if _, err := callTool(tools, "terminal_write", context.Background(), `{"session_id":"missing","data":"status"}`); !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("write missing session error = %v, want ErrSessionNotFound", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := callTool(tools, "terminal_read", ctx, `{"session_id":"session-1","cursor":0}`); !errors.Is(err, context.Canceled) {
		t.Errorf("read cancelled context error = %v, want context.Canceled", err)
	}
	if _, err := callTool(tools, "terminal_open_serial", ctx, `{}`); !errors.Is(err, context.Canceled) {
		t.Errorf("open_serial cancelled context error = %v, want context.Canceled", err)
	}
}

func TestTerminalToolsPreserveUnderlyingErrors(t *testing.T) {
	manager := session.NewManager()
	deviceErr := errors.New("device write failed")
	terminal := newFakeSession("session-1")
	terminal.writeErr = deviceErr
	if err := terminal.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	if err := manager.Register(terminal); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	tools := newTestTools(t, manager, config.File{}, nil)

	if _, err := callTool(tools, "terminal_write", context.Background(), `{"session_id":"session-1","data":"status"}`); !errors.Is(err, deviceErr) {
		t.Errorf("write error = %v, want to unwrap device error", err)
	}
}

func TestTerminalListSessionsReturnsSortedSnapshot(t *testing.T) {
	manager := session.NewManager()
	for _, entry := range []struct {
		id       string
		endpoint string
		label    string
	}{
		{id: "session-b", endpoint: "COM6", label: "zynq-debug"},
		{id: "session-a", endpoint: "COM8", label: "imx6ull-left"},
	} {
		if err := manager.RegisterWithMetadata(newFakeSession(entry.id), session.SessionMetadata{Transport: "serial", Endpoint: entry.endpoint, Label: entry.label}); err != nil {
			t.Fatalf("RegisterWithMetadata(%q) error = %v", entry.id, err)
		}
	}
	tools := newTestTools(t, manager, config.File{}, nil)
	result, err := callTool(tools, "terminal_list_sessions", context.Background(), `{}`)
	if err != nil {
		t.Fatalf("terminal_list_sessions error = %v", err)
	}
	sessions, ok := result["sessions"].([]sessionSummary)
	if !ok {
		t.Fatalf("sessions = %#v, want []sessionSummary", result["sessions"])
	}
	if want := []sessionSummary{
		{ID: "session-b", Reference: "SER-1", Transport: "serial", Endpoint: "COM6", Label: "zynq-debug", State: "new"},
		{ID: "session-a", Reference: "SER-2", Transport: "serial", Endpoint: "COM8", Label: "imx6ull-left", State: "new"},
	}; !reflect.DeepEqual(sessions, want) {
		t.Errorf("sessions = %#v, want %#v", sessions, want)
	}
	if _, err := callTool(tools, "terminal_close", context.Background(), `{"session_id":"session-a"}`); err != nil {
		t.Fatalf("terminal_close error = %v", err)
	}
	result, err = callTool(tools, "terminal_list_sessions", context.Background(), `{}`)
	if err != nil {
		t.Fatalf("terminal_list_sessions after close error = %v", err)
	}
	sessions = result["sessions"].([]sessionSummary)
	if want := []sessionSummary{{ID: "session-b", Reference: "SER-1", Transport: "serial", Endpoint: "COM6", Label: "zynq-debug", State: "new"}}; !reflect.DeepEqual(sessions, want) {
		t.Errorf("sessions after close = %#v, want %#v", sessions, want)
	}
}

func TestTerminalListSessionsAllowsEmptyAndDuplicateLabels(t *testing.T) {
	manager := session.NewManager()
	for _, entry := range []struct {
		id       string
		endpoint string
		label    string
	}{
		{id: "session-a", endpoint: "COM8", label: "board"},
		{id: "session-b", endpoint: "COM6", label: "board"},
		{id: "session-c", endpoint: "COM3"},
	} {
		terminal := newFakeSession(entry.id)
		if err := terminal.Connect(context.Background()); err != nil {
			t.Fatalf("Connect(%q) error = %v", entry.id, err)
		}
		if err := manager.RegisterWithMetadata(terminal, session.SessionMetadata{Transport: "serial", Endpoint: entry.endpoint, Label: entry.label}); err != nil {
			t.Fatalf("RegisterWithMetadata(%q) error = %v", entry.id, err)
		}
	}
	tools := newTestTools(t, manager, config.File{}, nil)
	result, err := callTool(tools, "terminal_list_sessions", context.Background(), `{}`)
	if err != nil {
		t.Fatalf("terminal_list_sessions error = %v", err)
	}
	sessions := result["sessions"].([]sessionSummary)
	if got := []string{sessions[0].Label, sessions[1].Label, sessions[2].Label}; !reflect.DeepEqual(got, []string{"board", "board", ""}) {
		t.Errorf("labels = %q, want duplicate labels and a stable empty label", got)
	}
	if _, err := callTool(tools, "terminal_write", context.Background(), `{"session_id":"session-b","data":"status"}`); err != nil {
		t.Errorf("terminal_write by session ID with duplicate labels error = %v", err)
	}
	if _, err := callTool(tools, "terminal_write", context.Background(), `{"session_id":"board","data":"status"}`); !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("terminal_write by label error = %v, want ErrSessionNotFound", err)
	}
}

func TestLeaseToolsBlockOrdinaryWritesAndExposeSessionState(t *testing.T) {
	manager := session.NewManager()
	terminal := newFakeSession("session-1")
	if err := terminal.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := manager.RegisterWithMetadata(terminal, session.SessionMetadata{Transport: "serial", Endpoint: "COM8"}); err != nil {
		t.Fatal(err)
	}
	application, err := app.New(app.Dependencies{Manager: manager})
	if err != nil {
		t.Fatal(err)
	}
	tools, err := NewTools(application)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := callTool(tools, "terminal_acquire_lease", context.Background(), `{"session_id":"SER-1","owner":"transfer-owner","type":"file-transfer"}`); err != nil {
		t.Fatalf("terminal_acquire_lease error = %v", err)
	}
	if _, err := callTool(tools, "terminal_write", context.Background(), `{"session_id":"SER-1","data":"blocked"}`); !errors.Is(err, app.ErrSessionBusy) || !strings.Contains(err.Error(), "Session SER-1 is locked by file-transfer") {
		t.Errorf("terminal_write while leased error = %v, want friendly busy error", err)
	}
	if _, err := callTool(tools, "terminal_write_leased", context.Background(), `{"session_id":"SER-1","owner":"transfer-owner","data":"allowed"}`); err != nil {
		t.Fatalf("terminal_write_leased error = %v", err)
	}
	result, err := callTool(tools, "terminal_list_sessions", context.Background(), `{}`)
	if err != nil {
		t.Fatal(err)
	}
	sessions := result["sessions"].([]sessionSummary)
	if len(sessions) != 1 || sessions[0].Lease == nil || sessions[0].Lease.Type != "file-transfer" || sessions[0].Lease.State != "active" {
		t.Errorf("terminal_list_sessions lease = %#v, want active file-transfer", sessions)
	}
	if _, err := callTool(tools, "terminal_release_lease", context.Background(), `{"session_id":"SER-1","owner":"transfer-owner"}`); err != nil {
		t.Fatalf("terminal_release_lease error = %v", err)
	}
	if _, err := callTool(tools, "terminal_write", context.Background(), `{"session_id":"SER-1","data":"restored"}`); err != nil {
		t.Fatalf("terminal_write after release error = %v", err)
	}
	if got := string(terminal.writtenData()); got != "allowedrestored" {
		t.Errorf("written data = %q, want allowedrestored", got)
	}
}

func TestSessionEventsToolReturnsLifecycleLeaseAndFileProgressWithoutTerminalData(t *testing.T) {
	manager := session.NewManager()
	terminal := newFakeSession("session-1")
	if err := terminal.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := manager.RegisterWithMetadata(terminal, session.SessionMetadata{Transport: "serial", Endpoint: "COM8"}); err != nil {
		t.Fatal(err)
	}
	application, err := app.New(app.Dependencies{Manager: manager})
	if err != nil {
		t.Fatal(err)
	}
	tools, err := NewTools(application)
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range []struct {
		name  string
		input string
	}{
		{"terminal_session_attach", `{"session_id":"SER-1","actor":"user"}`},
		{"terminal_acquire_lease", `{"session_id":"SER-1","owner":"transfer-owner","type":"file-transfer"}`},
		{"terminal_report_file_transfer", `{"session_id":"SER-1","type":"FILE_TRANSFER_PROGRESS","actor":"user","metadata":{"sent":622592,"total":1048576,"percent":59.4,"speed":850000}}`},
	} {
		if _, err := callTool(tools, input.name, context.Background(), input.input); err != nil {
			t.Fatalf("%s error = %v", input.name, err)
		}
	}
	result, err := callTool(tools, "terminal_session_events", context.Background(), `{"session_id":"SER-1","max_events":8}`)
	if err != nil {
		t.Fatal(err)
	}
	events := result["events"].([]sessionEventResult)
	if len(events) < 4 || events[len(events)-3].Type != string(session.EventSessionAttached) || events[len(events)-2].Type != string(session.EventLeaseAcquired) || events[len(events)-1].Type != string(session.EventFileTransferProgress) {
		t.Errorf("session events = %#v, want attached, lease acquired, and progress", events)
	}
	progress := events[len(events)-1]
	if progress.Metadata["sent"] != float64(622592) || progress.Metadata["total"] != float64(1048576) || progress.Metadata["percent"] != 59.4 || progress.Metadata["speed"] != float64(850000) {
		t.Errorf("progress metadata = %#v, want structured JSON fields", progress.Metadata)
	}
	if got := terminal.writtenData(); len(got) != 0 {
		t.Errorf("terminal bytes = %q, want events not to write terminal output", got)
	}
}

func TestTerminalToolsAcceptShortSessionReference(t *testing.T) {
	manager := session.NewManager()
	terminal := newFakeSession("opaque-session-id")
	if err := terminal.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	if err := manager.RegisterWithMetadata(terminal, session.SessionMetadata{Transport: "serial", Endpoint: "COM8"}); err != nil {
		t.Fatalf("RegisterWithMetadata() error = %v", err)
	}
	tools := newTestTools(t, manager, config.File{}, nil)
	if _, err := callTool(tools, "terminal_write", context.Background(), `{"session_id":"SER-1","data":"status\n"}`); err != nil {
		t.Fatalf("terminal_write by reference error = %v", err)
	}
	if got := string(terminal.writtenData()); got != "status\n" {
		t.Errorf("reference write data = %q, want status newline", got)
	}
	closed, err := callTool(tools, "terminal_close", context.Background(), `{"session_id":"SER-1"}`)
	if err != nil {
		t.Fatalf("terminal_close by reference error = %v", err)
	}
	if got := closed["session_id"]; got != "opaque-session-id" {
		t.Errorf("close session_id = %v, want opaque-session-id", got)
	}
	if got := closed["session_ref"]; got != "SER-1" {
		t.Errorf("close session_ref = %v, want SER-1", got)
	}
}

func TestOpenSerialRejectsControlCharacterLabel(t *testing.T) {
	manager := session.NewManager()
	tools := newTestTools(t, manager, config.File{}, nil)
	if _, err := callTool(tools, "terminal_open_serial", context.Background(), `{"port":"COM8","label":"board\nleft"}`); !errors.Is(err, ErrInvalidSessionLabel) {
		t.Errorf("terminal_open_serial control label error = %v, want ErrInvalidSessionLabel", err)
	}
	if got := manager.ListInfo(); len(got) != 0 {
		t.Errorf("registered sessions after invalid label = %#v, want none", got)
	}
}

func TestTerminalListSerialPorts(t *testing.T) {
	tests := []struct {
		name      string
		ports     []serialtransport.Port
		listError error
		wantError string
	}{
		{name: "no ports"},
		{name: "one port", ports: []serialtransport.Port{{Name: "COM8"}}},
		{name: "multiple ports", ports: []serialtransport.Port{{Name: "COM3"}, {Name: "COM8"}}},
		{name: "enumeration failure", listError: errors.New("enumerator unavailable"), wantError: "enumerator unavailable"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager := session.NewManager()
			tools := newTestToolsWithPortLister(t, manager, config.File{}, nil, func() ([]serialtransport.Port, error) {
				return tt.ports, tt.listError
			})

			result, err := callTool(tools, "terminal_list_serial_ports", context.Background(), `{}`)
			if tt.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantError) {
					t.Fatalf("terminal_list_serial_ports error = %v, want containing %q", err, tt.wantError)
				}
				return
			}
			if err != nil {
				t.Fatalf("terminal_list_serial_ports error = %v", err)
			}
			wantPorts := tt.ports
			if wantPorts == nil {
				wantPorts = []serialtransport.Port{}
			}
			ports, ok := result["ports"].([]serialtransport.Port)
			if !ok || !reflect.DeepEqual(ports, wantPorts) {
				t.Errorf("ports = %#v, want %#v", result["ports"], wantPorts)
			}
			if tt.name == "no ports" {
				encoded, marshalErr := json.Marshal(result)
				if marshalErr != nil || string(encoded) != `{"ports":[]}` {
					t.Errorf("empty ports JSON = %q, %v; want {\"ports\":[]}", encoded, marshalErr)
				}
			}
			if sessions := manager.List(); len(sessions) != 0 {
				t.Errorf("manager sessions = %#v, want no sessions", sessions)
			}
		})
	}
}

func TestTerminalListSerialPortsSchema(t *testing.T) {
	tools := newTestTools(t, session.NewManager(), config.File{}, nil)
	for _, candidate := range tools {
		if candidate.Name() != "terminal_list_serial_ports" {
			continue
		}
		schema := candidate.InputSchema()
		if schema.Type != "object" || len(schema.Properties) != 0 || len(schema.Required) != 0 {
			t.Errorf("terminal_list_serial_ports schema = %#v, want empty object", schema)
		}
		return
	}
	t.Fatal("terminal_list_serial_ports was not registered")
}

func TestTerminalListDevicesReturnsBestEffortSerialMetadata(t *testing.T) {
	store, err := device.LoadStateStore(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("LoadStateStore() error = %v", err)
	}
	registry, err := device.NewRegistryWithStateStore(device.ScannerFunc(func(context.Context) ([]device.Endpoint, error) {
		return []device.Endpoint{{
			Transport: "serial",
			Endpoint:  "COM6",
			Metadata: device.SerialMetadata{
				VID:          "0403",
				PID:          "6010",
				USBSerial:    "ABC123",
				Manufacturer: "FTDI",
				Product:      "FT232H",
				USBPath:      "PCIROOT(0)#USBROOT(0)#USB(2)",
			},
		}}, nil
	}), store)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	defer registry.Close()
	if err := registry.Start(context.Background()); err != nil {
		t.Fatalf("Registry.Start() error = %v", err)
	}
	tools, err := NewDeviceTools(registry)
	if err != nil {
		t.Fatalf("NewDeviceTools() error = %v", err)
	}
	result, err := callTool(tools, "terminal_list_devices", context.Background(), `{}`)
	if err != nil {
		t.Fatalf("terminal_list_devices error = %v", err)
	}
	devices, ok := result["devices"].([]deviceResult)
	if !ok || len(devices) != 1 {
		t.Fatalf("devices = %#v, want one deviceResult", result["devices"])
	}
	got := devices[0]
	if got.VID != "0403" || got.PID != "6010" || got.USBSerial != "ABC123" || got.Manufacturer != "FTDI" || got.Product != "FT232H" || got.USBPath == "" {
		t.Errorf("device metadata = %#v, want complete metadata", got)
	}
	if !strings.HasPrefix(got.DeviceID, "dev_") || got.IdentityMethod != string(device.IdentityUSBSerial) || !got.Persistent {
		t.Errorf("device identity = %#v, want persistent USB serial identity", got)
	}
}

// newTestTools creates serial tools with in-memory config and a deterministic
// ID so Tool tests never open a physical serial port or write user config.
func newTestTools(t *testing.T, manager *session.Manager, file config.File, factory serialSessionFactory) []tool.Tool {
	return newTestToolsWithPortLister(t, manager, file, factory, func() ([]serialtransport.Port, error) {
		return nil, nil
	})
}

// newTestToolsWithPortLister adds a fake device enumerator so port-list tests
// remain deterministic without requiring a host serial device.
func newTestToolsWithPortLister(t *testing.T, manager *session.Manager, file config.File, factory serialSessionFactory, listPorts serialPortLister) []tool.Tool {
	t.Helper()
	if factory == nil {
		factory = func(id string, _ serialtransport.Config) (connectableSession, error) {
			return newFakeSession(id), nil
		}
	}
	tools, err := newSerialTools(
		manager,
		app.SerialDependencies{
			ConfigPath:   func() (string, error) { return "test.toml", nil },
			LoadConfig:   func(string) (config.File, error) { return file, nil },
			NewSession:   factory,
			NewSessionID: func() (string, error) { return "session-1", nil },
		},
		listPorts,
	)
	if err != nil {
		t.Fatalf("newSerialTools() error = %v", err)
	}
	return tools
}

func callTool(tools []tool.Tool, name string, ctx context.Context, input string) (tool.Result, error) {
	for _, candidate := range tools {
		if candidate.Name() == name {
			return candidate.Call(ctx, json.RawMessage(input))
		}
	}
	return nil, fmt.Errorf("test tool %q not found", name)
}

// fakeSession records calls while implementing the same public lifecycle that
// terminal tools use through Session and connectableSession interfaces.
type fakeSession struct {
	mu sync.Mutex

	id        string
	state     session.SessionState
	connected bool
	recent    []byte
	activity  []session.SessionEvent
	events    []session.Event
	written   []byte
	actors    []session.Actor
	writeErr  error
	closeErr  error
}

func newFakeSession(id string) *fakeSession {
	return &fakeSession{id: id, state: session.StateNew}
}

func (s *fakeSession) ID() string {
	return s.id
}

func (s *fakeSession) State() session.SessionState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

func (s *fakeSession) Connect(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.connected = true
	s.state = session.StateOpen
	return nil
}

func (s *fakeSession) ReadOutput(ctx context.Context, next session.OutputCursor, maxBytes int) (session.OutputChunk, error) {
	if err := ctx.Err(); err != nil {
		return session.OutputChunk{}, err
	}
	return s.ReadRecent(maxBytes)
}

func (s *fakeSession) ReadRecent(maxBytes int) (session.OutputChunk, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state != session.StateOpen {
		return session.OutputChunk{}, session.ErrNotOpen
	}
	if maxBytes <= 0 {
		return session.OutputChunk{}, errors.New("invalid read limit")
	}
	data := append([]byte(nil), s.recent...)
	if len(data) > maxBytes {
		data = data[len(data)-maxBytes:]
	}
	return session.OutputChunk{Data: data, Next: session.OutputCursor(len(s.recent))}, nil
}

// ReadActivity returns already-recorded fake activity so Tool tests can verify
// cursor serialization without depending on a physical transport.
func (s *fakeSession) ReadActivity(ctx context.Context, next session.ActivityCursor, maxEvents int) (session.ActivityChunk, error) {
	if err := ctx.Err(); err != nil {
		return session.ActivityChunk{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state != session.StateOpen {
		return session.ActivityChunk{}, session.ErrNotOpen
	}
	if maxEvents <= 0 {
		return session.ActivityChunk{}, session.ErrInvalidActivityReadLimit
	}
	if int(next) >= len(s.activity) {
		return session.ActivityChunk{}, io.EOF
	}
	end := min(len(s.activity), int(next)+maxEvents)
	events := append([]session.SessionEvent(nil), s.activity[next:end]...)
	return session.ActivityChunk{Events: events, Next: session.ActivityCursor(end)}, nil
}

// ReadRecentActivity snapshots fake activity with the same tail cursor rule as Core.
func (s *fakeSession) ReadRecentActivity(maxEvents int) (session.ActivityChunk, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state != session.StateOpen {
		return session.ActivityChunk{}, session.ErrNotOpen
	}
	if maxEvents <= 0 {
		return session.ActivityChunk{}, session.ErrInvalidActivityReadLimit
	}
	start := max(0, len(s.activity)-maxEvents)
	events := append([]session.SessionEvent(nil), s.activity[start:]...)
	return session.ActivityChunk{Events: events, Next: session.ActivityCursor(len(s.activity))}, nil
}

func (s *fakeSession) ReadEvents(ctx context.Context, next session.EventCursor, maxEvents int) (session.EventChunk, error) {
	if err := ctx.Err(); err != nil {
		return session.EventChunk{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if maxEvents <= 0 {
		return session.EventChunk{}, session.ErrInvalidEventReadLimit
	}
	if int(next) >= len(s.events) {
		return session.EventChunk{Next: session.EventCursor(len(s.events))}, nil
	}
	end := min(len(s.events), int(next)+maxEvents)
	return session.EventChunk{Events: append([]session.Event(nil), s.events[next:end]...), Next: session.EventCursor(end)}, nil
}

func (s *fakeSession) ReadRecentEvents(maxEvents int) (session.EventChunk, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if maxEvents <= 0 {
		return session.EventChunk{}, session.ErrInvalidEventReadLimit
	}
	start := max(0, len(s.events)-maxEvents)
	return session.EventChunk{Events: append([]session.Event(nil), s.events[start:]...), Next: session.EventCursor(len(s.events))}, nil
}

func (s *fakeSession) PublishEvent(event session.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	event.ID = uint64(len(s.events))
	event.SessionID = s.id
	s.events = append(s.events, event)
}

func (s *fakeSession) Write(request session.WriteRequest) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state != session.StateOpen {
		return 0, session.ErrNotOpen
	}
	if s.writeErr != nil {
		return 0, s.writeErr
	}
	s.actors = append(s.actors, request.Actor)
	s.written = append(s.written, request.Data...)
	s.activity = append(s.activity, session.SessionEvent{Timestamp: time.Now(), Actor: request.Actor, Operation: session.OperationWrite, Data: append([]byte(nil), request.Data...)})
	return len(request.Data), nil
}

func (s *fakeSession) Resize(uint16, uint16) error { return nil }

func (s *fakeSession) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state = session.StateClosed
	return s.closeErr
}

func (s *fakeSession) writtenData() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]byte(nil), s.written...)
}

// writeActors returns the actor recorded for each fake Session write request.
func (s *fakeSession) writeActors() []session.Actor {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]session.Actor(nil), s.actors...)
}

var _ connectableSession = (*fakeSession)(nil)

// waitingSession blocks cursor reads until a test publishes an output chunk or
// closes it. It makes timeout and cancellation tests independent of a real
// serial device and the Core reader goroutine.
type waitingSession struct {
	*fakeSession
	output    chan session.OutputChunk
	closed    chan struct{}
	closeOnce sync.Once
}

func newWaitingSession(id string) *waitingSession {
	return &waitingSession{
		fakeSession: newFakeSession(id),
		output:      make(chan session.OutputChunk, 4),
		closed:      make(chan struct{}),
	}
}

func (s *waitingSession) ReadOutput(ctx context.Context, _ session.OutputCursor, _ int) (session.OutputChunk, error) {
	select {
	case chunk := <-s.output:
		return chunk, nil
	case <-s.closed:
		return session.OutputChunk{}, io.EOF
	case <-ctx.Done():
		return session.OutputChunk{}, ctx.Err()
	}
}

func (s *waitingSession) Close() error {
	s.closeOnce.Do(func() { close(s.closed) })
	return s.fakeSession.Close()
}

func (s *waitingSession) emit(chunk session.OutputChunk) {
	s.output <- chunk
}

var _ connectableSession = (*waitingSession)(nil)
