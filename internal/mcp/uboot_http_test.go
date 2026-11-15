package mcp

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/core/tool"
	protocol "github.com/modelcontextprotocol/go-sdk/mcp"
)

// waitingBootTool exposes cancellation at the adapter boundary without hardware.
type waitingBootTool struct{ started chan struct{} }

// Name identifies the existing operation whose legacy HTTP lifetime is repaired.
func (*waitingBootTool) Name() string { return "terminal_enter_uboot" }

// Description describes the test operation.
func (*waitingBootTool) Description() string { return "Wait for cancellation" }

// InputSchema accepts an empty test request.
func (*waitingBootTool) InputSchema() tool.InputSchema { return tool.InputSchema{Type: "object"} }

// Call waits for the request lifetime; it neither writes bytes nor owns a Session.
func (b *waitingBootTool) Call(ctx context.Context, _ json.RawMessage) (tool.Result, error) {
	b.started <- struct{}{}
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestUBootCancellationCapabilityOnlyAdvertisedByHTTPComposition(t *testing.T) {
	manager, registry, _ := newTestRegistry(t)
	defer manager.Close()
	for _, httpMode := range []bool{false, true} {
		var client *protocol.ClientSession
		var closeClient func()
		if httpMode {
			client, closeClient = connectHTTPTestClient(t, registry)
		} else {
			client, closeClient = connectTestClient(t, registry)
		}
		listed, err := client.ListTools(context.Background(), nil)
		closeClient()
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, item := range listed.Tools {
			if item.Name == "terminal_enter_uboot" {
				found = true
				value, advertised := item.Meta["io.channelterm/uboot-http-cancellation"]
				if advertised != httpMode || advertised && value != float64(1) {
					t.Fatalf("HTTP=%v capability=%v advertised=%v", httpMode, value, advertised)
				}
			}
		}
		if !found {
			t.Fatal("U-Boot tool missing")
		}
	}
}

func TestUBootHTTPContextCancellationIsRequestScoped(t *testing.T) {
	registry := tool.NewRegistry()
	boot := &waitingBootTool{started: make(chan struct{}, 2)}
	if err := registry.Register(boot); err != nil {
		t.Fatal(err)
	}
	a := &adapter{registry: registry}
	handler := a.handler(exposedTool{name: boot.Name(), target: boot.Name()})
	first, cancelFirst := context.WithCancel(context.Background())
	second, cancelSecond := context.WithCancel(context.Background())
	defer cancelFirst()
	defer cancelSecond()
	start := func(original context.Context) <-chan struct{} {
		done := make(chan struct{})
		go func() {
			// Legacy SDK detaches cancellation but preserves these request values.
			ctx := context.WithValue(context.Background(), httpRequestContextKey{}, original)
			_, _ = handler(ctx, nil)
			close(done)
		}()
		select {
		case <-boot.started:
		case <-time.After(time.Second):
			t.Fatal("operation did not start")
		}
		return done
	}
	firstDone, secondDone := start(first), start(second)
	cancelFirst()
	select {
	case <-firstDone:
	case <-time.After(time.Second):
		t.Fatal("HTTP cancellation did not reach U-Boot")
	}
	select {
	case <-secondDone:
		t.Fatal("cancelling one HTTP request cancelled another")
	default:
	}
	cancelSecond()
	select {
	case <-secondDone:
	case <-time.After(time.Second):
		t.Fatal("second HTTP cancellation did not reach U-Boot")
	}
}
