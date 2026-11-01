package command

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/core/app"
	protocol "github.com/modelcontextprotocol/go-sdk/mcp"
)

func listMCPDebugOverview(ctx context.Context, endpoint string) (app.DebugOverview, error) {
	var result app.DebugOverview
	token, err := loadHTTPClientToken()
	if err != nil {
		return result, err
	}
	transport, err := newBearerTokenTransport(endpoint, token)
	if err != nil {
		return result, err
	}
	c := protocol.NewClient(&protocol.Implementation{Name: "channelterm-cli", Version: version}, nil)
	client, err := c.Connect(ctx, &protocol.StreamableClientTransport{Endpoint: endpoint, DisableStandaloneSSE: true, HTTPClient: &http.Client{Transport: &queryHTTPTransport{ctx: ctx, base: transport}}}, nil)
	if err != nil {
		return result, err
	}
	defer client.Close()
	err = callMCPTool(ctx, client, "debug_overview", map[string]any{}, &result)
	return result, err
}

func printGDBOverview(ctx context.Context, address string, asJSON bool, output io.Writer, list func(context.Context, string) (app.DebugOverview, error)) error {
	s, err := list(ctx, address)
	if err != nil {
		return fmt.Errorf("read unified GDB overview from %q: %w", address, err)
	}
	if s.Operations == nil {
		s.Operations = []app.DebugObservation{}
	}
	if asJSON {
		e := json.NewEncoder(output)
		e.SetIndent("", "  ")
		return e.Encode(struct {
			Endpoint string `json:"endpoint"`
			app.DebugOverview
		}{address, s})
	}
	if _, err := fmt.Fprintf(output, "Host: %s\nObserved: %s\n", gdbListCell(address), s.ObservedAt.Format(time.RFC3339)); err != nil {
		return err
	}
	if len(s.Operations) == 0 {
		_, err := fmt.Fprintln(output, "No registered debug operations. Unregistered processes are not discovered.")
		return err
	}
	t := tabwriter.NewWriter(output, 0, 0, 2, ' ', 0)
	fmt.Fprintln(t, "NAME\tMODE\tSESSION ID\tCONNECTION\tTARGET\tTUNNEL\tSOURCE\tCLIENT\tUPDATED\tSTALE\tOPERATION\tPORTS")
	for _, r := range s.Operations {
		ports := []string{}
		for _, p := range r.Ports {
			v := p.State
			if p.Address != "" {
				v = p.Address
			}
			if p.Configured != "" {
				v += " (configured " + p.Configured + ")"
			}
			ports = append(ports, p.Role+"["+p.Perspective+"]="+v)
		}
		fmt.Fprintf(t, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%t\t%s\t%s\n", gdbListCell(r.Name), gdbListCell(r.Mode), gdbListCell(r.SessionID), gdbListCell(r.Connection), gdbListCell(r.TargetState), gdbListCell(r.TunnelState), gdbListCell(r.Source), gdbListCell(r.Client.ID), r.UpdatedAt.Format(time.RFC3339), r.Stale, gdbListCell(r.ID), gdbListCell(strings.Join(ports, "; ")))
	}
	return t.Flush()
}
