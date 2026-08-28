package command

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/akira-init1/ChannelTerm/internal/core/app"
	"github.com/akira-init1/ChannelTerm/internal/core/config"
	"github.com/akira-init1/ChannelTerm/internal/core/session"
	serialtransport "github.com/akira-init1/ChannelTerm/internal/core/transport/serial"
)

// listSources isolates read-only discovery from command parsing. List never
// opens a terminal transport; these dependencies enumerate local ports, read
// existing configuration, and query an already-running MCP HTTP service.
type listSources struct {
	configPath   func() (string, error)
	loadConfig   func(string) (config.File, error)
	listPorts    func() ([]serialtransport.Port, error)
	listProfiles func(context.Context, string) ([]app.SerialProfileInfo, error)
	listSessions func(context.Context, string) ([]mcpListedSession, error)
}

// mcpListedSession is the stable subset of terminal_list_sessions used by the
// CLI. The opaque ID remains available for scripts while Reference is the
// shorter human-facing value such as SER-1.
type mcpListedSession struct {
	ID        string `json:"session_id"`
	Reference string `json:"session_ref"`
	Transport string `json:"transport"`
	Endpoint  string `json:"endpoint"`
	Label     string `json:"label"`
	State     string `json:"state"`
}

// listItem is one connection target shown by list. When a local target also
// has a Session in the local MCP host, the MCP fields describe that Session in
// the same row instead of duplicating the endpoint.
type listItem struct {
	Reference    string `json:"reference,omitempty"`
	SessionID    string `json:"session_id,omitempty"`
	MCPReference string `json:"mcp_session_ref,omitempty"`
	MCPState     string `json:"mcp_session_state,omitempty"`
	Kind         string `json:"kind"`
	Transport    string `json:"transport"`
	Target       string `json:"target"`
	State        string `json:"state"`
	Occupancy    string `json:"occupancy"`
	Source       string `json:"source"`
	Label        string `json:"label,omitempty"`
}

// listMCPStatus reports whether the optional MCP source contributed live
// sessions. Offline is informational by default so local discovery remains
// useful when no MCP HTTP server is running.
type listMCPStatus struct {
	Endpoint string `json:"endpoint"`
	State    string `json:"state"`
	Detail   string `json:"detail,omitempty"`
}

// listReport is the machine-readable result of one list invocation.
type listReport struct {
	MCP   listMCPStatus `json:"mcp"`
	Items []listItem    `json:"items"`
}

// runList executes the production list command with only read-only sources.
func runList(ctx context.Context, args []string, output io.Writer) error {
	application, err := app.New(app.Dependencies{Manager: session.NewManager()})
	if err != nil {
		return err
	}
	return runListWithSources(ctx, args, output, listSources{
		listPorts: func() ([]serialtransport.Port, error) {
			return application.ListSerialPorts(ctx)
		},
		listProfiles: application.ListSerialProfiles,
		listSessions: listMCPSessions,
	})
}

// runListWithSources parses list flags, collects matching read-only results,
// and renders either a human table or one JSON object. It is kept independent
// from concrete devices and HTTP so tests never need to open a serial port or
// start a real server.
func runListWithSources(ctx context.Context, args []string, output io.Writer, sources listSources) error {
	if sources.listPorts == nil || sources.listSessions == nil || (sources.listProfiles == nil && (sources.configPath == nil || sources.loadConfig == nil)) {
		return errors.New("list sources must not be nil")
	}
	flags := flag.NewFlagSet("list", flag.ContinueOnError)
	flags.SetOutput(output)
	transport := flags.String("transport", "", "comma-separated transport names, for example serial")
	kind := flags.String("kind", "", "comma-separated result kinds: device, profile, or session")
	endpoint := flags.String("endpoint", defaultMCPEndpoint, "MCP Streamable HTTP endpoint for active sessions")
	configPath := flags.String("config", "", "path to an existing TOML configuration file")
	jsonOutput := flags.Bool("json", false, "write one JSON result object")
	longOutput := flags.Bool("long", false, "show full session IDs and extended table fields")
	flags.BoolVar(longOutput, "l", false, "alias for --long")
	noMCP := flags.Bool("no-mcp", false, "skip the MCP session query")
	help := flags.Bool("help", false, "show help and exit")
	shortHelp := flags.Bool("h", false, "show help and exit")
	flags.Usage = func() {
		fmt.Fprintln(output, "Usage: channelterm list [--transport NAME] [--kind KIND] [options]")
		fmt.Fprintln(output)
		fmt.Fprintln(output, "List detected local ports, saved connection profiles, and sessions from an existing MCP HTTP server.")
		fmt.Fprintln(output, "The command never opens a serial port, connects to SSH, or writes configuration.")
		fmt.Fprintln(output, "Device references such as SER-COM8 and MCP session references such as SER-1 can be passed to channelterm attach.")
		fmt.Fprintln(output)
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
		return fmt.Errorf("unexpected list argument %q", flags.Arg(0))
	}

	transports := parseListFilter(*transport)
	kinds, err := parseListKinds(*kind)
	if err != nil {
		return err
	}
	report, err := collectList(ctx, *endpoint, *configPath, *noMCP, transports, kinds, sources)
	if err != nil {
		return err
	}
	if *jsonOutput {
		encoder := json.NewEncoder(output)
		encoder.SetIndent("", "  ")
		return encoder.Encode(report)
	}
	return renderList(output, report, *longOutput)
}

// collectList gathers each selected source independently. MCP being offline is
// a normal state for a local CLI, whereas an error from an explicitly selected
// local scanner or configuration file is returned to avoid claiming a complete
// local inventory from partial data.
func collectList(ctx context.Context, endpoint, configuredPath string, noMCP bool, transports, kinds map[string]bool, sources listSources) (listReport, error) {
	report := listReport{MCP: listMCPStatus{Endpoint: endpoint, State: "skipped"}, Items: []listItem{}}
	includeDevices := allowsListKind(kinds, "device") && allowsListTransport(transports, "serial")
	includeProfiles := allowsListKind(kinds, "profile") && allowsListTransport(transports, "serial")
	includeSessions := allowsListKind(kinds, "session")

	if includeDevices {
		ports, err := sources.listPorts()
		if err != nil {
			return listReport{}, fmt.Errorf("list local serial ports: %w", err)
		}
		for _, port := range ports {
			report.Items = append(report.Items, listItem{
				Reference: serialTargetReference(port.Name),
				Kind:      "device",
				Transport: "serial",
				Target:    port.Name,
				State:     "present",
				Occupancy: "unknown",
				Source:    "local",
			})
		}
	}

	if includeProfiles {
		if sources.listProfiles != nil {
			profiles, err := sources.listProfiles(ctx, configuredPath)
			if err != nil {
				return listReport{}, err
			}
			for _, listed := range profiles {
				state := "configured"
				if strings.TrimSpace(listed.Profile.Port) == "" {
					state = "incomplete"
				}
				report.Items = append(report.Items, listItem{Reference: serialTargetReference(listed.Profile.Port), Kind: "profile", Transport: "serial", Target: listed.Profile.Port, State: state, Occupancy: "not connected", Source: "config", Label: listed.Name})
			}
		} else {
			path := configuredPath
			if path == "" {
				var err error
				path, err = sources.configPath()
				if err != nil {
					return listReport{}, fmt.Errorf("resolve list configuration path: %w", err)
				}
			}
			file, err := sources.loadConfig(path)
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				return listReport{}, fmt.Errorf("load list configuration %q: %w", path, err)
			}
			if err == nil {
				profileNames := make([]string, 0, len(file.Serial.Profiles))
				for name := range file.Serial.Profiles {
					profileNames = append(profileNames, name)
				}
				sort.Strings(profileNames)
				for _, name := range profileNames {
					profile, resolveErr := file.ResolveSerial(name)
					if resolveErr != nil {
						return listReport{}, fmt.Errorf("resolve serial profile %q: %w", name, resolveErr)
					}
					state := "configured"
					if strings.TrimSpace(profile.Port) == "" {
						state = "incomplete"
					}
					report.Items = append(report.Items, listItem{
						Reference: serialTargetReference(profile.Port),
						Kind:      "profile",
						Transport: "serial",
						Target:    profile.Port,
						State:     state,
						Occupancy: "not connected",
						Source:    "config",
						Label:     name,
					})
				}
			}
		}
	}

	if includeSessions && !noMCP {
		sessions, err := sources.listSessions(ctx, endpoint)
		if err != nil {
			report.MCP.State = "offline"
			report.MCP.Detail = err.Error()
		} else {
			report.MCP.State = "online"
			for _, listed := range sessions {
				if !allowsListTransport(transports, listed.Transport) {
					continue
				}
				report.Items = append(report.Items, listItem{
					Reference: listed.Reference,
					SessionID: listed.ID,
					Kind:      "session",
					Transport: listed.Transport,
					Target:    listed.Endpoint,
					State:     listed.State,
					Occupancy: "owned by ChannelTerm",
					Source:    "mcp",
					Label:     listed.Label,
				})
			}
		}
	}
	if noMCP {
		report.MCP.State = "disabled"
	}
	report.Items = mergeListItems(report.Items, includeDevices && includeProfiles, includeDevices || includeProfiles, isLocalMCPEndpoint(endpoint))
	sort.Slice(report.Items, func(i, j int) bool {
		if report.Items[i].Kind != report.Items[j].Kind {
			return report.Items[i].Kind < report.Items[j].Kind
		}
		if report.Items[i].Transport != report.Items[j].Transport {
			return report.Items[i].Transport < report.Items[j].Transport
		}
		if report.Items[i].Target != report.Items[j].Target {
			return report.Items[i].Target < report.Items[j].Target
		}
		return report.Items[i].Reference < report.Items[j].Reference
	})
	return report, nil
}

// mergeListItems removes duplicate representations of one endpoint only when
// the selected kinds make both sources visible. A remote MCP endpoint is not
// merged with a similarly named local port because those are different hosts.
func mergeListItems(items []listItem, mergeProfiles, mergeSessions, localMCP bool) []listItem {
	merged := make([]listItem, 0, len(items))
	targets := make(map[string]int)
	for _, item := range items {
		key := listTargetKey(item.Transport, item.Target)
		switch item.Kind {
		case "profile":
			if mergeProfiles {
				if index, ok := targets[key]; ok && merged[index].Kind == "device" {
					mergeProfileIntoTarget(&merged[index], item)
					continue
				}
			}
		case "session":
			if mergeSessions && localMCP {
				if index, ok := targets[key]; ok {
					mergeSessionIntoTarget(&merged[index], item)
					continue
				}
			}
		}
		merged = append(merged, item)
		if item.Kind != "session" && item.Target != "" {
			targets[key] = len(merged) - 1
		}
	}
	return merged
}

// mergeProfileIntoTarget retains the discoverable device as the primary row
// while preserving that saved settings exist for the endpoint.
func mergeProfileIntoTarget(target *listItem, profile listItem) {
	target.Source = combineListSource(target.Source, profile.Source)
	target.Label = combineListLabel(target.Label, profile.Label)
}

// mergeSessionIntoTarget records ChannelTerm ownership in the existing target
// row. The canonical Session ID remains JSON-only and the short Session
// reference is rendered in the dedicated MCP session column.
func mergeSessionIntoTarget(target *listItem, listed listItem) {
	target.SessionID = listed.SessionID
	target.MCPReference = listed.Reference
	target.MCPState = listed.State
	target.Occupancy = listed.Occupancy
	target.Source = combineListSource(target.Source, listed.Source)
	target.Label = combineListLabel(target.Label, listed.Label)
}

// listTargetKey uses a separator outside normal endpoint syntax so one
// transport cannot merge with another merely because their target text matches.
func listTargetKey(transport, target string) string {
	return strings.ToLower(strings.TrimSpace(transport)) + "\x00" + strings.ToLower(strings.TrimSpace(target))
}

// combineListSource forms a stable, compact source summary without repeating a
// source when an endpoint is represented by more than one input.
func combineListSource(current, added string) string {
	for _, source := range strings.Split(current, "+") {
		if source == added {
			return current
		}
	}
	if current == "" {
		return added
	}
	return current + "+" + added
}

// combineListLabel keeps distinct profile and Session labels visible after
// their endpoint rows are combined.
func combineListLabel(current, added string) string {
	if added == "" || current == added {
		return current
	}
	if current == "" {
		return added
	}
	return current + ", " + added
}

// isLocalMCPEndpoint reports whether an MCP HTTP URL identifies this computer.
// Only such servers can own the same serial port as this CLI process.
func isLocalMCPEndpoint(endpoint string) bool {
	parsed, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil {
		return false
	}
	host := parsed.Hostname()
	if strings.EqualFold(host, "localhost") {
		return true
	}
	return net.ParseIP(host) != nil && net.ParseIP(host).IsLoopback()
}

// serialTargetReference is a deterministic local target reference. It uses
// the reported endpoint rather than enumeration order so a rescan cannot make
// a previously displayed reference select a different serial port.
func serialTargetReference(port string) string {
	port = strings.TrimSpace(port)
	if port == "" {
		return ""
	}
	return "SER-" + strings.ToUpper(port)
}

// listMCPSessions uses the existing read-only terminal_list_sessions tool and
// closes its client immediately after the snapshot. It never attaches to or
// changes a remote Session.
func listMCPSessions(ctx context.Context, endpoint string) ([]mcpListedSession, error) {
	client, err := connectMCPClient(ctx, strings.TrimSpace(endpoint))
	if err != nil {
		return nil, err
	}
	defer func() { _ = client.Close() }()
	var result struct {
		Sessions []mcpListedSession `json:"sessions"`
	}
	if err := callMCPTool(ctx, client, "terminal_list_sessions", map[string]any{}, &result); err != nil {
		return nil, err
	}
	if result.Sessions == nil {
		return []mcpListedSession{}, nil
	}
	return result.Sessions, nil
}

// parseListFilter normalizes a comma-separated filter without constraining
// future transport names. A currently unsupported transport simply has no
// matching source rows instead of making the CLI reject a future protocol.
func parseListFilter(value string) map[string]bool {
	result := make(map[string]bool)
	for _, item := range strings.Split(value, ",") {
		item = strings.ToLower(strings.TrimSpace(item))
		if item != "" {
			result[item] = true
		}
	}
	return result
}

// parseListKinds limits kinds because they control which local sources run;
// silently accepting a misspelled kind could hide the inventory a user asked
// to inspect.
func parseListKinds(value string) (map[string]bool, error) {
	result := parseListFilter(value)
	for kind := range result {
		switch kind {
		case "device", "profile", "session":
		default:
			return nil, fmt.Errorf("unsupported list kind %q; use device, profile, or session", kind)
		}
	}
	return result, nil
}

// allowsListKind returns true for an empty filter or an explicitly selected kind.
func allowsListKind(filter map[string]bool, kind string) bool {
	return len(filter) == 0 || filter[kind]
}

// allowsListTransport returns true for an empty filter or an explicitly selected transport.
func allowsListTransport(filter map[string]bool, transport string) bool {
	return len(filter) == 0 || filter[strings.ToLower(strings.TrimSpace(transport))]
}

// renderList writes a compact, copy-friendly table by default. longOutput
// includes opaque Session IDs and extended metadata; JSON always retains every
// available field for scripts regardless of this presentation choice.
func renderList(output io.Writer, report listReport, longOutput bool) error {
	if report.MCP.Detail == "" {
		if _, err := fmt.Fprintf(output, "MCP: %s (%s)\n", report.MCP.State, report.MCP.Endpoint); err != nil {
			return err
		}
	} else if _, err := fmt.Fprintf(output, "MCP: %s (%s): %s\n", report.MCP.State, report.MCP.Endpoint, report.MCP.Detail); err != nil {
		return err
	}
	if len(report.Items) == 0 {
		_, err := fmt.Fprintln(output, "No matching connections, devices, or profiles.")
		return err
	}
	table := tabwriter.NewWriter(output, 0, 0, 2, ' ', 0)
	header := "REF\tKIND\tTRANSPORT\tTARGET\tSTATE\tOCCUPANCY\tMCP SESSION\tSOURCE\tLABEL"
	if longOutput {
		header = "REF\tKIND\tTRANSPORT\tTARGET\tSTATE\tOCCUPANCY\tMCP SESSION\tSESSION ID\tSOURCE\tLABEL"
	}
	if _, err := fmt.Fprintln(table, header); err != nil {
		return err
	}
	for _, item := range report.Items {
		reference := item.Reference
		if reference == "" {
			reference = "-"
		}
		target := item.Target
		if target == "" {
			target = "-"
		}
		mcpSession := "-"
		if item.MCPReference != "" {
			mcpSession = item.MCPReference + " (" + item.MCPState + ")"
		}
		if longOutput {
			sessionID := item.SessionID
			if sessionID == "" {
				sessionID = "-"
			}
			if _, err := fmt.Fprintf(table, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", reference, item.Kind, item.Transport, target, item.State, item.Occupancy, mcpSession, sessionID, item.Source, item.Label); err != nil {
				return err
			}
			continue
		}
		if _, err := fmt.Fprintf(table, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", reference, item.Kind, item.Transport, target, item.State, item.Occupancy, mcpSession, item.Source, item.Label); err != nil {
			return err
		}
	}
	return table.Flush()
}
