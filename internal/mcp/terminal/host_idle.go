package terminal

import (
	"context"
	"encoding/json"

	"github.com/akira-init1/ChannelTerm/internal/core/app"
	"github.com/akira-init1/ChannelTerm/internal/core/tool"
)

type hostIdleTool struct{ application *app.Application }

func (*hostIdleTool) Name() string { return "host_idle_status" }
func (*hostIdleTool) Description() string {
	return "Read the Host's effective keep/release/exit policy, launch source, grace periods and bounded Session retention reasons. Observation never renews use or changes policy. Release requires registered client-instance usage; list polling does not retain Sessions. Closing a GUI only detaches; the Host independently reclaims idle Sessions. Released targets must be opened explicitly; no input is replayed. Exit is opt-in and requires an external start path."
}
func (*hostIdleTool) InputSchema() tool.InputSchema {
	return tool.InputSchema{Type: "object", Properties: map[string]tool.InputProperty{}}
}
func (t *hostIdleTool) Call(ctx context.Context, input json.RawMessage) (tool.Result, error) {
	var q struct{}
	if err := decodeDebug(input, &q); err != nil {
		return nil, err
	}
	status, err := t.application.HostIdlePolicy(ctx)
	return tool.Result{"policy": status}, err
}
