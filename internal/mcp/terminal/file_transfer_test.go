package terminal

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/akira-init1/ChannelTerm/internal/core/app"
	"github.com/akira-init1/ChannelTerm/internal/core/session"
)

func TestFileTransferToolRejectsInvalidInputAndEnforcesWrites(t *testing.T) {
	manager := session.NewManager()
	defer manager.Close()
	a, err := app.New(app.Dependencies{Manager: manager})
	if err != nil {
		t.Fatal(err)
	}
	tool := authorizedTool{Tool: &fileTransferTool{application: a}}
	for _, input := range []string{`null`, `{}`, `{"action":"unknown"}`, `{"action":"status","transfer_id":"x","sources":["a"]}`, `{"action":"cancel","transfer_id":"x","wait_ms":30001}`, `{"action":"start","unexpected":true}`, `{"action":"status"} {}`} {
		if _, err := tool.Call(context.Background(), json.RawMessage(input)); err == nil {
			t.Fatalf("accepted %s", input)
		}
	}
	ctx := app.WithClientAccess(context.Background(), app.ClientAccess{})
	if _, err := tool.Call(ctx, json.RawMessage(`{"action":"start"}`)); !errors.Is(err, app.ErrClientPermissionDenied) {
		t.Fatal(err)
	}
	if _, err := tool.Call(ctx, json.RawMessage(`{"action":"cancel","transfer_id":"x"}`)); !errors.Is(err, app.ErrClientPermissionDenied) {
		t.Fatal(err)
	}
	if _, err := tool.Call(ctx, json.RawMessage(`{"action":"status","transfer_id":"x"}`)); errors.Is(err, app.ErrClientPermissionDenied) {
		t.Fatal("read-only status denied")
	}
}
