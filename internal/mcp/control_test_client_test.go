package mcp

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/akira-init1/ChannelTerm/internal/core/app"
	"github.com/akira-init1/ChannelTerm/internal/core/session"
	protocol "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Test composition supplies an authenticated user; protocol tests migrate using
// the real public presence/control endpoints, never a production bypass flag.
func testControlContext(ctx context.Context) context.Context {
	return app.WithClientAccess(app.WithClientIdentity(ctx, session.ClientIdentity{ID: "CL-protocol-test", Kind: "user", Name: "Protocol test"}), app.ClientAccess{Write: true, Manage: true})
}

func configureControlTestClient(t *testing.T, c *protocol.Client) func(*protocol.ClientSession) {
	t.Helper()
	var remote *protocol.ClientSession
	var proof app.InstanceProof
	var mu sync.Mutex
	targets := make(map[string]string)
	c.AddSendingMiddleware(func(next protocol.MethodHandler) protocol.MethodHandler {
		return func(ctx context.Context, method string, req protocol.Request) (protocol.Result, error) {
			q, ok := req.GetParams().(*protocol.CallToolParams)
			if !ok || method != "tools/call" || q.Name == "client_instance" {
				return next(ctx, method, req)
			}
			mu.Lock()
			current := proof
			mu.Unlock()
			if current.ID == "" {
				return next(ctx, method, req)
			}
			if q.Meta == nil {
				q.Meta = protocol.Meta{}
			}
			q.Meta["io.channelterm/instance"] = current
			if q.Name != "terminal_control" {
				data, _ := json.Marshal(q.Arguments)
				var args struct {
					SessionID string `json:"session_id"`
					DebugID   string `json:"debug_id"`
					BridgeID  string `json:"bridge_id"`
				}
				_ = json.Unmarshal(data, &args)
				if args.SessionID == "" {
					mu.Lock()
					args.SessionID = targets[args.DebugID+args.BridgeID]
					mu.Unlock()
				}
				if args.SessionID != "" {
					result, err := remote.CallTool(ctx, &protocol.CallToolParams{Name: "terminal_control", Arguments: map[string]any{"action": "status", "session_id": args.SessionID}})
					if err == nil && !result.IsError {
						data, _ = json.Marshal(result.StructuredContent)
						var status struct {
							Control app.ControlStatus `json:"control"`
						}
						_ = json.Unmarshal(data, &status)
						q.Meta["io.channelterm/control"] = app.ControlProof{Version: status.Control.Version, OperationID: status.Control.OperationID}
					}
				}
			}
			result, err := next(ctx, method, req)
			if response, ok := result.(*protocol.CallToolResult); ok && err == nil && !response.IsError {
				data, _ := json.Marshal(response.StructuredContent)
				var decoded struct {
					Status struct {
						SessionID string `json:"session_id"`
						DebugID   string `json:"debug_id"`
						BridgeID  string `json:"bridge_id"`
					} `json:"status"`
				}
				_ = json.Unmarshal(data, &decoded)
				if decoded.Status.SessionID != "" {
					mu.Lock()
					targets[decoded.Status.DebugID+decoded.Status.BridgeID] = decoded.Status.SessionID
					mu.Unlock()
				}
			}
			return result, err
		}
	})
	return func(s *protocol.ClientSession) {
		remote = s
		result, err := s.CallTool(context.Background(), &protocol.CallToolParams{Name: "client_instance", Arguments: map[string]any{"action": "register", "name": "Protocol test", "entry": "test"}})
		if err != nil || result.IsError {
			t.Fatalf("register: %v %+v", err, result)
		}
		data, _ := json.Marshal(result.StructuredContent)
		var v struct {
			Credential app.InstanceProof `json:"credential"`
		}
		if json.Unmarshal(data, &v) != nil || v.Credential.ID == "" {
			t.Fatal("invalid registration")
		}
		mu.Lock()
		proof = v.Credential
		mu.Unlock()
	}
}

func TestDefaultControlRejectsOldAndUnknownProtocolWriters(t *testing.T) {
	for _, authenticated := range []bool{false, true} {
		t.Run(map[bool]string{false: "stdio-unknown", true: "registered-user-old-client"}[authenticated], func(t *testing.T) {
			manager, registry, _ := newTestRegistry(t)
			defer manager.Close()
			server, err := NewServer(registry, "test")
			if err != nil {
				t.Fatal(err)
			}
			serverTransport, clientTransport := protocol.NewInMemoryTransports()
			ctx := context.Background()
			if authenticated {
				ctx = testControlContext(ctx)
			}
			host, err := server.Connect(ctx, serverTransport, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer host.Close()
			client := protocol.NewClient(&protocol.Implementation{Name: "legacy", Version: "test"}, nil)
			remote, err := client.Connect(context.Background(), clientTransport, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer remote.Close()
			call := func(name string, args map[string]any, meta protocol.Meta) *protocol.CallToolResult {
				t.Helper()
				r, e := remote.CallTool(context.Background(), &protocol.CallToolParams{Name: name, Arguments: args, Meta: meta})
				if e != nil {
					t.Fatal(e)
				}
				return r
			}
			read := call("terminal_read", map[string]any{"session_id": "board"}, nil)
			if read.IsError {
				t.Fatal("legacy reader denied", read)
			}
			checkDenied := func(meta protocol.Meta) {
				t.Helper()
				r := call("terminal_write", map[string]any{"session_id": "board", "data": "forbidden", "actor": "user"}, meta)
				b, _ := json.Marshal(r.StructuredContent)
				var out struct{ Error struct{ Code string } }
				_ = json.Unmarshal(b, &out)
				if !r.IsError || out.Error.Code != "control_required" {
					t.Fatalf("missing migration error: %s", b)
				}
			}
			checkDenied(nil)
			registered := call("client_instance", map[string]any{"action": "register", "name": "writer", "entry": "cli"}, nil)
			encoded, _ := json.Marshal(registered.StructuredContent)
			var reg struct {
				Credential app.InstanceProof `json:"credential"`
			}
			_ = json.Unmarshal(encoded, &reg)
			checkDenied(protocol.Meta{"io.channelterm/instance": reg.Credential})
			status := call("terminal_control", map[string]any{"action": "status", "session_id": "board"}, nil)
			encoded, _ = json.Marshal(status.StructuredContent)
			var st struct {
				Control app.ControlStatus `json:"control"`
			}
			_ = json.Unmarshal(encoded, &st)
			r := call("terminal_write", map[string]any{"session_id": "board", "data": "valid-new-client", "actor": "agent"}, protocol.Meta{"io.channelterm/instance": reg.Credential, "io.channelterm/control": app.ControlProof{Version: st.Control.Version}})
			if r.IsError == authenticated {
				t.Fatalf("identity boundary authenticated=%v result=%+v", authenticated, r)
			}
		})
	}
}
