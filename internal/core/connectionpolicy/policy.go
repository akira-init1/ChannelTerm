// Package connectionpolicy defines the default reaction to a discovered device.
//
// Policy evaluates discovery state only. It never opens an endpoint, creates a
// Session, or asks a user for input; those actions remain the responsibility of
// the client that consumes a decision.
package connectionpolicy

import (
	"fmt"
	"strings"
)

// Policy identifies the default client reaction to a newly discovered endpoint.
type Policy string

const (
	// PolicyAsk requires the client to obtain the user's approval before opening
	// a discovered endpoint.
	PolicyAsk Policy = "ask"
	// PolicyAuto permits the client to open a discovered endpoint without asking
	// for approval first when it has sufficient connection parameters.
	PolicyAuto Policy = "auto"
	// PolicyDeny tells the client to ignore a discovered endpoint. It does not
	// restrict an explicit user-requested connection.
	PolicyDeny Policy = "deny"
)

// Action identifies the next step a client should take after evaluating a
// device-discovery event.
type Action string

const (
	// ActionNone means no discovery-driven action should be taken.
	ActionNone Action = "none"
	// ActionAsk requires the client to ask the user before opening the endpoint.
	ActionAsk Action = "ask"
	// ActionConnect permits the client to open the endpoint without a further
	// approval prompt.
	ActionConnect Action = "connect"
	// ActionDeny tells the client to ignore the discovery event without asking.
	ActionDeny Action = "deny"
)

// Default is the safe policy used when configuration does not specify one.
const Default = PolicyAsk

// Parse validates an explicitly supplied policy value.
func Parse(value string) (Policy, error) {
	switch Policy(strings.ToLower(strings.TrimSpace(value))) {
	case PolicyAsk:
		return PolicyAsk, nil
	case PolicyAuto:
		return PolicyAuto, nil
	case PolicyDeny:
		return PolicyDeny, nil
	default:
		return "", fmt.Errorf("connection default policy is invalid: %q", value)
	}
}

// FromConfig resolves an optional configuration value, using Default when the
// field is omitted. A non-empty unrecognized value is rejected rather than
// silently changing the user's intended connection behavior.
func FromConfig(value string) (Policy, error) {
	if strings.TrimSpace(value) == "" {
		return Default, nil
	}
	return Parse(value)
}

// Decide returns the discovery-driven action for one endpoint.
//
// An absent device and an endpoint with an active Session both take precedence
// over the configured policy. This prevents a second Session from being opened
// for an endpoint that is already within an active lifecycle stage.
func Decide(policy Policy, present, connected bool) Action {
	if !present || connected {
		return ActionNone
	}
	switch policy {
	case PolicyAuto:
		return ActionConnect
	case PolicyDeny:
		return ActionDeny
	default:
		return ActionAsk
	}
}
