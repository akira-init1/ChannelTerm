package mcp

import (
	"github.com/akira-init1/ChannelTerm/internal/core/app"
	"github.com/akira-init1/ChannelTerm/internal/core/connectionpolicy"
	"github.com/akira-init1/ChannelTerm/internal/core/device"
	"github.com/akira-init1/ChannelTerm/internal/core/session"
)

// jsonSchema is the recursive JSON Schema subset required to describe every
// structured ChannelTerm Tool result exposed over MCP.
type jsonSchema struct {
	Type       string                `json:"type"`
	Properties map[string]jsonSchema `json:"properties,omitempty"`
	Required   []string              `json:"required,omitempty"`
	Items      *jsonSchema           `json:"items,omitempty"`
	Enum       []string              `json:"enum,omitempty"`
	Format     string                `json:"format,omitempty"`
}

// terminalOutputSchema returns the public result contract for one exposed MCP
// name. Aliases intentionally share the same result schema as their target.
func terminalOutputSchema(name string) (jsonSchema, bool) {
	var schema jsonSchema
	switch name {
	case "terminal_list_serial_ports":
		schema = objectSchema(map[string]jsonSchema{"ports": arraySchema(serialPortSchema())}, "ports")
	case "terminal_list_devices":
		schema = objectSchema(map[string]jsonSchema{"devices": arraySchema(deviceSchema())}, "devices")
	case "terminal_read_device_events", "terminal_wait_device_event":
		schema = cursorChunkSchema(deviceEventSchema())
	case "terminal_get_connection_decision":
		schema = connectionDecisionSchema()
	case "terminal_open_serial":
		schema = objectSchema(map[string]jsonSchema{
			"session_id":  stringSchema(),
			"session_ref": stringSchema(),
			"reused":      booleanSchema(),
		}, "session_id", "session_ref", "reused")
	case "terminal_list_sessions":
		schema = objectSchema(map[string]jsonSchema{"sessions": arraySchema(sessionSummarySchema())}, "sessions")
	case "terminal_read", "terminal_wait":
		schema = objectSchema(map[string]jsonSchema{
			"data":       stringSchema(),
			"encoding":   stringSchema("utf8", "hex", "base64"),
			"bytes_read": integerSchema(),
			"next":       integerSchema(),
			"dropped":    booleanSchema(),
		}, "data", "encoding", "bytes_read", "next", "dropped")
	case "terminal_read_activity", "terminal_wait_activity":
		schema = cursorChunkSchema(activityEventSchema())
	case "terminal_session_events":
		schema = cursorChunkSchema(sessionEventSchema())
	case "terminal_wait_file_transfer":
		schema = fileTransferResultSchema()
	case "terminal_session_attach":
		schema = objectSchema(map[string]jsonSchema{"attached": booleanSchema()}, "attached")
	case "terminal_session_detach":
		schema = objectSchema(map[string]jsonSchema{"detached": booleanSchema()}, "detached")
	case "terminal_report_file_transfer":
		schema = objectSchema(map[string]jsonSchema{"published": booleanSchema()}, "published")
	case "terminal_exec":
		schema = objectSchema(map[string]jsonSchema{
			"session_id":   stringSchema(),
			"command_id":   stringSchema(),
			"exit_code":    integerSchema(),
			"output_start": integerSchema(),
			"output_end":   integerSchema(),
		}, "session_id", "command_id", "exit_code", "output_start", "output_end")
	case "terminal_write", "terminal_write_leased":
		schema = objectSchema(map[string]jsonSchema{"bytes_written": integerSchema()}, "bytes_written")
	case "terminal_acquire_lease", "terminal_renew_lease":
		schema = leaseSchema()
	case "terminal_begin_file_transfer_cancel":
		schema = objectSchema(map[string]jsonSchema{
			"active":     booleanSchema(),
			"request_id": stringSchema(),
			"state":      stringSchema("inactive", "confirming"),
		}, "active", "request_id", "state")
	case "terminal_resolve_file_transfer_cancel":
		schema = objectSchema(map[string]jsonSchema{
			"state":       stringSchema("resumed", "cancelled", "completed", "expired"),
			"transferred": integerSchema(),
			"total":       integerSchema(),
			"percent":     numberSchema(),
		}, "state", "transferred", "total", "percent")
	case "terminal_file_transfer_checkpoint":
		schema = objectSchema(map[string]jsonSchema{
			"action": stringSchema(string(app.FileTransferContinue), string(app.FileTransferCancel)),
		}, "action")
	case "terminal_release_lease":
		schema = objectSchema(map[string]jsonSchema{"released": booleanSchema()}, "released")
	case "terminal_close":
		schema = objectSchema(map[string]jsonSchema{
			"session_id":  stringSchema(),
			"session_ref": stringSchema(),
			"closed":      booleanSchema(),
		}, "session_id", "session_ref", "closed")
	default:
		return jsonSchema{}, false
	}
	return schema, true
}

func serialPortSchema() jsonSchema {
	return objectSchema(map[string]jsonSchema{
		"name":         stringSchema(),
		"vid":          stringSchema(),
		"pid":          stringSchema(),
		"usb_serial":   stringSchema(),
		"manufacturer": stringSchema(),
		"product":      stringSchema(),
		"usb_path":     stringSchema(),
	}, "name", "vid", "pid", "usb_serial", "manufacturer", "product", "usb_path")
}

func deviceSchema() jsonSchema {
	return objectSchema(map[string]jsonSchema{
		"device_id":       stringSchema(),
		"identity_method": stringSchema("usb_serial", "usb_path", "runtime"),
		"persistent":      booleanSchema(),
		"transport":       stringSchema(),
		"endpoint":        stringSchema(),
		"state":           stringSchema(string(device.StatePresent)),
		"vid":             stringSchema(),
		"pid":             stringSchema(),
		"usb_serial":      stringSchema(),
		"manufacturer":    stringSchema(),
		"product":         stringSchema(),
		"usb_path":        stringSchema(),
		"first_seen":      dateTimeSchema(),
		"last_seen":       dateTimeSchema(),
	}, "device_id", "identity_method", "persistent", "transport", "endpoint", "state", "vid", "pid", "usb_serial", "manufacturer", "product", "usb_path", "first_seen", "last_seen")
}

func deviceEventSchema() jsonSchema {
	return objectSchema(map[string]jsonSchema{
		"timestamp": dateTimeSchema(),
		"type":      stringSchema(string(device.EventAppeared), string(device.EventDisappeared)),
		"transport": stringSchema(),
		"endpoint":  stringSchema(),
	}, "timestamp", "type", "transport", "endpoint")
}

func connectionDecisionSchema() jsonSchema {
	return objectSchema(map[string]jsonSchema{
		"transport":   stringSchema(),
		"endpoint":    stringSchema(),
		"present":     booleanSchema(),
		"connected":   booleanSchema(),
		"policy":      stringSchema(string(connectionpolicy.PolicyAsk), string(connectionpolicy.PolicyAuto), string(connectionpolicy.PolicyDeny)),
		"action":      stringSchema(string(connectionpolicy.ActionNone), string(connectionpolicy.ActionAsk), string(connectionpolicy.ActionConnect), string(connectionpolicy.ActionDeny)),
		"reason":      stringSchema("device_not_present", "already_connected"),
		"session_id":  stringSchema(),
		"session_ref": stringSchema(),
	}, "transport", "endpoint", "present", "connected", "policy", "action")
}

func sessionSummarySchema() jsonSchema {
	return objectSchema(map[string]jsonSchema{
		"session_id":  stringSchema(),
		"session_ref": stringSchema(),
		"transport":   stringSchema(),
		"endpoint":    stringSchema(),
		"label":       stringSchema(),
		"state":       stringSchema("new", "connecting", "open", "closing", "closed", "failed"),
		"lease":       leaseSummarySchema(),
	}, "session_id", "session_ref", "transport", "endpoint", "label", "state")
}

func leaseSummarySchema() jsonSchema {
	return objectSchema(map[string]jsonSchema{
		"type":        leaseTypeSchema(),
		"transfer_id": stringSchema(),
		"created_at":  dateTimeSchema(),
		"expires_at":  dateTimeSchema(),
		"state":       stringSchema("active"),
	}, "type", "created_at", "expires_at", "state")
}

func activityEventSchema() jsonSchema {
	return objectSchema(map[string]jsonSchema{
		"timestamp": dateTimeSchema(),
		"actor":     stringSchema(string(session.ActorUser), string(session.ActorAgent), string(session.ActorSystem)),
		"operation": stringSchema(string(session.OperationWrite)),
		"data":      stringSchema(),
		"encoding":  stringSchema("base64"),
	}, "timestamp", "actor", "operation", "data", "encoding")
}

func sessionEventSchema() jsonSchema {
	return objectSchema(map[string]jsonSchema{
		"id":         integerSchema(),
		"timestamp":  dateTimeSchema(),
		"session_id": stringSchema(),
		"type": stringSchema(
			string(session.EventSessionCreated),
			string(session.EventSessionAttached),
			string(session.EventSessionDetached),
			string(session.EventLeaseAcquired),
			string(session.EventLeaseReleased),
			string(session.EventFileTransferStarted),
			string(session.EventFileTransferProgress),
			string(session.EventFileTransferCompleted),
			string(session.EventFileTransferCancelled),
			string(session.EventFileTransferFailed),
			string(session.EventTerminalCommandStarted),
			string(session.EventTerminalCommandOutputStarted),
			string(session.EventTerminalCommandCompleted),
			string(session.EventTerminalCommandFailed),
		),
		"actor":    stringSchema(),
		"metadata": objectSchema(nil),
	}, "id", "timestamp", "session_id", "type", "actor")
}

func cursorChunkSchema(event jsonSchema) jsonSchema {
	return objectSchema(map[string]jsonSchema{
		"events":  arraySchema(event),
		"next":    integerSchema(),
		"dropped": booleanSchema(),
	}, "events", "next", "dropped")
}

func fileTransferResultSchema() jsonSchema {
	return objectSchema(map[string]jsonSchema{
		"state":          stringSchema("completed", "cancelled", "failed"),
		"transfer_id":    stringSchema(),
		"event":          sessionEventSchema(),
		"next":           integerSchema(),
		"dropped":        booleanSchema(),
		"lease_released": booleanSchema(),
		"source_path":    stringSchema(),
		"requested_path": stringSchema(),
		"resolved_path":  stringSchema(),
		"renamed":        booleanSchema(),
		"sha256":         stringSchema(),
	}, "state", "transfer_id", "event", "next", "dropped", "lease_released")
}

func leaseSchema() jsonSchema {
	return objectSchema(map[string]jsonSchema{
		"session_id":  stringSchema(),
		"type":        leaseTypeSchema(),
		"transfer_id": stringSchema(),
		"created_at":  dateTimeSchema(),
		"expires_at":  dateTimeSchema(),
		"state":       stringSchema("active"),
	}, "session_id", "type", "created_at", "expires_at", "state")
}

func leaseTypeSchema() jsonSchema {
	return stringSchema(string(app.LeaseTypeTerminal), string(app.LeaseTypeFileTransfer), string(app.LeaseTypeDebug))
}

func objectSchema(properties map[string]jsonSchema, required ...string) jsonSchema {
	return jsonSchema{Type: "object", Properties: properties, Required: required}
}

func arraySchema(items jsonSchema) jsonSchema {
	return jsonSchema{Type: "array", Items: &items}
}

func stringSchema(values ...string) jsonSchema {
	return jsonSchema{Type: "string", Enum: values}
}

func dateTimeSchema() jsonSchema {
	return jsonSchema{Type: "string", Format: "date-time"}
}

func integerSchema() jsonSchema { return jsonSchema{Type: "integer"} }

func numberSchema() jsonSchema { return jsonSchema{Type: "number"} }

func booleanSchema() jsonSchema { return jsonSchema{Type: "boolean"} }
