package command

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/akira-init1/ChannelTerm/internal/core/session"
)

// runEvents streams JSON Lines Session events from a shared MCP host. It never
// attaches a terminal, opens a Transport, or reads terminal output bytes.
func runEvents(ctx context.Context, args []string, output io.Writer) error {
	flags := flag.NewFlagSet("events", flag.ContinueOnError)
	flags.SetOutput(output)
	endpoint := flags.String("endpoint", defaultMCPEndpoint, "Session Host MCP endpoint")
	maxEvents := flags.Int("max-events", 128, "maximum events returned per host read")
	help := flags.Bool("help", false, "show help and exit")
	shortHelp := flags.Bool("h", false, "show help and exit")
	flags.Usage = func() {
		fmt.Fprintln(output, "Usage: channelterm events SESSION [--endpoint URL] [--max-events N]")
		fmt.Fprintln(output)
		fmt.Fprintln(output, "Stream structured Session events as JSON Lines. Terminal output bytes are never included.")
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
	if flags.NArg() != 1 {
		return ErrAttachSessionIDRequired
	}
	if *maxEvents <= 0 {
		return errors.New("max-events must be positive")
	}
	remote, err := connectMCPClient(ctx, strings.TrimSpace(*endpoint))
	if err != nil {
		return fmt.Errorf("connect MCP endpoint %q: %w", *endpoint, err)
	}
	defer remote.Close()

	identifier := flags.Arg(0)
	encoder := json.NewEncoder(output)
	var cursor *session.EventCursor
	for {
		arguments := map[string]any{"session_id": identifier, "max_events": *maxEvents}
		if cursor != nil {
			arguments["cursor"] = uint64(*cursor)
		}
		var result struct {
			Events  []session.Event `json:"events"`
			Next    uint64          `json:"next"`
			Dropped bool            `json:"dropped"`
		}
		if err := callMCPTool(ctx, remote, "terminal_session_events", arguments, &result); err != nil {
			if errors.Is(err, context.Canceled) {
				return nil
			}
			return fmt.Errorf("read Session events for %q: %w", identifier, err)
		}
		if result.Dropped {
			if err := encoder.Encode(map[string]any{"dropped": true, "next": result.Next}); err != nil {
				return err
			}
		}
		for _, event := range result.Events {
			if err := encoder.Encode(event); err != nil {
				return err
			}
		}
		next := session.EventCursor(result.Next)
		cursor = &next
	}
}
