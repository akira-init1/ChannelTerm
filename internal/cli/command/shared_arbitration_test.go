package command

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/core/session"
	mcpadapter "github.com/akira-init1/ChannelTerm/internal/mcp"
	protocol "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Exercise the production registry composition through separate HTTP clients,
// including the foreground SSH input adapter. The Channel is fake; no serial
// device, SSH server, installed Host, or GUI is involved.
func TestSharedArbitrationAcrossCLIAndMCP(t *testing.T) {
	for _, adapter := range []struct {
		name string
		open func(context.Context, string, string) (attachSession, error)
	}{
		{"attach", newMCPAttachSession},
		{"ssh-owner", newSSHOwnerAttachment},
	} {
		t.Run(adapter.name, func(t *testing.T) {
			host := newAttachTestHost(t)
			defer host.close()
			otherDevice := registerArbitrationSession(t, host.manager, "other")
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			cli, err := adapter.open(ctx, host.server.URL, "board")
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = cli.Close() }()
			var attached *mcpAttachSession
			switch value := cli.(type) {
			case *mcpAttachSession:
				attached = value
			case *sshOwnerAttachment:
				attached = value.mcpAttachSession
			default:
				t.Fatalf("unexpected CLI adapter %T", cli)
			}
			agent, closeAgent := newAttachTestAgent(t, host.server.URL)
			defer closeAgent()
			call := func(name string, arguments map[string]any, destination any) {
				t.Helper()
				if err := callMCPTool(ctx, agent, name, arguments, destination); err != nil {
					t.Fatalf("%s: %v", name, err)
				}
			}
			leaseArgs := map[string]any{"session_id": "board", "owner": "agent", "type": "file-transfer"}
			if err := attached.AcquireFileTransferLease(ctx); err != nil {
				t.Fatal(err)
			}
			assertSharedLease(t, ctx, attached.client, agent, attached.transferID)
			for _, name := range []string{"terminal_write", "terminal_write_leased", "terminal_acquire_lease", "terminal_renew_lease", "terminal_release_lease"} {
				args := arbitrationArguments(name, "board", "agent")
				assertArbitrationToolError(t, ctx, agent, name, args, "locked by file-transfer", "not owned by caller")
			}
			if _, err := cli.Write(session.WriteRequest{Actor: session.ActorUser, Data: []byte("cli-owned")}); err != nil {
				t.Fatal(err)
			}
			if err := attached.RenewFileTransferLease(ctx); err != nil {
				t.Fatal(err)
			}
			assertSharedLease(t, ctx, attached.client, agent, attached.transferID)

			// A lease restricts writes to one Session, not independent readers or
			// operations on another Session managed by the same Host.
			host.device.emit([]byte("output"))
			chunk, err := cli.ReadOutput(ctx, 0, 128)
			if err != nil || string(chunk.Data) != "output" {
				t.Fatalf("CLI read during lease = %q, %v", chunk.Data, err)
			}
			var output struct{ Data string }
			call("terminal_read", map[string]any{"session_id": "board", "cursor": 0, "encoding": "utf8"}, &output)
			if output.Data != "output" {
				t.Fatalf("MCP read during lease = %q", output.Data)
			}
			call("terminal_write", map[string]any{"session_id": "other", "data": "independent\n"}, nil)
			otherLease := map[string]any{"session_id": "other", "owner": "agent", "type": "terminal"}
			call("terminal_acquire_lease", otherLease, nil)
			call("terminal_release_lease", arbitrationArguments("terminal_release_lease", "other", "agent"), nil)
			if got := string(otherDevice.waitWritten(t)); got != "independent\n" {
				t.Fatalf("other Session bytes = %q", got)
			}

			if err := attached.ReleaseFileTransferLease(ctx); err != nil {
				t.Fatal(err)
			}
			assertSharedLease(t, ctx, attached.client, agent, "")
			call("terminal_write", map[string]any{"session_id": "board", "data": "agent-normal\n"}, nil)

			// Reverse ownership: an independently connected MCP client can block
			// CLI input, and client-local token state cannot grant write access.
			var acquired struct {
				TransferID string `json:"transfer_id"`
			}
			call("terminal_acquire_lease", leaseArgs, &acquired)
			delete(leaseArgs, "type")
			assertSharedLease(t, ctx, attached.client, agent, acquired.TransferID)
			for _, localOwner := range []string{"", "forged-owner"} {
				attached.leaseOwner = localOwner
				wantError := "locked by file-transfer"
				if localOwner != "" {
					wantError = "not owned by caller"
				}
				if n, err := cli.Write(session.WriteRequest{Actor: session.ActorUser, Data: []byte("rejected")}); n != 0 || err == nil || !strings.Contains(err.Error(), wantError) {
					t.Fatalf("CLI owner %q write = %d, %v", localOwner, n, err)
				}
			}
			attached.leaseOwner = ""
			call("terminal_renew_lease", leaseArgs, nil)
			assertSharedLease(t, ctx, attached.client, agent, acquired.TransferID)
			call("terminal_write_leased", map[string]any{"session_id": "board", "owner": "agent", "data": "agent-owned"}, nil)
			call("terminal_release_lease", leaseArgs, nil)
			assertSharedLease(t, ctx, attached.client, agent, "")
			if _, err := cli.Write(session.WriteRequest{Actor: session.ActorUser, Data: []byte("cli-normal\n")}); err != nil {
				t.Fatal(err)
			}
			want := "cli-ownedagent-normal\nagent-ownedcli-normal\n"
			if got := string(host.device.waitWrittenLength(t, len(want))); got != want {
				t.Fatalf("written bytes = %q, want %q (no rejected input replay)", got, want)
			}

			// Exercise task A's CLI recovery-abort entry, without pretending to
			// simulate device-side shell recovery in this adapter integration test.
			if err := attached.AcquireFileTransferLease(ctx); err != nil {
				t.Fatal(err)
			}
			original, _ := host.manager.Get("board")
			if err := attached.AbortFileTransferRecovery(ctx); err != nil {
				t.Fatal(err)
			}
			if original.State() != session.StateClosed {
				t.Fatalf("recovery abort left Session %s", original.State())
			}
			for _, name := range []string{"terminal_write", "terminal_write_leased", "terminal_acquire_lease", "terminal_renew_lease"} {
				assertArbitrationToolError(t, ctx, agent, name, arbitrationArguments(name, "board", "agent"), "not found")
			}
			if _, err := cli.Write(session.WriteRequest{Actor: session.ActorUser, Data: []byte("stale-owner")}); err == nil {
				t.Fatal("CLI retained write access after recovery abort")
			}
			if got := string(host.device.waitWrittenLength(t, len(want))); got != want {
				t.Fatalf("recovery abort admitted additional bytes: %q", got)
			}
			call("terminal_write", map[string]any{"session_id": "other", "data": "after-close"}, nil)
		})
	}
}

// In-memory MCP transports let virtual time expire the real Host lease without
// shortening its production TTL or waiting on real network timers. Both clients
// still cross the MCP server and the production CLI registry constructor.
func TestSharedArbitrationAcrossMCPClientsAfterLeaseExpiry(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		manager := session.NewManager()
		defer func() { _ = manager.Close() }()
		device := registerArbitrationSession(t, manager, "board")
		otherDevice := registerArbitrationSession(t, manager, "other")
		original, _ := manager.Get("board")
		registry, err := newMCPRegistry(manager, nil)
		if err != nil {
			t.Fatal(err)
		}
		server, err := mcpadapter.NewServer(registry, "test")
		if err != nil {
			t.Fatal(err)
		}
		ctx := context.Background()
		clients := make([]*protocol.ClientSession, 2)
		for i := range clients {
			serverTransport, clientTransport := protocol.NewInMemoryTransports()
			serverSession, err := server.Connect(ctx, serverTransport, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = serverSession.Close() }()
			client := protocol.NewClient(&protocol.Implementation{Name: "arbitration-test", Version: "test"}, nil)
			clients[i], err = client.Connect(ctx, clientTransport, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = clients[i].Close() }()
		}
		var lease struct {
			ExpiresAt time.Time `json:"expires_at"`
		}
		args := map[string]any{"session_id": "board", "owner": "owner", "type": "file-transfer"}
		if err := callMCPTool(ctx, clients[0], "terminal_acquire_lease", args, &lease); err != nil {
			t.Fatal(err)
		}
		if !lease.ExpiresAt.After(time.Now()) {
			t.Fatal("lease did not return a future expiry")
		}
		assertArbitrationToolError(t, ctx, clients[1], "terminal_write", map[string]any{"session_id": "board", "data": "blocked"}, "locked by file-transfer")
		time.Sleep(time.Until(lease.ExpiresAt) + time.Nanosecond)
		synctest.Wait()
		if original.State() != session.StateClosed {
			t.Fatalf("expired Session state = %s", original.State())
		}
		for _, client := range clients {
			for _, name := range []string{"terminal_write", "terminal_write_leased", "terminal_acquire_lease", "terminal_renew_lease"} {
				assertArbitrationToolError(t, ctx, client, name, arbitrationArguments(name, "board", "owner"), "not found")
			}
		}
		device.mu.Lock()
		written := string(device.written)
		device.mu.Unlock()
		if written != "" {
			t.Fatalf("expired Session received bytes: %q", written)
		}
		if err := callMCPTool(ctx, clients[1], "terminal_write", map[string]any{"session_id": "other", "data": "still-independent"}, nil); err != nil {
			t.Fatal(err)
		}
		if got := string(otherDevice.waitWritten(t)); got != "still-independent" {
			t.Fatalf("other Session bytes = %q", got)
		}
	})
}

func registerArbitrationSession(t *testing.T, manager *session.Manager, id string) *attachTestTransport {
	t.Helper()
	device := newAttachTestTransport()
	terminal, err := session.New(id, device)
	if err != nil {
		t.Fatal(err)
	}
	if err := terminal.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := manager.Register(terminal); err != nil {
		_ = terminal.Close()
		t.Fatal(err)
	}
	return device
}

// Compare the Host's complete public lease summaries, not adapter-local tokens.
func assertSharedLease(t *testing.T, ctx context.Context, first, second *protocol.ClientSession, transferID string) {
	t.Helper()
	var leases [2]map[string]any
	for i, client := range []*protocol.ClientSession{first, second} {
		var result struct {
			Sessions []struct {
				ID    string         `json:"session_id"`
				Lease map[string]any `json:"lease"`
			} `json:"sessions"`
		}
		if err := callMCPTool(ctx, client, "terminal_list_sessions", map[string]any{}, &result); err != nil {
			t.Fatal(err)
		}
		found := false
		for _, item := range result.Sessions {
			if item.ID == "board" {
				found = true
				leases[i] = item.Lease
			}
		}
		if !found {
			t.Fatal("list_sessions omitted board")
		}
	}
	if !reflect.DeepEqual(leases[0], leases[1]) {
		t.Fatalf("clients see different leases: %v / %v", leases[0], leases[1])
	}
	if transferID == "" {
		if leases[0] != nil {
			t.Fatalf("released lease still visible: %v", leases[0])
		}
	} else if leases[0]["transfer_id"] != transferID || leases[0]["state"] != "active" || leases[0]["type"] != "file-transfer" {
		t.Fatalf("Host lease = %v, want active transfer %q", leases[0], transferID)
	}
}

func assertArbitrationToolError(t *testing.T, ctx context.Context, client *protocol.ClientSession, name string, args map[string]any, messages ...string) {
	t.Helper()
	result, err := client.CallTool(ctx, &protocol.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s transport failed instead of returning a Host decision: %v", name, err)
	}
	if !result.IsError {
		t.Fatalf("%s unexpectedly allowed: %#v", name, result)
	}
	for _, message := range messages {
		if strings.Contains(resultMessage(result), message) {
			return
		}
	}
	t.Fatalf("%s diagnostic = %q, want one of %q", name, resultMessage(result), messages)
}

// Keep negative tests valid against each tool's strict input schema, so rejection
// proves a Host arbitration decision rather than malformed client arguments.
func arbitrationArguments(name, id, owner string) map[string]any {
	args := map[string]any{"session_id": id}
	if name != "terminal_write" {
		args["owner"] = owner
	}
	switch name {
	case "terminal_write", "terminal_write_leased":
		args["data"] = "rejected"
	case "terminal_acquire_lease":
		args["type"] = "file-transfer"
	}
	return args
}
