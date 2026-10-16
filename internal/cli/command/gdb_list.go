package command

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/core/app"
	protocol "github.com/modelcontextprotocol/go-sdk/mcp"
)

// runGDBList queries one existing Host without attaching or renewing any bridge.
// The bounded query and caller cancellation affect only this observation.
func runGDBList(ctx context.Context, args []string, output io.Writer, list func(context.Context, string) ([]app.BridgeSummary, error)) error {
	flags := flag.NewFlagSet("gdb-list", flag.ContinueOnError)
	flags.SetOutput(output)
	endpoint := flags.String("endpoint", defaultMCPEndpoint, "Session Host MCP HTTP endpoint to query")
	jsonOutput := flags.Bool("json", false, "write one JSON result object")
	help := flags.Bool("help", false, "show help and exit")
	shortHelp := flags.Bool("h", false, "show help and exit")
	flags.Usage = func() {
		fmt.Fprintln(output, "Usage: channelterm gdb-list [--endpoint URL] [--json]")
		fmt.Fprintln(output)
		fmt.Fprintln(output, "List all unfinished external GDB bridges and their actual TCP ports in one existing Host.")
		fmt.Fprintln(output, "Includes disconnected bridges until finalization. Does not renew leases, connect to GDB, or scan OS ports.")
		fmt.Fprintln(output, "The query times out after 10 seconds; Host access failures return an error.")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if *help || *shortHelp {
		flags.Usage()
		return nil
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected gdb-list argument %q", flags.Arg(0))
	}
	address := strings.TrimSpace(*endpoint)
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	items, err := list(ctx, address)
	if err != nil {
		return fmt.Errorf("list GDB bridges from %q: %w", address, err)
	}
	if items == nil {
		items = []app.BridgeSummary{}
	}
	if *jsonOutput {
		encoder := json.NewEncoder(output)
		encoder.SetIndent("", "  ")
		return encoder.Encode(struct {
			Endpoint string              `json:"endpoint"`
			Bridges  []app.BridgeSummary `json:"bridges"`
		}{Endpoint: address, Bridges: items})
	}
	if _, err := fmt.Fprintf(output, "Host: %s\n", gdbListCell(address)); err != nil {
		return err
	}
	if len(items) == 0 {
		_, err := fmt.Fprintln(output, "No active external GDB bridges.")
		return err
	}
	table := tabwriter.NewWriter(output, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(table, "PORT\tADDRESS\tSESSION\tTARGET\tSTATE\tBRIDGE"); err != nil {
		return err
	}
	for _, item := range items {
		if _, err := fmt.Fprintf(table, "%d\t%s\t%s\t%s\t%s\t%s\n", item.Port, gdbListCell(item.Address), gdbListCell(item.SessionRef), gdbListCell(item.Target), gdbListCell(item.State), gdbListCell(item.BridgeID)); err != nil {
			return err
		}
	}
	return table.Flush()
}

// gdbListCell escapes control characters in Host metadata without modifying the
// JSON representation. Empty metadata can occur during concurrent finalization.
func gdbListCell(value string) string {
	if value == "" {
		return "-"
	}
	quoted := strconv.Quote(value)
	return quoted[1 : len(quoted)-1]
}

// listMCPBridges uses the Host's read-only inventory and closes only its MCP
// client. Host authentication is shared with other CLI observation commands.
func listMCPBridges(ctx context.Context, endpoint string) ([]app.BridgeSummary, error) {
	token, err := loadHTTPAuthToken()
	if err != nil {
		return nil, err
	}
	transport, err := newBearerTokenTransport(endpoint, token)
	if err != nil {
		return nil, err
	}
	// A short query must also bound SDK cancellation notifications and DELETE
	// cleanup, which can otherwise use contexts detached from the caller.
	mcpClient := protocol.NewClient(&protocol.Implementation{Name: "channelterm-cli", Version: version}, nil)
	client, err := mcpClient.Connect(ctx, &protocol.StreamableClientTransport{
		Endpoint: endpoint, DisableStandaloneSSE: true,
		HTTPClient: &http.Client{Transport: &queryHTTPTransport{ctx: ctx, base: transport}},
	}, nil)
	if err != nil {
		return nil, err
	}
	defer client.Close()
	var result struct {
		Bridges []app.BridgeSummary `json:"bridges"`
	}
	if err := callMCPTool(ctx, client, "debug_bridge_list", map[string]any{}, &result); err != nil {
		return nil, err
	}
	return result.Bridges, nil
}

// queryHTTPTransport bounds every request by both its own context and the
// enclosing query. This includes SDK cleanup requests after cancellation.
type queryHTTPTransport struct {
	ctx  context.Context
	base http.RoundTripper
}

// RoundTrip retains both cancellation sources through response-body consumption.
func (t *queryHTTPTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if err := t.ctx.Err(); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(request.Context())
	stop := context.AfterFunc(t.ctx, cancel)
	finish := func() { stop(); cancel() }
	response, err := t.base.RoundTrip(request.Clone(ctx))
	if err != nil {
		finish()
		return nil, err
	}
	// Keep cancellation active while the SDK reads the response body, then
	// release the callback when it closes the body.
	response.Body = &queryResponseBody{ReadCloser: response.Body, finish: finish}
	return response, nil
}

// queryResponseBody releases request cancellation resources after HTTP reads.
type queryResponseBody struct {
	io.ReadCloser
	finish func()
}

// Close releases the body and the query's per-request cancellation callback.
func (b *queryResponseBody) Close() error {
	defer b.finish()
	return b.ReadCloser.Close()
}
