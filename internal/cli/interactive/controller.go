// Package interactive translates local terminal bytes into remote writes and
// local escape commands.
//
// It has no dependency on a console, Session, or Transport. Callers own the
// actual writes and local presentation so the same byte-level controller can
// serve serial, SSH, and future interactive terminal clients.
package interactive

// DefaultEscapeByte is the byte conventionally emitted by Ctrl+].
const DefaultEscapeByte byte = 0x1D

// ActionKind identifies the result of processing local terminal input.
type ActionKind uint8

const (
	// ActionRemote writes Data to the connected Session without changing it.
	ActionRemote ActionKind = iota
	// ActionQuit asks the local interactive client to end its Session.
	ActionQuit
	// ActionHelp asks the local interactive client to display escape help.
	ActionHelp
	// ActionEscapePending reports that the controller has entered local escape
	// mode and is waiting for its command byte.
	ActionEscapePending
	// ActionUnknownEscape reports an unsupported byte following the escape byte.
	ActionUnknownEscape
)

// Action describes one ordered effect requested by a Controller.
//
// Data is populated for ActionRemote and contains caller-owned bytes copied by
// Controller. Command is populated for ActionUnknownEscape. ActionQuit,
// ActionHelp, and ActionEscapePending do not carry payload data.
type Action struct {
	Kind    ActionKind
	Data    []byte
	Command byte
}

// Controller maintains the escape-prefix state for one interactive input
// stream. Its zero value uses DefaultEscapeByte.
//
// Controller is intentionally not safe for concurrent use because terminal
// input must be processed in read order by one input loop.
type Controller struct {
	escapeByte byte
	pending    bool
}

// NewController creates a Controller whose local escape prefix is escapeByte.
//
// escapeByte is kept as a constructor argument so future CLI configuration can
// change the prefix without changing the state machine. A zero value uses
// DefaultEscapeByte so a Controller can be constructed from an omitted setting.
func NewController(escapeByte byte) *Controller {
	return &Controller{escapeByte: escapeByte}
}

// EscapeByte returns the configured local escape prefix.
func (c *Controller) EscapeByte() byte {
	if c.escapeByte == 0 {
		return DefaultEscapeByte
	}
	return c.escapeByte
}

// Process consumes data in input order and returns the remote and local
// actions it represents.
//
// Ctrl+C is not special here: in raw terminal mode it is byte 0x03 and is
// emitted as ActionRemote. Esc cancels local escape mode without an action.
// An unsupported command after the escape prefix is reported locally and always
// returns the controller to normal input mode.
func (c *Controller) Process(data []byte) []Action {
	var actions []Action
	remoteStart := 0
	flushRemote := func(end int) {
		if end == remoteStart {
			return
		}
		payload := append([]byte(nil), data[remoteStart:end]...)
		actions = append(actions, Action{Kind: ActionRemote, Data: payload})
	}

	for index, value := range data {
		if c.pending {
			flushRemote(index)
			switch value {
			case 'q':
				actions = append(actions, Action{Kind: ActionQuit})
			case '?':
				actions = append(actions, Action{Kind: ActionHelp})
			case ']':
				actions = append(actions, Action{Kind: ActionRemote, Data: []byte{c.EscapeByte()}})
			case 0x1B:
				// Esc cancels the local escape mode without reaching the remote Session.
			default:
				actions = append(actions, Action{Kind: ActionUnknownEscape, Command: value})
			}
			c.pending = false
			remoteStart = index + 1
			continue
		}
		if value == c.EscapeByte() {
			flushRemote(index)
			c.pending = true
			actions = append(actions, Action{Kind: ActionEscapePending})
			remoteStart = index + 1
		}
	}
	flushRemote(len(data))
	return actions
}
