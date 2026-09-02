// Package terminal exposes Session operations as protocol-neutral Tools.
//
// Serial tools translate structured Tool input to application use cases. They
// retain no transport objects or lifecycle orchestration; the shared Session
// Manager remains the owner of active sessions through internal/core/app.
package terminal

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/akira-init1/ChannelTerm/internal/core/app"
	"github.com/akira-init1/ChannelTerm/internal/core/config"
	"github.com/akira-init1/ChannelTerm/internal/core/connectionpolicy"
	"github.com/akira-init1/ChannelTerm/internal/core/device"
	"github.com/akira-init1/ChannelTerm/internal/core/session"
	"github.com/akira-init1/ChannelTerm/internal/core/tool"
	serialtransport "github.com/akira-init1/ChannelTerm/internal/core/transport/serial"
)

const maxWaitTimeout = 24 * time.Hour

var (
	// ErrNilSessionManager is returned when serial tools have no Session owner.
	ErrNilSessionManager = app.ErrNilSessionManager
	// ErrSessionIDRequired is returned when a session operation omits its ID.
	ErrSessionIDRequired = errors.New("session_id is required")
	// ErrSessionNotFound is returned when no active Session has the supplied ID.
	ErrSessionNotFound = app.ErrSessionNotFound
	// ErrInvalidEncoding is returned when a tool requests an unsupported byte encoding.
	ErrInvalidEncoding = errors.New("encoding must be utf8, hex, or base64")
	// ErrInvalidUTF8Output is returned instead of silently replacing invalid terminal bytes in a text response.
	ErrInvalidUTF8Output = errors.New("terminal output is not valid UTF-8; use hex or base64 encoding")
	// ErrInvalidWaitTimeout is returned when timeout_ms is zero or negative.
	ErrInvalidWaitTimeout = errors.New("timeout_ms must be positive")
	// ErrWaitTimeoutTooLarge is returned when timeout_ms exceeds the accepted bound.
	ErrWaitTimeoutTooLarge = errors.New("timeout_ms exceeds the maximum of 24 hours")
	// ErrTimeoutRequiresCursor is returned when timeout_ms is sent to terminal_read without a cursor wait.
	ErrTimeoutRequiresCursor = errors.New("timeout_ms requires a cursor")
	// ErrNilDeviceRegistry is returned when device tools have no discovery state owner.
	ErrNilDeviceRegistry = errors.New("device registry must not be nil")
	// ErrInvalidSessionLabel is returned when a display label contains a control character.
	ErrInvalidSessionLabel = errors.New("session label must not contain control characters")
)

// NewSerialTools creates the terminal tools backed by manager.
//
// The returned tools use the standard ChannelTerm configuration path and
// normal Serial Transport construction. manager owns every Session after a
// successful terminal_open_serial call and should be closed during application
// shutdown to release any remaining terminal resources.
func NewSerialTools(manager *session.Manager) ([]tool.Tool, error) {
	application, err := app.New(app.Dependencies{Manager: manager})
	if err != nil {
		return nil, err
	}
	return serialToolsForApplication(application), nil
}

// NewDeviceTools creates read-only device discovery tools backed by registry.
//
// The tools only expose Registry state and events. They do not inspect session
// state, open endpoints, create Sessions, or send bytes to any device.
func NewDeviceTools(registry *device.Registry) ([]tool.Tool, error) {
	if registry == nil {
		return nil, ErrNilDeviceRegistry
	}
	application, err := app.New(app.Dependencies{Manager: session.NewManager(), Devices: registry})
	if err != nil {
		return nil, err
	}
	return newDeviceTools(application), nil
}

// NewConnectionDecisionTools creates discovery-decision tools backed by a
// Device Registry and Session Manager.
//
// The tools only report what a client should do after discovery. They never
// open an endpoint, create a Session, or prompt a user, preserving the boundary
// between Device Registry observation and client-controlled connection setup.
func NewConnectionDecisionTools(manager *session.Manager, registry *device.Registry, policy connectionpolicy.Policy) ([]tool.Tool, error) {
	if manager == nil {
		return nil, ErrNilSessionManager
	}
	if registry == nil {
		return nil, ErrNilDeviceRegistry
	}
	if _, err := connectionpolicy.Parse(string(policy)); err != nil {
		return nil, err
	}
	application, err := app.New(app.Dependencies{Manager: manager, Devices: registry, Policy: policy})
	if err != nil {
		return nil, err
	}
	return newConnectionDecisionTools(application), nil
}

// NewTools creates every MCP terminal adapter over one shared Application.
// Tool names, schemas, and JSON result fields remain protocol compatibility
// concerns of this package; all terminal business operations use Application.
func NewTools(application *app.Application) ([]tool.Tool, error) {
	if application == nil {
		return nil, app.ErrNilApplicationManager
	}
	tools := serialToolsForApplication(application)
	tools = append(tools, newDeviceTools(application)...)
	tools = append(tools, newConnectionDecisionTools(application)...)
	return tools, nil
}

type connectableSession = app.ConnectedSession
type serialSessionFactory = app.SerialSessionFactory
type serialPortLister func() ([]serialtransport.Port, error)

// serialTools contains construction dependencies shared by the six exposed
// tools. It exists to make tests use fakes without making runtime dependency
// injection part of the public Tool API.
type serialTools struct {
	application *app.Application
}

// deviceTools holds the independent Device Registry dependency for discovery
// tools. Keeping it separate from serialTools preserves the boundary between
// endpoint presence and explicit terminal Session creation.
type deviceTools struct {
	application *app.Application
}

// connectionDecisionTools keeps the independent discovery and session views
// together only for the read-only decision operation. It owns neither resource.
type connectionDecisionTools struct {
	application *app.Application
}

func newSerialTools(manager *session.Manager, serviceDependencies app.SerialDependencies, listPorts serialPortLister) ([]tool.Tool, error) {
	if listPorts == nil {
		return nil, errors.New("serial port lister must not be nil")
	}
	application, err := app.New(app.Dependencies{Manager: manager, Serial: serviceDependencies, ListSerialPorts: listPorts})
	if err != nil {
		return nil, err
	}
	return serialToolsForApplication(application), nil
}

func serialToolsForApplication(application *app.Application) []tool.Tool {
	dependencies := &serialTools{
		application: application,
	}
	return []tool.Tool{
		&openSerialTool{serialTools: dependencies},
		&listSerialPortsTool{serialTools: dependencies},
		&listSessionsTool{serialTools: dependencies},
		&readTool{serialTools: dependencies},
		&readActivityTool{serialTools: dependencies},
		&writeTool{serialTools: dependencies},
		&writeLeasedTool{serialTools: dependencies},
		&acquireLeaseTool{serialTools: dependencies},
		&releaseLeaseTool{serialTools: dependencies},
		&closeTool{serialTools: dependencies},
	}
}

func newDeviceTools(application *app.Application) []tool.Tool {
	dependencies := &deviceTools{application: application}
	return []tool.Tool{&listDevicesTool{deviceTools: dependencies}, &readDeviceEventsTool{deviceTools: dependencies}}
}

func newConnectionDecisionTools(application *app.Application) []tool.Tool {
	dependencies := &connectionDecisionTools{application: application}
	return []tool.Tool{&connectionDecisionTool{connectionDecisionTools: dependencies}}
}

type listSerialPortsTool struct{ *serialTools }

type listDevicesTool struct{ *deviceTools }

// Name returns the stable terminal_list_devices Tool identifier.
func (*listDevicesTool) Name() string { return "terminal_list_devices" }

// Description explains that this Tool returns discovery state only.
func (*listDevicesTool) Description() string {
	return "List currently present devices detected by ChannelTerm without opening them or creating sessions."
}

// InputSchema describes the empty object accepted by this read-only Tool.
func (*listDevicesTool) InputSchema() tool.InputSchema { return tool.InputSchema{Type: "object"} }

// Call returns current Registry records. Presence is independent from whether
// any endpoint is currently open through Session Manager.
func (t *listDevicesTool) Call(ctx context.Context, _ json.RawMessage) (tool.Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	devices, err := t.application.ListDevices(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]deviceResult, 0, len(devices))
	for _, discovered := range devices {
		result = append(result, deviceResult{
			DeviceID:       discovered.DeviceID,
			IdentityMethod: string(discovered.IdentityMethod),
			Persistent:     discovered.Persistent,
			Transport:      discovered.Transport,
			Endpoint:       discovered.Endpoint,
			State:          string(discovered.State),
			VID:            discovered.Metadata.VID,
			PID:            discovered.Metadata.PID,
			USBSerial:      discovered.Metadata.USBSerial,
			Manufacturer:   discovered.Metadata.Manufacturer,
			Product:        discovered.Metadata.Product,
			USBPath:        discovered.Metadata.USBPath,
			FirstSeen:      discovered.FirstSeen.Format(time.RFC3339Nano),
			LastSeen:       discovered.LastSeen.Format(time.RFC3339Nano),
		})
	}
	return tool.Result{"devices": result}, nil
}

type readDeviceEventsTool struct{ *deviceTools }

// Name returns the stable terminal_read_device_events Tool identifier.
func (*readDeviceEventsTool) Name() string { return "terminal_read_device_events" }

// Description explains that cursor reads wait for discovery transitions.
func (*readDeviceEventsTool) Description() string {
	return "Read retained device appearance and disappearance events, or wait for events after a supplied cursor. After an appeared event, call terminal_get_connection_decision before opening a serial session."
}

// InputSchema describes bounded device-event reads and optional cursor waiting.
func (*readDeviceEventsTool) InputSchema() tool.InputSchema {
	return tool.InputSchema{
		Type: "object",
		Properties: map[string]tool.InputProperty{
			"cursor":     {Type: "integer", Description: "Optional next device event cursor; when set, wait for newer events."},
			"max_events": {Type: "integer", Description: "Maximum number of device events to return."},
			"timeout_ms": {Type: "integer", Description: "Optional wait timeout in milliseconds when cursor is supplied; maximum 86400000."},
		},
	}
}

// Call returns retained discovery events or waits for an event newer than Cursor.
func (t *readDeviceEventsTool) Call(ctx context.Context, input json.RawMessage) (tool.Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var args deviceEventsInput
	if err := decodeInput(input, &args); err != nil {
		return nil, err
	}
	limit := args.MaxEvents
	if limit == 0 {
		limit = 1024
	}
	var chunk device.EventChunk
	var err error
	if args.Cursor == nil {
		if args.TimeoutMS != nil {
			return nil, ErrTimeoutRequiresCursor
		}
		chunk, err = t.application.ReadDeviceEvents(ctx, nil, limit)
	} else {
		waitCtx, cancel, timeoutErr := waitContext(ctx, args.TimeoutMS)
		if timeoutErr != nil {
			return nil, timeoutErr
		}
		defer cancel()
		chunk, err = t.application.ReadDeviceEvents(waitCtx, args.Cursor, limit)
	}
	if err != nil {
		return nil, fmt.Errorf("read device events: %w", err)
	}
	events := make([]deviceEventResult, 0, len(chunk.Events))
	for _, event := range chunk.Events {
		events = append(events, deviceEventResult{
			Timestamp: event.Timestamp.Format(time.RFC3339Nano),
			Type:      string(event.Type),
			Transport: event.Transport,
			Endpoint:  event.Endpoint,
		})
	}
	return tool.Result{"events": events, "next": uint64(chunk.Next), "dropped": chunk.Dropped}, nil
}

type connectionDecisionTool struct{ *connectionDecisionTools }

// Name returns the stable terminal_get_connection_decision Tool identifier.
func (*connectionDecisionTool) Name() string { return "terminal_get_connection_decision" }

// Description explains the required Agent workflow after a device appears.
func (*connectionDecisionTool) Description() string {
	return "Evaluate the configured response to a discovered endpoint. After an appeared device event, call this before terminal_open_serial: action ask requires user approval, connect permits client-controlled opening, deny requires no action, and none requires no action. This tool never opens a session."
}

// InputSchema describes the runtime endpoint whose discovery state is evaluated.
func (*connectionDecisionTool) InputSchema() tool.InputSchema {
	return tool.InputSchema{
		Type: "object",
		Properties: map[string]tool.InputProperty{
			"transport": {Type: "string", Description: "Transport family reported by device discovery, for example serial."},
			"endpoint":  {Type: "string", Description: "Runtime endpoint reported by device discovery, for example COM6."},
		},
		Required: []string{"transport", "endpoint"},
	}
}

// Call returns a discovery-driven action without changing device or Session
// state. Active New, Connecting, Open, and Closing Sessions suppress a second
// connection, while Failed and Closed Sessions intentionally do not.
func (t *connectionDecisionTool) Call(ctx context.Context, input json.RawMessage) (tool.Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var args connectionDecisionInput
	if err := decodeInput(input, &args); err != nil {
		return nil, err
	}
	transport := strings.TrimSpace(args.Transport)
	endpoint := strings.TrimSpace(args.Endpoint)
	if transport == "" {
		return nil, errors.New("connection decision transport is required")
	}
	if endpoint == "" {
		return nil, errors.New("connection decision endpoint is required")
	}
	decision, err := t.application.ConnectionDecision(ctx, transport, endpoint)
	if err != nil {
		return nil, err
	}
	result := tool.Result{
		"transport": decision.Transport,
		"endpoint":  decision.Endpoint,
		"present":   decision.Present,
		"connected": decision.Connected,
		"policy":    string(decision.Policy),
		"action":    string(decision.Action),
	}
	if !decision.Present {
		result["reason"] = "device_not_present"
	} else if decision.Connected {
		result["session_id"] = decision.SessionID
		result["session_ref"] = decision.SessionReference
		result["reason"] = "already_connected"
	}
	return result, nil
}

// devicePresent performs an exact runtime identity match. Discovery is process
// local, so labels and persistent-device heuristics are deliberately excluded.
func devicePresent(devices []device.Device, transport, endpoint string) bool {
	for _, discovered := range devices {
		if discovered.Transport == transport && discovered.Endpoint == endpoint && discovered.State == device.StatePresent {
			return true
		}
	}
	return false
}

// activeSessionForEndpoint finds a deterministic active Session for the exact
// transport and endpoint pair. Metadata labels are presentation-only, and a
// Failed or Closed Session must not permanently prevent a retry.
func activeSessionForEndpoint(infos []session.SessionInfo, transport, endpoint string) (string, bool) {
	var id string
	for _, info := range infos {
		if info.Metadata.Transport != transport || info.Metadata.Endpoint != endpoint || !isActiveSessionState(info.State) {
			continue
		}
		if id == "" || info.ID < id {
			id = info.ID
		}
	}
	return id, id != ""
}

// isActiveSessionState identifies lifecycle stages that still own or are in the
// process of releasing an endpoint. Failed and Closed states are excluded so
// discovery can recommend another connection attempt after those lifecycles end.
func isActiveSessionState(state session.SessionState) bool {
	switch state {
	case session.StateNew, session.StateConnecting, session.StateOpen, session.StateClosing:
		return true
	default:
		return false
	}
}

// Name returns the stable terminal_list_serial_ports Tool identifier.
func (*listSerialPortsTool) Name() string { return "terminal_list_serial_ports" }

// Description explains that the Tool detects available serial devices without
// opening a port or changing Session state.
func (*listSerialPortsTool) Description() string {
	return "List serial ports currently detected by the operating system without opening them."
}

// InputSchema describes the empty object accepted by this read-only Tool.
func (*listSerialPortsTool) InputSchema() tool.InputSchema { return tool.InputSchema{Type: "object"} }

// Call returns serial devices reported by the operating system without opening
// a port, creating a Session, sending data, or reading configuration.
func (t *listSerialPortsTool) Call(ctx context.Context, _ json.RawMessage) (tool.Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ports, err := t.application.ListSerialPorts(ctx)
	if err != nil {
		return nil, fmt.Errorf("list serial ports: %w", err)
	}
	if ports == nil {
		// JSON encodes a nil slice as null. MCP callers receive an empty array
		// instead so the result shape stays stable when no device is present.
		ports = []serialtransport.Port{}
	}
	return tool.Result{"ports": ports}, nil
}

type openSerialTool struct{ *serialTools }

// Name returns the stable terminal_open_serial Tool identifier.
func (*openSerialTool) Name() string { return "terminal_open_serial" }

// Description explains that the Tool opens a configured serial terminal.
func (*openSerialTool) Description() string {
	return "Open a serial terminal session from a profile and optional connection overrides. When opening after device discovery, call this only after user approval or a terminal_get_connection_decision action of connect; explicit user-requested connections remain allowed."
}

// InputSchema describes the serial profile and optional connection overrides.
func (*openSerialTool) InputSchema() tool.InputSchema {
	return tool.InputSchema{
		Type: "object",
		Properties: map[string]tool.InputProperty{
			"profile":      {Type: "string", Description: "Optional named serial profile."},
			"config_path":  {Type: "string", Description: "Optional TOML configuration path."},
			"save":         {Type: "string", Description: "Optional profile name used to save resolved settings before opening."},
			"port":         {Type: "string", Description: "Serial port name, such as COM3 or /dev/ttyUSB0."},
			"baud":         {Type: "integer", Description: "Serial baud rate."},
			"data_bits":    {Type: "integer", Description: "Serial data bits: 5, 6, 7, or 8."},
			"parity":       {Type: "string", Description: "Serial parity.", Enum: []string{"none", "odd", "even", "mark", "space"}},
			"stop_bits":    {Type: "string", Description: "Serial stop bits.", Enum: []string{"1", "1.5", "2"}},
			"flow_control": {Type: "string", Description: "Serial flow control.", Enum: []string{"none", "software", "hardware"}},
			"wake":         {Type: "boolean", Description: "Send one carriage return after connecting."},
			"label":        {Type: "string", Description: "Optional display-only session label; it is not an identifier."},
		},
	}
}

// Call converts Tool input to the serial application use case and returns its ID.
func (t *openSerialTool) Call(ctx context.Context, input json.RawMessage) (tool.Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var args openSerialInput
	if err := decodeInput(input, &args); err != nil {
		return nil, err
	}
	if err := validateSessionLabel(args.Label); err != nil {
		return nil, err
	}

	opened, err := t.application.OpenSerial(ctx, app.OpenSerialRequest{
		Profile:    args.Profile,
		ConfigPath: args.ConfigPath,
		Overrides: config.SerialOverrides{
			Port:        args.Port,
			BaudRate:    args.Baud,
			DataBits:    args.DataBits,
			Parity:      args.Parity,
			StopBits:    args.StopBits,
			FlowControl: args.FlowControl,
			Wake:        args.Wake,
		},
		Save:  args.Save,
		Label: args.Label,
	})
	if err != nil {
		return nil, err
	}
	return tool.Result{"session_id": opened.Info.ID, "session_ref": opened.Info.Metadata.Reference, "reused": opened.Reused}, nil
}

type listSessionsTool struct{ *serialTools }

// Name returns the stable terminal_list_sessions Tool identifier.
func (*listSessionsTool) Name() string { return "terminal_list_sessions" }

// Description explains that the Tool reports active Session metadata.
func (*listSessionsTool) Description() string {
	return "List active terminal sessions with display metadata and lifecycle states."
}

// InputSchema describes the empty object accepted by this Tool.
func (*listSessionsTool) InputSchema() tool.InputSchema { return tool.InputSchema{Type: "object"} }

// Call returns the current Manager snapshot without exposing transports.
func (t *listSessionsTool) Call(ctx context.Context, _ json.RawMessage) (tool.Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	sessions := t.application.ListSessions()
	result := make([]sessionSummary, 0, len(sessions))
	for _, info := range sessions {
		summary := sessionSummary{
			ID:        info.ID,
			Reference: info.Metadata.Reference,
			Transport: info.Metadata.Transport,
			Endpoint:  info.Metadata.Endpoint,
			Label:     info.Metadata.Label,
			State:     info.State.String(),
		}
		if lease, active, err := t.application.LeaseStatus(info.ID); err == nil && active {
			summary.Lease = &leaseSummary{Type: string(lease.Type), CreatedAt: lease.CreatedAt.Format(time.RFC3339Nano), State: lease.State}
		}
		result = append(result, summary)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Reference < result[j].Reference })
	return tool.Result{"sessions": result}, nil
}

type readTool struct{ *serialTools }

// Name returns the stable terminal_read Tool identifier.
func (*readTool) Name() string { return "terminal_read" }

// Description explains that cursor reads can wait for future terminal output.
func (*readTool) Description() string {
	return "Read retained terminal output, or wait for output after a supplied cursor."
}

// InputSchema describes a session ID and optional cursor-based read controls.
func (*readTool) InputSchema() tool.InputSchema {
	return tool.InputSchema{
		Type: "object",
		Properties: map[string]tool.InputProperty{
			"session_id": {Type: "string", Description: "Session ID or short reference returned by terminal_open_serial."},
			"cursor":     {Type: "integer", Description: "Optional next output cursor; when set, wait for newer output."},
			"max_bytes":  {Type: "integer", Description: "Maximum number of output bytes to return."},
			"encoding":   {Type: "string", Description: "Output representation: utf8, hex, or base64.", Enum: []string{"utf8", "hex", "base64"}},
			"timeout_ms": {Type: "integer", Description: "Optional wait timeout in milliseconds when cursor is supplied; maximum 86400000."},
		},
		Required: []string{"session_id"},
	}
}

// Call reads a bounded terminal output chunk from the requested Session.
func (t *readTool) Call(ctx context.Context, input json.RawMessage) (tool.Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var args readInput
	if err := decodeInput(input, &args); err != nil {
		return nil, err
	}
	limit := args.MaxBytes
	if limit == 0 {
		limit = session.DefaultAIReadLimit
	}
	var chunk session.OutputChunk
	var err error
	if args.Cursor == nil {
		if args.TimeoutMS != nil {
			return nil, ErrTimeoutRequiresCursor
		}
		chunk, err = t.application.ReadSession(ctx, args.SessionID, nil, limit)
	} else {
		waitCtx, cancel, timeoutErr := waitContext(ctx, args.TimeoutMS)
		if timeoutErr != nil {
			return nil, timeoutErr
		}
		defer cancel()
		chunk, err = t.application.ReadSession(waitCtx, args.SessionID, args.Cursor, limit)
	}
	if err != nil {
		return nil, fmt.Errorf("read session %q: %w", args.SessionID, err)
	}
	data, encoding, err := encodeOutput(chunk.Data, args.Encoding)
	if err != nil {
		return nil, fmt.Errorf("encode session %q output: %w", args.SessionID, err)
	}
	return tool.Result{
		"data":       data,
		"encoding":   encoding,
		"bytes_read": len(chunk.Data),
		"next":       uint64(chunk.Next),
		"dropped":    chunk.Dropped,
	}, nil
}

type readActivityTool struct{ *serialTools }

// Name returns the stable terminal_read_activity Tool identifier.
func (*readActivityTool) Name() string { return "terminal_read_activity" }

// Description explains that activity events are separate from terminal output.
func (*readActivityTool) Description() string {
	return "Read retained Session activity events, or wait for events after an activity cursor."
}

// InputSchema describes a Session ID and bounded activity-event read controls.
func (*readActivityTool) InputSchema() tool.InputSchema {
	return tool.InputSchema{
		Type: "object",
		Properties: map[string]tool.InputProperty{
			"session_id": {Type: "string", Description: "Session ID or short reference returned by terminal_open_serial."},
			"cursor":     {Type: "integer", Description: "Optional next activity cursor; when set, wait for newer events."},
			"max_events": {Type: "integer", Description: "Maximum number of activity events to return."},
			"timeout_ms": {Type: "integer", Description: "Optional wait timeout in milliseconds when cursor is supplied; maximum 86400000."},
		},
		Required: []string{"session_id"},
	}
}

// Call returns copied activity metadata without reading terminal output or
// changing either output or activity cursors held by other consumers.
func (t *readActivityTool) Call(ctx context.Context, input json.RawMessage) (tool.Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var args activityInput
	if err := decodeInput(input, &args); err != nil {
		return nil, err
	}
	limit := args.MaxEvents
	if limit == 0 {
		limit = session.DefaultActivityBufferCapacity
	}
	var chunk session.ActivityChunk
	var err error
	if args.Cursor == nil {
		if args.TimeoutMS != nil {
			return nil, ErrTimeoutRequiresCursor
		}
		chunk, err = t.application.ReadSessionActivity(ctx, args.SessionID, nil, limit)
	} else {
		waitCtx, cancel, timeoutErr := waitContext(ctx, args.TimeoutMS)
		if timeoutErr != nil {
			return nil, timeoutErr
		}
		defer cancel()
		chunk, err = t.application.ReadSessionActivity(waitCtx, args.SessionID, args.Cursor, limit)
	}
	if err != nil {
		return nil, fmt.Errorf("read activity for session %q: %w", args.SessionID, err)
	}
	return tool.Result{
		"events":  encodeActivityEvents(chunk.Events),
		"next":    uint64(chunk.Next),
		"dropped": chunk.Dropped,
	}, nil
}

// encodeActivityEvents exposes raw event payloads as Base64. Each item carries
// its encoding explicitly so Agents never confuse invalid UTF-8 device input
// with replacement text introduced by a JSON string conversion.
func encodeActivityEvents(events []session.SessionEvent) []activityEventResult {
	encoded := make([]activityEventResult, 0, len(events))
	for _, event := range events {
		encoded = append(encoded, activityEventResult{
			Timestamp: event.Timestamp.Format(time.RFC3339Nano),
			Actor:     string(event.Actor),
			Operation: string(event.Operation),
			Data:      base64.StdEncoding.EncodeToString(event.Data),
			Encoding:  "base64",
		})
	}
	return encoded
}

type writeTool struct{ *serialTools }

// Name returns the stable terminal_write Tool identifier.
func (*writeTool) Name() string { return "terminal_write" }

// Description explains that the Tool sends lossless byte input to an active terminal session.
func (*writeTool) Description() string {
	return "Write UTF-8, hexadecimal, or base64 bytes to an active terminal session."
}

// InputSchema describes the session ID and text payload required for a write.
func (*writeTool) InputSchema() tool.InputSchema {
	return tool.InputSchema{
		Type: "object",
		Properties: map[string]tool.InputProperty{
			"session_id": {Type: "string", Description: "Session ID or short reference returned by terminal_open_serial."},
			"data":       {Type: "string", Description: "Payload to send without adding a line ending."},
			"encoding":   {Type: "string", Description: "Payload representation: utf8 (default), hex, or base64.", Enum: []string{"utf8", "hex", "base64"}},
			"actor":      {Type: "string", Description: "Internal operation source: user, agent, or system.", Enum: []string{string(session.ActorUser), string(session.ActorAgent), string(session.ActorSystem)}},
		},
		Required: []string{"session_id", "data"},
	}
}

// Call writes the complete supplied text or returns the original write error.
func (t *writeTool) Call(ctx context.Context, input json.RawMessage) (tool.Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var args writeInput
	if err := decodeInput(input, &args); err != nil {
		return nil, err
	}
	payload, err := decodePayload(args.Encoding, args.Data)
	if err != nil {
		return nil, fmt.Errorf("decode write payload: %w", err)
	}
	actor := args.Actor
	if actor == "" {
		// Existing MCP clients do not send actor. Preserve their behavior while
		// recording the conventional origin for every terminal_write call.
		actor = session.ActorAgent
	}
	if !actor.Valid() {
		return nil, fmt.Errorf("%w: %q", session.ErrInvalidActor, actor)
	}
	if _, err := t.application.WriteSession(ctx, args.SessionID, session.WriteRequest{Actor: actor, Data: payload}); err != nil {
		return nil, fmt.Errorf("write session %q: %w", args.SessionID, err)
	}
	return tool.Result{"bytes_written": len(payload)}, nil
}

// writeLeasedTool writes through a caller-owned lease without extending the
// stable terminal_write input schema used by existing MCP clients.
type writeLeasedTool struct{ *serialTools }

// Name returns the lease-aware terminal write Tool identifier.
func (*writeLeasedTool) Name() string { return "terminal_write_leased" }

// Description explains that the Tool is only for an active exclusive lease.
func (*writeLeasedTool) Description() string {
	return "Write bytes through an active Session lease owned by this operation."
}

// InputSchema describes the normal write payload plus its lease owner capability.
func (*writeLeasedTool) InputSchema() tool.InputSchema {
	schema := (&writeTool{}).InputSchema()
	schema.Properties["owner"] = tool.InputProperty{Type: "string", Description: "Opaque owner capability returned to the lease caller."}
	schema.Required = append(schema.Required, "owner")
	return schema
}

// Call writes only when owner matches the Session's active lease.
func (t *writeLeasedTool) Call(ctx context.Context, input json.RawMessage) (tool.Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var args leasedWriteInput
	if err := decodeInput(input, &args); err != nil {
		return nil, err
	}
	payload, err := decodePayload(args.Encoding, args.Data)
	if err != nil {
		return nil, fmt.Errorf("decode write payload: %w", err)
	}
	actor := args.Actor
	if actor == "" {
		actor = session.ActorAgent
	}
	if !actor.Valid() {
		return nil, fmt.Errorf("%w: %q", session.ErrInvalidActor, actor)
	}
	if _, err := t.application.WriteSessionWithLease(ctx, args.SessionID, args.Owner, session.WriteRequest{Actor: actor, Data: payload}); err != nil {
		return nil, fmt.Errorf("write leased session %q: %w", args.SessionID, err)
	}
	return tool.Result{"bytes_written": len(payload)}, nil
}

// acquireLeaseTool creates an exclusive application-level Session lease.
type acquireLeaseTool struct{ *serialTools }

// Name returns the stable lease-acquisition Tool identifier.
func (*acquireLeaseTool) Name() string { return "terminal_acquire_lease" }

// Description explains that readers remain available while other writers fail immediately.
func (*acquireLeaseTool) Description() string {
	return "Acquire one exclusive Session lease. Readers continue normally; other writers fail until release."
}

// InputSchema describes the target, opaque owner capability, and lease type.
func (*acquireLeaseTool) InputSchema() tool.InputSchema {
	return tool.InputSchema{Type: "object", Properties: map[string]tool.InputProperty{
		"session_id": {Type: "string", Description: "Session ID or short reference."},
		"owner":      {Type: "string", Description: "Opaque caller-generated lease owner capability."},
		"type":       {Type: "string", Description: "Exclusive operation type.", Enum: []string{string(app.LeaseTypeTerminal), string(app.LeaseTypeFileTransfer), string(app.LeaseTypeDebug)}},
	}, Required: []string{"session_id", "owner", "type"}}
}

// Call acquires and returns non-secret lease state. The owner capability is not echoed.
func (t *acquireLeaseTool) Call(ctx context.Context, input json.RawMessage) (tool.Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var args leaseInput
	if err := decodeInput(input, &args); err != nil {
		return nil, err
	}
	lease, err := t.application.AcquireLease(args.SessionID, args.Owner, app.LeaseType(args.Type))
	if err != nil {
		return nil, err
	}
	return leaseResult(lease), nil
}

// releaseLeaseTool releases a caller-owned exclusive Session lease.
type releaseLeaseTool struct{ *serialTools }

// Name returns the stable lease-release Tool identifier.
func (*releaseLeaseTool) Name() string { return "terminal_release_lease" }

// Description explains that release is idempotent for an already absent lease.
func (*releaseLeaseTool) Description() string {
	return "Release an exclusive Session lease owned by this operation."
}

// InputSchema describes the target and opaque owner capability.
func (*releaseLeaseTool) InputSchema() tool.InputSchema {
	return tool.InputSchema{Type: "object", Properties: map[string]tool.InputProperty{
		"session_id": {Type: "string", Description: "Session ID or short reference."},
		"owner":      {Type: "string", Description: "Opaque caller-generated lease owner capability."},
	}, Required: []string{"session_id", "owner"}}
}

// Call releases the lease after verifying the owner capability.
func (t *releaseLeaseTool) Call(ctx context.Context, input json.RawMessage) (tool.Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var args leaseInput
	if err := decodeInput(input, &args); err != nil {
		return nil, err
	}
	if err := t.application.ReleaseLease(args.SessionID, args.Owner); err != nil {
		return nil, err
	}
	return tool.Result{"released": true}, nil
}

type closeTool struct{ *serialTools }

// Name returns the stable terminal_close Tool identifier.
func (*closeTool) Name() string { return "terminal_close" }

// Description explains that the Tool closes and unregisters an active Session.
func (*closeTool) Description() string {
	return "Close an active terminal session and release its transport."
}

// InputSchema describes the session ID required to close a Session.
func (*closeTool) InputSchema() tool.InputSchema {
	return tool.InputSchema{
		Type: "object",
		Properties: map[string]tool.InputProperty{
			"session_id": {Type: "string", Description: "ID returned by terminal_open_serial."},
		},
		Required: []string{"session_id"},
	}
}

// Call delegates Manager removal and terminal cleanup to the application use case.
func (t *closeTool) Call(ctx context.Context, input json.RawMessage) (tool.Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var args closeInput
	if err := decodeInput(input, &args); err != nil {
		return nil, err
	}
	identifier := strings.TrimSpace(args.SessionID)
	if identifier == "" {
		return nil, ErrSessionIDRequired
	}
	closed, err := t.application.CloseSession(identifier)
	if err != nil {
		if errors.Is(err, app.ErrSessionNotFound) {
			return nil, fmt.Errorf("%w: %q", ErrSessionNotFound, identifier)
		}
		return nil, fmt.Errorf("close session %q: %w", identifier, err)
	}
	return tool.Result{"session_id": closed.ID, "session_ref": closed.Metadata.Reference, "closed": true}, nil
}

type openSerialInput struct {
	Profile     string  `json:"profile"`
	ConfigPath  string  `json:"config_path"`
	Save        string  `json:"save"`
	Port        *string `json:"port"`
	Baud        *int    `json:"baud"`
	DataBits    *int    `json:"data_bits"`
	Parity      *string `json:"parity"`
	StopBits    *string `json:"stop_bits"`
	FlowControl *string `json:"flow_control"`
	Wake        *bool   `json:"wake"`
	Label       string  `json:"label"`
}

type readInput struct {
	SessionID string                `json:"session_id"`
	Cursor    *session.OutputCursor `json:"cursor"`
	MaxBytes  int                   `json:"max_bytes"`
	Encoding  string                `json:"encoding"`
	TimeoutMS *int64                `json:"timeout_ms"`
}

type activityInput struct {
	SessionID string                  `json:"session_id"`
	Cursor    *session.ActivityCursor `json:"cursor"`
	MaxEvents int                     `json:"max_events"`
	TimeoutMS *int64                  `json:"timeout_ms"`
}

type deviceEventsInput struct {
	Cursor    *device.Cursor `json:"cursor"`
	MaxEvents int            `json:"max_events"`
	TimeoutMS *int64         `json:"timeout_ms"`
}

type connectionDecisionInput struct {
	Transport string `json:"transport"`
	Endpoint  string `json:"endpoint"`
}

type deviceResult struct {
	DeviceID       string `json:"device_id"`
	IdentityMethod string `json:"identity_method"`
	Persistent     bool   `json:"persistent"`
	Transport      string `json:"transport"`
	Endpoint       string `json:"endpoint"`
	State          string `json:"state"`
	VID            string `json:"vid"`
	PID            string `json:"pid"`
	USBSerial      string `json:"usb_serial"`
	Manufacturer   string `json:"manufacturer"`
	Product        string `json:"product"`
	USBPath        string `json:"usb_path"`
	FirstSeen      string `json:"first_seen"`
	LastSeen       string `json:"last_seen"`
}

type deviceEventResult struct {
	Timestamp string `json:"timestamp"`
	Type      string `json:"type"`
	Transport string `json:"transport"`
	Endpoint  string `json:"endpoint"`
}

type activityEventResult struct {
	Timestamp string `json:"timestamp"`
	Actor     string `json:"actor"`
	Operation string `json:"operation"`
	Data      string `json:"data"`
	Encoding  string `json:"encoding"`
}

type writeInput struct {
	SessionID string        `json:"session_id"`
	Data      string        `json:"data"`
	Encoding  string        `json:"encoding"`
	Actor     session.Actor `json:"actor"`
}

type leasedWriteInput struct {
	SessionID string        `json:"session_id"`
	Owner     string        `json:"owner"`
	Data      string        `json:"data"`
	Encoding  string        `json:"encoding"`
	Actor     session.Actor `json:"actor"`
}

type leaseInput struct {
	SessionID string `json:"session_id"`
	Owner     string `json:"owner"`
	Type      string `json:"type"`
}

type closeInput struct {
	SessionID string `json:"session_id"`
}

type sessionSummary struct {
	ID        string        `json:"session_id"`
	Reference string        `json:"session_ref"`
	Transport string        `json:"transport"`
	Endpoint  string        `json:"endpoint"`
	Label     string        `json:"label"`
	State     string        `json:"state"`
	Lease     *leaseSummary `json:"lease,omitempty"`
}

type leaseSummary struct {
	Type      string `json:"type"`
	CreatedAt string `json:"created_at"`
	State     string `json:"state"`
}

func leaseResult(lease app.SessionLease) tool.Result {
	return tool.Result{"session_id": lease.SessionID, "type": string(lease.Type), "created_at": lease.CreatedAt.Format(time.RFC3339Nano), "state": lease.State}
}

// validateSessionLabel keeps display-oriented metadata safe for future CLI and
// log renderers. Empty labels and duplicate printable labels are intentional;
// labels are never used to address or identify Sessions.
func validateSessionLabel(label string) error {
	for _, r := range label {
		if unicode.IsControl(r) {
			return ErrInvalidSessionLabel
		}
	}
	return nil
}

// decodeInput rejects unknown fields so tool callers receive stable validation
// errors instead of silently ignoring an option they expected to take effect.
func decodeInput(input json.RawMessage, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(input))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return fmt.Errorf("decode tool input: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("decode tool input: multiple JSON values")
		}
		return fmt.Errorf("decode tool input: %w", err)
	}
	return nil
}

// waitContext derives the optional per-call timeout without hiding parent
// cancellation. A timeout is only meaningful for cursor reads, which the
// caller validates before invoking this helper.
func waitContext(parent context.Context, timeoutMS *int64) (context.Context, context.CancelFunc, error) {
	if timeoutMS == nil {
		return parent, func() {}, nil
	}
	if *timeoutMS <= 0 {
		return nil, nil, ErrInvalidWaitTimeout
	}
	if *timeoutMS > int64(maxWaitTimeout/time.Millisecond) {
		return nil, nil, ErrWaitTimeoutTooLarge
	}
	timeout := time.Duration(*timeoutMS) * time.Millisecond
	ctx, cancel := context.WithTimeout(parent, timeout)
	return ctx, cancel, nil
}

// encodeOutput converts raw Ring Buffer bytes to the requested lossless
// representation. UTF-8 rejects malformed byte sequences rather than letting
// Go's string conversion make callers mistake replacement text for source data.
func encodeOutput(data []byte, requested string) (string, string, error) {
	encoding := strings.ToLower(strings.TrimSpace(requested))
	if encoding == "" {
		encoding = "utf8"
	}
	switch encoding {
	case "utf8":
		if !utf8.Valid(data) {
			return "", "", ErrInvalidUTF8Output
		}
		return string(data), encoding, nil
	case "hex":
		return hex.EncodeToString(data), encoding, nil
	case "base64":
		return base64.StdEncoding.EncodeToString(data), encoding, nil
	default:
		return "", "", fmt.Errorf("%w: %q", ErrInvalidEncoding, requested)
	}
}

// decodePayload validates all encoded input before Session.Write is called, so
// malformed hexadecimal or Base64 can never cause a partial terminal write.
func decodePayload(requested, data string) ([]byte, error) {
	encoding := strings.ToLower(strings.TrimSpace(requested))
	if encoding == "" {
		encoding = "utf8"
	}
	switch encoding {
	case "utf8":
		return []byte(data), nil
	case "hex":
		normalized := strings.Map(func(r rune) rune {
			if unicode.IsSpace(r) {
				return -1
			}
			return r
		}, data)
		decoded, err := hex.DecodeString(normalized)
		if err != nil {
			return nil, fmt.Errorf("decode hex payload: %w", err)
		}
		return decoded, nil
	case "base64":
		decoded, err := base64.StdEncoding.DecodeString(data)
		if err != nil {
			return nil, fmt.Errorf("decode base64 payload: %w", err)
		}
		return decoded, nil
	default:
		return nil, fmt.Errorf("%w: %q", ErrInvalidEncoding, requested)
	}
}
