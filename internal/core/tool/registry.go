package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
)

var (
	// ErrDuplicateTool is returned when a name is already registered.
	ErrDuplicateTool = errors.New("tool is already registered")
	// ErrToolNotFound is returned when no registered Tool has the requested name.
	ErrToolNotFound = errors.New("tool not found")
	// ErrNilTool is returned when Register receives a nil Tool.
	ErrNilTool = errors.New("tool must not be nil")
	// ErrInvalidToolName is returned when a Tool has no usable name.
	ErrInvalidToolName = errors.New("tool name must not be empty")
)

// Registry provides concurrent-safe registration and invocation of Tools.
//
// Registry holds its lock only while accessing its map. Tool.Call always runs
// after releasing that lock, so a slow or cancelled operation cannot block
// registration, discovery, or unrelated calls.
type Registry struct {
	mu    sync.RWMutex
	tools map[string]Tool
}

// NewRegistry creates an empty Registry ready for concurrent use.
func NewRegistry() *Registry {
	return &Registry{tools: make(map[string]Tool)}
}

// Register adds tool under its unique name.
//
// Register does not replace existing Tools because replacement could leave an
// in-flight caller observing an unexpected implementation or resource owner.
func (r *Registry) Register(tool Tool) error {
	if isNilTool(tool) {
		return ErrNilTool
	}
	name := strings.TrimSpace(tool.Name())
	if name == "" {
		return ErrInvalidToolName
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.tools[name]; exists {
		return fmt.Errorf("%w: %q", ErrDuplicateTool, name)
	}
	r.tools[name] = tool
	return nil
}

// Get returns the Tool registered under name and whether it exists.
func (r *Registry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	tool, ok := r.tools[name]
	return tool, ok
}

// List returns a name-sorted snapshot of all registered Tools.
//
// Returned Tools remain owned by their registrations. The deterministic order
// gives future discovery adapters stable output without constraining calls.
func (r *Registry) List() []Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	tools := make([]Tool, 0, len(r.tools))
	for _, tool := range r.tools {
		tools = append(tools, tool)
	}
	sort.Slice(tools, func(i, j int) bool { return tools[i].Name() < tools[j].Name() })
	return tools
}

// Call invokes the Tool registered under name.
//
// ctx is checked before handing control to the Tool. Errors from the Tool are
// returned unchanged so callers can retain context and underlying error chains.
func (r *Registry) Call(ctx context.Context, name string, input json.RawMessage) (Result, error) {
	tool, ok := r.Get(name)
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrToolNotFound, name)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return tool.Call(ctx, input)
}

// isNilTool also detects a Tool interface holding a typed nil pointer.
func isNilTool(tool Tool) bool {
	if tool == nil {
		return true
	}
	value := reflect.ValueOf(tool)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}
