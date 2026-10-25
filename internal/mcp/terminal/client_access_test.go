package terminal

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/akira-init1/ChannelTerm/internal/core/app"
	"github.com/akira-init1/ChannelTerm/internal/core/tool"
)

type accessProbe struct{ called bool }

func (*accessProbe) Name() string                  { return "future_unknown_operation" }
func (*accessProbe) Description() string           { return "test" }
func (*accessProbe) InputSchema() tool.InputSchema { return tool.InputSchema{Type: "object"} }
func (p *accessProbe) Call(context.Context, json.RawMessage) (tool.Result, error) {
	p.called = true
	return tool.Result{}, nil
}

func TestUnclassifiedToolsRequireManagementBeforeInvocation(t *testing.T) {
	probe := &accessProbe{}
	wrapped := authorizedTool{Tool: probe}
	for _, access := range []app.ClientAccess{{}, {Write: true}} {
		_, err := wrapped.Call(app.WithClientAccess(context.Background(), access), nil)
		if !errors.Is(err, app.ErrClientPermissionDenied) || probe.called {
			t.Fatal("unclassified side effect reached without management")
		}
	}
	if _, err := wrapped.Call(app.WithClientAccess(context.Background(), app.ClientAccess{Manage: true}), nil); err != nil || !probe.called {
		t.Fatal("management rejected")
	}
}
