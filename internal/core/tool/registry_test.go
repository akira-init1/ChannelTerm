package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestRegistryRegisterGetListAndCall(t *testing.T) {
	registry := NewRegistry()
	alpha := &fakeTool{name: "terminal_alpha"}
	beta := &fakeTool{name: "terminal_beta"}
	if err := registry.Register(beta); err != nil {
		t.Fatalf("Register() beta error = %v", err)
	}
	if err := registry.Register(alpha); err != nil {
		t.Fatalf("Register() alpha error = %v", err)
	}

	got, ok := registry.Get("terminal_alpha")
	if !ok || got != alpha {
		t.Errorf("Get() = (%v, %t), want alpha and true", got, ok)
	}
	if _, ok := registry.Get("terminal_missing"); ok {
		t.Error("Get() found an unregistered tool")
	}

	listed := registry.List()
	if len(listed) != 2 || listed[0].Name() != "terminal_alpha" || listed[1].Name() != "terminal_beta" {
		t.Errorf("List() = %#v, want name-sorted alpha and beta", listed)
	}

	result, err := registry.Call(context.Background(), "terminal_alpha", json.RawMessage(`{"value":"ready"}`))
	if err != nil {
		t.Fatalf("Call() error = %v", err)
	}
	if got := result["input"]; got != `{"value":"ready"}` {
		t.Errorf("Call() result input = %v, want original JSON", got)
	}
}

func TestRegistryRejectsDuplicateAndMissingTools(t *testing.T) {
	registry := NewRegistry()
	registered := &fakeTool{name: "terminal_read"}
	if err := registry.Register(registered); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if err := registry.Register(&fakeTool{name: "terminal_read"}); !errors.Is(err, ErrDuplicateTool) {
		t.Errorf("duplicate Register() error = %v, want ErrDuplicateTool", err)
	}
	if _, err := registry.Call(context.Background(), "terminal_missing", nil); !errors.Is(err, ErrToolNotFound) {
		t.Errorf("Call() missing error = %v, want ErrToolNotFound", err)
	}
	if err := registry.Register(nil); !errors.Is(err, ErrNilTool) {
		t.Errorf("Register(nil) error = %v, want ErrNilTool", err)
	}
	if err := registry.Register(&fakeTool{}); !errors.Is(err, ErrInvalidToolName) {
		t.Errorf("Register(empty name) error = %v, want ErrInvalidToolName", err)
	}
}

func TestRegistryCallHonorsCancelledContext(t *testing.T) {
	registry := NewRegistry()
	called := false
	if err := registry.Register(&fakeTool{name: "terminal_read", called: &called}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := registry.Call(ctx, "terminal_read", nil); !errors.Is(err, context.Canceled) {
		t.Errorf("Call() error = %v, want context.Canceled", err)
	}
	if called {
		t.Error("Call() invoked the tool after context cancellation")
	}
}

func TestRegistryConcurrentAccess(t *testing.T) {
	registry := NewRegistry()
	const toolCount = 64

	var wait sync.WaitGroup
	errs := make(chan error, toolCount*2)
	for i := range toolCount {
		wait.Add(1)
		go func(i int) {
			defer wait.Done()
			name := fmt.Sprintf("terminal_test_%d", i)
			if err := registry.Register(&fakeTool{name: name}); err != nil {
				errs <- fmt.Errorf("Register(%q): %w", name, err)
				return
			}
			if _, err := registry.Call(context.Background(), name, nil); err != nil {
				errs <- fmt.Errorf("Call(%q): %w", name, err)
			}
			_ = registry.List()
			_, _ = registry.Get(name)
		}(i)
	}
	wait.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	if got := len(registry.List()); got != toolCount {
		t.Errorf("List() length = %d, want %d", got, toolCount)
	}
}

// fakeTool is a small deterministic Tool used to exercise Registry semantics.
type fakeTool struct {
	name   string
	called *bool
}

func (t *fakeTool) Name() string        { return t.name }
func (t *fakeTool) Description() string { return "test tool" }
func (t *fakeTool) InputSchema() InputSchema {
	return InputSchema{Type: "object"}
}
func (t *fakeTool) Call(ctx context.Context, input json.RawMessage) (Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if t.called != nil {
		*t.called = true
	}
	return Result{"input": string(input)}, nil
}
