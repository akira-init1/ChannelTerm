package command

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"os"
	"sync"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/core/app"
	protocol "github.com/modelcontextprotocol/go-sdk/mcp"
)

type mcpToolClient interface {
	CallTool(context.Context, *protocol.CallToolParams) (*protocol.CallToolResult, error)
	Close() error
}

// cliInstances belongs to one command invocation, not package-global process state.
// Internal connections to the same Host/credential share one instance and close
// when the invocation exits; direct test/helper callers own their connection.
type cliInstances struct {
	mu      sync.Mutex
	clients map[string]*instanceMCPClient
}
type cliInstancesKey struct{}

func (s *cliInstances) close() {
	for _, c := range s.clients {
		_ = c.finish()
	}
}

type instanceMCPClient struct {
	*protocol.ClientSession
	proof     app.InstanceProof
	scoped    bool
	temporary bool
	stop      chan struct{}
	done      chan struct{}
	once      sync.Once
	mu        sync.Mutex
	failed    bool
	monitors  map[string]string
}

func newInstanceMCPClient(ctx context.Context, remote *protocol.ClientSession, endpoint string) (*instanceMCPClient, error) {
	c := &instanceMCPClient{ClientSession: remote, stop: make(chan struct{}), done: make(chan struct{}), monitors: make(map[string]string)}
	// Capability discovery distinguishes old Hosts from registration failures. Never
	// silently downgrade provenance after a new Host rejects an offered delegation.
	listed, err := remote.ListTools(ctx, nil)
	if err != nil {
		return nil, err
	}
	found := false
	for _, t := range listed.Tools {
		found = found || t.Name == "client_instance"
	}
	delegation := os.Getenv("CHANNELTERM_CLIENT_DELEGATION")
	if delegation != "" && os.Getenv("CHANNELTERM_CLIENT_DELEGATION_ENDPOINT") != endpoint {
		return nil, errors.New("client delegation endpoint mismatch")
	}
	if !found {
		if delegation != "" {
			return nil, errors.New("Host does not support client delegation")
		}
		close(c.done)
		return c, nil
	}
	var registered struct {
		Credential app.InstanceProof `json:"credential"`
	}
	err = callMCPTool(ctx, remote, "client_instance", map[string]any{"action": "register", "name": "ChannelTerm CLI", "entry": "cli", "delegation": delegation}, &registered)
	if err != nil {
		return nil, err
	}
	if registered.Credential.ID == "" || registered.Credential.Secret == "" {
		return nil, errors.New("invalid instance registration response")
	}
	c.proof = registered.Credential
	go c.renew()
	return c, nil
}

func (c *instanceMCPClient) renew() {
	defer close(c.done)
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-c.stop:
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			err := callMCPTool(ctx, c.ClientSession, "client_instance", map[string]any{"action": "renew", "credential": c.proof}, nil)
			cancel()
			if err != nil {
				c.mu.Lock()
				c.failed = true
				c.mu.Unlock()
				return
			}
		}
	}
}

func (c *instanceMCPClient) CallTool(ctx context.Context, params *protocol.CallToolParams) (*protocol.CallToolResult, error) {
	c.mu.Lock()
	failed := c.failed
	c.mu.Unlock()
	if failed {
		return nil, errors.New("client presence renewal failed; reconnect explicitly, input was not retried")
	}
	q := *params
	if c.proof.ID != "" {
		q.Meta = protocol.Meta{}
		for k, v := range params.Meta {
			q.Meta[k] = v
		}
		q.Meta["io.channelterm/instance"] = c.proof
	}
	result, err := c.ClientSession.CallTool(ctx, &q)
	if err != nil || result.IsError || c.proof.ID == "" {
		return result, err
	}
	// Attachment monitoring is explicit and ends on detach, including during a
	// long interactive pause. Short list queries do not register Session uses.
	if params.Name == "terminal_session_attach" || params.Name == "terminal_session_detach" {
		encoded, _ := json.Marshal(params.Arguments)
		var args struct {
			SessionID string `json:"session_id"`
		}
		_ = json.Unmarshal(encoded, &args)
		c.mu.Lock()
		defer c.mu.Unlock()
		if params.Name == "terminal_session_attach" {
			var use struct {
				Use app.InstanceUse `json:"use"`
			}
			err = callMCPTool(ctx, c.ClientSession, "client_instance", map[string]any{"action": "use", "credential": c.proof, "key": "attach:" + args.SessionID, "session_id": args.SessionID, "kind": "monitor"}, &use)
			if err == nil {
				c.monitors[args.SessionID] = use.Use.ID
			}
		} else if id := c.monitors[args.SessionID]; id != "" {
			err = callMCPTool(ctx, c.ClientSession, "client_instance", map[string]any{"action": "unuse", "credential": c.proof, "use_id": id}, nil)
			delete(c.monitors, args.SessionID)
		}
	}
	return result, err
}

func (c *instanceMCPClient) Close() error {
	if c.scoped {
		return nil
	}
	return c.finish()
}

func (c *instanceMCPClient) finish() error {
	var err error
	c.once.Do(func() {
		close(c.stop)
		<-c.done
		if c.proof.ID != "" {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			_ = callMCPTool(ctx, c.ClientSession, "client_instance", map[string]any{"action": "end", "credential": c.proof}, nil)
			cancel()
		}
		err = c.ClientSession.Close()
	})
	return err
}

func instanceConnectionKey(endpoint, token string) string {
	digest := sha256.Sum256([]byte(token))
	return endpoint + string(digest[:])
}
