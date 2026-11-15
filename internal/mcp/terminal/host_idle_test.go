package terminal

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/core/app"
	"github.com/akira-init1/ChannelTerm/internal/core/session"
)

func TestHostIdleStatusIsReadOnlyAndLegacyMonitoringRequiresRegistration(t *testing.T) {
	m := session.NewManager()
	defer m.Close()
	a, err := app.New(app.Dependencies{Manager: m, Idle: app.IdlePolicy{SessionTimeout: 30 * time.Second, Origin: "desktop"}})
	if err != nil {
		t.Fatal(err)
	}
	wrapped := authorizedTool{Tool: &hostIdleTool{application: a}, application: a}
	ctx := app.ExternalControl(app.WithClientAccess(context.Background(), app.ClientAccess{}))
	result, err := wrapped.Call(ctx, json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	status := result["policy"].(app.HostIdleStatus)
	if status.Mode != "release" || status.Origin != "desktop" || status.SessionTimeoutMS != 30000 {
		t.Fatal(status)
	}
	if _, err := wrapped.Call(ctx, json.RawMessage(`{"mode":"keep"}`)); err == nil {
		t.Fatal("status accepted mutation")
	}
	if _, _, err := a.BeginClientCall(ctx, "anything", "terminal_read", false); !errors.Is(err, app.ErrIdleRegistration) {
		t.Fatal(err)
	}
}
