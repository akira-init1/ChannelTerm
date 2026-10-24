// Command host runs a disposable loopback MCP Host for Electron integration tests.
// It composes the real Application, Session, Channel, and MCP layers with an
// in-memory echo endpoint; it never discovers hardware or loads user credentials.
package main

import (
	"bufio"
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/core/app"
	"github.com/akira-init1/ChannelTerm/internal/core/channel"
	"github.com/akira-init1/ChannelTerm/internal/core/session"
	"github.com/akira-init1/ChannelTerm/internal/core/tool"
	"github.com/akira-init1/ChannelTerm/internal/mcp"
	"github.com/akira-init1/ChannelTerm/internal/mcp/terminal"
)

// echoTransport hands one in-memory endpoint to the real Channel and Session.
type echoTransport struct{ stream net.Conn }

// Connect transfers the fixture stream to an established Core Channel.
func (t echoTransport) Connect(context.Context) (channel.Channel, error) {
	return channel.NewStream(t.stream)
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	token := os.Getenv("CHANNELTERM_HTTP_AUTH_TOKEN")
	if token == "" {
		return fmt.Errorf("test Host requires an explicit disposable token")
	}
	manager := session.NewManager()
	defer manager.Close()
	application, err := app.New(app.Dependencies{Manager: manager})
	if err != nil {
		return err
	}
	local, remote := net.Pipe()
	defer remote.Close()
	core, err := session.New("electron-fixture", echoTransport{local}, session.WithReceiveBufferCapacity(1024))
	if err != nil {
		return err
	}
	if err := core.Connect(context.Background()); err != nil {
		return err
	}
	if err := manager.RegisterWithMetadata(core, session.SessionMetadata{Transport: "serial", Endpoint: "in-memory", Label: "Isolated echo"}); err != nil {
		return err
	}
	var output sync.Mutex
	writeOutput := func(data []byte) {
		output.Lock()
		defer output.Unlock()
		_, _ = remote.Write(data)
	}
	go func() {
		writeOutput([]byte("ChannelTerm isolated Host ready\r\nfixture> "))
		buffer := make([]byte, 65536)
		for {
			n, err := remote.Read(buffer)
			if n > 0 {
				writeOutput(buffer[:n])
			}
			if err != nil {
				return
			}
		}
	}()
	registry := tool.NewRegistry()
	registered, err := terminal.NewTools(application)
	if err != nil {
		return err
	}
	for _, current := range registered {
		if err := registry.Register(current); err != nil {
			return err
		}
	}
	handler, err := mcp.NewStreamableHTTPHandler(registry, "electron-fixture")
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	server := &http.Server{ReadHeaderTimeout: 5 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+token)) != 1 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.URL.Path != "/mcp" {
			http.NotFound(w, r)
			return
		}
		handler.ServeHTTP(w, r)
	})}
	defer server.Close()
	go func() { _ = server.Serve(listener) }()
	encoder := json.NewEncoder(os.Stdout)
	if err := encoder.Encode(map[string]string{"endpoint": "http://" + listener.Addr().String() + "/mcp"}); err != nil {
		return err
	}
	// Test controls travel over the fixture's private stdin, not a product API.
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var command struct {
			Action string `json:"action"`
			Data   string `json:"data"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &command); err != nil {
			return err
		}
		var actionErr error
		switch command.Action {
		case "lease":
			_, actionErr = application.AcquireLease(core.ID(), "fixture-owner", app.LeaseTypeTerminal)
		case "release":
			actionErr = application.ReleaseLease(core.ID(), "fixture-owner")
		case "flood":
			writeOutput([]byte(strings.Repeat("x", 4096) + "\r\nretained-after-overwrite\r\n"))
		case "output":
			writeOutput([]byte(command.Data))
		case "close":
			actionErr = core.Close()
		case "stop":
			return nil
		default:
			actionErr = fmt.Errorf("unknown fixture action")
		}
		message := ""
		if actionErr != nil {
			message = actionErr.Error()
		}
		if err := encoder.Encode(map[string]any{"action": command.Action, "error": message}); err != nil {
			return err
		}
	}
	if err := scanner.Err(); err != nil && err != io.EOF {
		return err
	}
	return nil
}
