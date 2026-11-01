package command

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/core/app"
	"github.com/akira-init1/ChannelTerm/internal/core/session"
	mcpadapter "github.com/akira-init1/ChannelTerm/internal/mcp"
)

func TestGDBUnifiedCLIUsesSameHostSnapshotAndPreservesLegacyJSON(t *testing.T) {
	m := session.NewManager()
	defer m.Close()
	a, _ := app.New(app.Dependencies{Manager: m})
	registry, err := newMCPRegistryForApplication(a)
	if err != nil {
		t.Fatal(err)
	}
	h, err := mcpadapter.NewStreamableHTTPHandler(registry, "test")
	if err != nil {
		t.Fatal(err)
	}
	auth, token := registeredTestHandler(t, h)
	t.Setenv(httpAuthTokenEnvVar, token)
	s := httptest.NewServer(auth)
	defer s.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client, _, err := connectMCPClient(ctx, s.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	owner := strings.Repeat("fake-owner", 4)
	var registered struct {
		Operation app.DebugObservation `json:"operation"`
	}
	if err := callMCPTool(ctx, client, "debug_observation_register", map[string]any{"owner": owner, "request_id": "fixture", "name": "Network target", "mode": "tcp", "executor": "mi", "ports": []app.DebugPort{{Role: "target", Configured: "[::1]:4000"}}}, &registered); err != nil {
		t.Fatal(err)
	}
	if err := callMCPTool(ctx, client, "debug_observation_report", map[string]any{"owner": owner, "id": registered.Operation.ID, "sequence": 1, "connection": "connected", "mi": `*stopped,reason="breakpoint-hit"`, "ports": []app.DebugPort{{Role: "target", Address: "[::1]:4000"}}}, &registered); err != nil {
		t.Fatal(err)
	}
	for _, all := range []bool{false, true} {
		var output bytes.Buffer
		args := []string{"--endpoint", s.URL, "--json"}
		if all {
			args = append(args, "--all")
		}
		if err := runGDBList(ctx, args, &output, listMCPBridges); err != nil {
			t.Fatal(err)
		}
		var result map[string]json.RawMessage
		if err := json.Unmarshal(output.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if all {
			var items []app.DebugObservation
			if err := json.Unmarshal(result["operations"], &items); err != nil {
				t.Fatal(err)
			}
			if len(items) != 1 || items[0].ID != registered.Operation.ID || items[0].TargetState != "stopped" || items[0].SessionID != "" {
				t.Fatal(items)
			}
			if result["bridges"] != nil {
				t.Fatal("mixed legacy envelope")
			}
		} else if len(result) != 2 || string(result["bridges"]) != "[]" {
			t.Fatal(output.String())
		}
		if strings.Contains(output.String(), owner) {
			t.Fatal("capability leak")
		}
	}
	var table bytes.Buffer
	if err := runGDBList(ctx, []string{"--all", "--endpoint", s.URL}, &table, listMCPBridges); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Network target", "stopped", "client_report", "[::1]:4000", registered.Operation.ID} {
		if !strings.Contains(table.String(), want) {
			t.Fatal("missing", want, table.String())
		}
	}
}
