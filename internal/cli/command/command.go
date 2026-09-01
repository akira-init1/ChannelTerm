package command

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/cli/highlight"
	"github.com/akira-init1/ChannelTerm/internal/cli/interactive"
	"github.com/akira-init1/ChannelTerm/internal/cli/terminalinput"
	"github.com/akira-init1/ChannelTerm/internal/core/app"
	"github.com/akira-init1/ChannelTerm/internal/core/config"
	"github.com/akira-init1/ChannelTerm/internal/core/connectionpolicy"
	"github.com/akira-init1/ChannelTerm/internal/core/device"
	"github.com/akira-init1/ChannelTerm/internal/core/session"
	"github.com/akira-init1/ChannelTerm/internal/core/tool"
	serialtransport "github.com/akira-init1/ChannelTerm/internal/core/transport/serial"
	initmcp "github.com/akira-init1/ChannelTerm/internal/init/mcp"
	"github.com/akira-init1/ChannelTerm/internal/mcp"
	"github.com/akira-init1/ChannelTerm/internal/mcp/terminal"
	protocol "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Run routes one CLI invocation using caller-owned process I/O.
//
// ctx carries process cancellation from the Composition Root. stdin, stdout,
// and stderr remain adapter concerns; Core packages never receive them.
func Run(ctx context.Context, args []string, input io.Reader, stdout, stderr io.Writer) error {
	_ = stderr
	return runWithIO(ctx, args, input, stdout, nil)
}

// run parses a CLI invocation and writes usage text to output.
//
// args excludes the executable name. output receives help text to keep command
// behavior testable without changing process-wide standard streams. run returns
// an error only for invalid flags or an unsupported command.
func run(args []string, output io.Writer) error {
	return runWithIO(context.Background(), args, os.Stdin, output, nil)
}

// cliSession adds connection establishment to the public Session operations
// used by the CLI. Core intentionally keeps Connect off the Session interface
// because a caller needs it only while constructing a new session.
type cliSession interface {
	session.Session
	Connect(context.Context) error
}

// serialSessionFactory keeps command parsing independent from physical serial
// opening so CLI behavior can be tested without a device.
type serialSessionFactory func(serialtransport.Config) (cliSession, error)

// version identifies this CLI build in the version command output.
const version = "0.1.0"

const (
	defaultMCPListen = "127.0.0.1:37099"
	defaultMCPPath   = "/mcp"
)

// runWithIO routes CLI commands while accepting I/O and session construction as
// dependencies. This keeps parsing testable without changing process-wide
// standard streams or opening a physical serial device.
func runWithIO(ctx context.Context, args []string, input io.Reader, output io.Writer, newSession serialSessionFactory) error {
	return runWithDependencies(ctx, args, input, output, newSession, newMCPAttachSession)
}

// runWithDependencies routes CLI commands with injectable connection factories.
//
// The separate attach factory keeps tests independent from a running MCP HTTP
// server while production attach clients still use the shared MCP host.
func runWithDependencies(ctx context.Context, args []string, input io.Reader, output io.Writer, newSession serialSessionFactory, newAttach attachSessionFactory) error {
	if len(args) > 0 && args[0] == "serial" {
		return runSerial(ctx, args[1:], input, output, newSession)
	}
	if len(args) > 0 && args[0] == "connect" {
		return runApplicationConnect(ctx, args[1:], input, output, newSession)
	}
	if len(args) > 0 && args[0] == "attach" {
		return runAttach(ctx, args[1:], input, output, newAttach)
	}
	if len(args) > 0 && args[0] == "list" {
		return runList(ctx, args[1:], output)
	}
	if len(args) > 0 && args[0] == "mcp" {
		return runMCP(ctx, args[1:], output)
	}
	if len(args) > 0 && args[0] == "init" {
		return runInit(args[1:], output)
	}

	flags := flag.NewFlagSet("channelterm", flag.ContinueOnError)
	flags.SetOutput(output)
	flags.Usage = func() {
		fmt.Fprintln(output, "Usage: channelterm [options] [command]")
		fmt.Fprintln(output)
		fmt.Fprintln(output, "A shared terminal-session core for human and AI access.")
		fmt.Fprintln(output)
		fmt.Fprintln(output, "Commands:")
		fmt.Fprintln(output, "  attach  Attach to a Session hosted by local MCP HTTP")
		fmt.Fprintln(output, "  help    Show this help message")
		fmt.Fprintln(output, "  init    Configure supported MCP clients")
		fmt.Fprintln(output, "  list    List local devices, saved profiles, and MCP sessions")
		fmt.Fprintln(output, "  mcp     Start the Session Host MCP server")
		fmt.Fprintln(output, "  serial  Connect to a serial port")
		fmt.Fprintln(output, "  version Show the version")
		fmt.Fprintln(output)
		fmt.Fprintln(output, "Options:")
		flags.PrintDefaults()
	}
	help := flags.Bool("help", false, "show help and exit")
	shortHelp := flags.Bool("h", false, "show help and exit")
	showVersion := flags.Bool("version", false, "print version and exit")

	if len(args) == 1 && args[0] == "help" {
		flags.Usage()
		return nil
	}
	if len(args) == 1 && args[0] == "version" {
		printVersion(output)
		return nil
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() > 0 {
		return fmt.Errorf("unknown command %q", flags.Arg(0))
	}
	if *showVersion {
		printVersion(output)
		return nil
	}
	if *help || *shortHelp {
		flags.Usage()
		return nil
	}
	flags.Usage()
	return nil
}

// runInit discovers supported MCP clients, installs their ChannelTerm endpoint
// configuration, or prints the exact generated examples without writing files.
func runInit(args []string, output io.Writer) error {
	adapters, err := initmcp.NewAdapters(initmcp.Options{})
	if err != nil {
		return fmt.Errorf("initialize MCP client adapters: %w", err)
	}
	return runInitWithAdapters(args, output, adapters)
}

// runInitWithAdapters keeps init command tests independent from real user
// configuration while production uses the standard adapter locations.
func runInitWithAdapters(args []string, output io.Writer, adapters []initmcp.Adapter) error {
	flags := flag.NewFlagSet("init", flag.ContinueOnError)
	flags.SetOutput(output)
	install := flags.Bool("mcp", false, "detect supported MCP clients and install ChannelTerm configuration")
	show := flags.Bool("mcp-show", false, "print ChannelTerm MCP configuration examples without writing files")
	flags.Usage = func() {
		fmt.Fprintln(output, "Usage: channelterm init --mcp | --mcp-show [codex|claude|opencode|zoo]")
		fmt.Fprintln(output)
		fmt.Fprintln(output, "Install or display ChannelTerm MCP client configurations.")
		fmt.Fprintln(output)
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if *install == *show {
		return errors.New("choose exactly one of --mcp or --mcp-show")
	}
	if flags.NArg() > 1 {
		return fmt.Errorf("unexpected init argument %q", flags.Arg(1))
	}
	if *install && flags.NArg() != 0 {
		return fmt.Errorf("unexpected init argument %q", flags.Arg(0))
	}
	endpoint := initmcp.DefaultEndpoint()
	if *show {
		selected, err := selectMCPAdapters(adapters, flags.Arg(0))
		if err != nil {
			return err
		}
		for index, adapter := range selected {
			example, err := adapter.Example(endpoint)
			if err != nil {
				return fmt.Errorf("generate %s MCP configuration: %w", adapter.Name(), err)
			}
			if index > 0 {
				fmt.Fprintln(output)
			}
			fmt.Fprintf(output, "%s:\n%s", adapter.Name(), example)
		}
		return nil
	}

	detected := 0
	for _, adapter := range adapters {
		available, err := adapter.Detect()
		if err != nil {
			return fmt.Errorf("detect %s: %w", adapter.Name(), err)
		}
		if !available {
			continue
		}
		detected++
		result, err := adapter.Install(endpoint)
		if err != nil {
			return fmt.Errorf("install %s MCP configuration: %w", adapter.Name(), err)
		}
		if result.AlreadyPresent {
			fmt.Fprintf(output, "%s: ChannelTerm MCP configuration already exists (%s).\n", adapter.Name(), result.Path)
			continue
		}
		fmt.Fprintf(output, "%s: installed ChannelTerm MCP configuration (%s).\n", adapter.Name(), result.Path)
	}
	if detected == 0 {
		return errors.New("no supported MCP clients were detected")
	}
	return nil
}

// selectMCPAdapters treats an omitted identifier as a request for every
// supported format, while a non-empty identifier must resolve exactly once so
// --mcp-show cannot silently print an unexpected client configuration.
func selectMCPAdapters(adapters []initmcp.Adapter, identifier string) ([]initmcp.Adapter, error) {
	identifier = strings.ToLower(strings.TrimSpace(identifier))
	if identifier == "" {
		return adapters, nil
	}
	for _, adapter := range adapters {
		if adapter.ID() == identifier {
			return []initmcp.Adapter{adapter}, nil
		}
	}
	return nil, fmt.Errorf("unsupported MCP client %q; use codex, claude, opencode, or zoo", identifier)
}

// runMCP starts an MCP Server using the normal terminal Tool Registry.
//
// The command owns its Manager for process lifetime, while MCP adapters do not
// own Sessions. Stdio writes no status text because standard MCP clients reserve
// stdout for newline-delimited JSON-RPC; HTTP startup status is sent to stderr.
func runMCP(ctx context.Context, args []string, output io.Writer) (err error) {
	flags := flag.NewFlagSet("mcp", flag.ContinueOnError)
	flags.SetOutput(output)
	transport := flags.String("transport", "stdio", "MCP transport: stdio or http")
	listen := flags.String("listen", defaultMCPListen, "HTTP listen address when --transport=http")
	path := flags.String("path", defaultMCPPath, "HTTP endpoint path when --transport=http")
	connectionPolicy := flags.String("connection-policy", "", "Discovery policy: ask, auto, or deny")
	flags.Usage = func() {
		fmt.Fprintln(output, "Usage: channelterm mcp [--transport stdio|http] [--listen ADDRESS] [--path PATH] [--connection-policy ask|auto|deny]")
		fmt.Fprintln(output)
		fmt.Fprintln(output, "Start a Model Context Protocol server over standard I/O or Streamable HTTP.")
		fmt.Fprintln(output, "HTTP defaults to 127.0.0.1:37099/mcp and should only be exposed on a trusted network.")
		fmt.Fprintln(output)
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected mcp argument %q", flags.Arg(0))
	}
	selectedTransport := strings.ToLower(strings.TrimSpace(*transport))
	if selectedTransport != "stdio" && selectedTransport != "http" {
		return fmt.Errorf("unsupported MCP transport %q; use stdio or http", *transport)
	}
	connectionPolicySet := false
	flags.Visit(func(flag *flag.Flag) {
		if flag.Name == "connection-policy" {
			connectionPolicySet = true
		}
	})
	if connectionPolicySet {
		if _, err := connectionpolicy.Parse(*connectionPolicy); err != nil {
			return err
		}
	}
	endpointPath, err := normalizeMCPPath(*path)
	if err != nil {
		return err
	}
	configPath, err := config.DefaultPath()
	if err != nil {
		return fmt.Errorf("resolve MCP configuration path: %w", err)
	}
	file, err := config.LoadOrCreate(configPath)
	if err != nil {
		return fmt.Errorf("load MCP configuration: %w", err)
	}
	resolvedPolicy, err := resolveMCPConnectionPolicy(file, *connectionPolicy, connectionPolicySet)
	if err != nil {
		return err
	}

	manager := session.NewManager()
	defer func() {
		if closeErr := manager.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("close MCP terminal sessions: %w", closeErr)
		}
	}()
	statePath, err := config.DefaultStatePath()
	if err != nil {
		return fmt.Errorf("resolve device state path: %w", err)
	}
	store, err := device.LoadStateStore(statePath)
	if err != nil {
		return fmt.Errorf("load device state %q: %w", statePath, err)
	}
	devices, err := newSerialDeviceRegistryWithStateStore(serialtransport.ListPorts, store)
	if err != nil {
		return err
	}
	defer devices.Close()
	if err := devices.Start(ctx); err != nil {
		return fmt.Errorf("initialize device registry: %w", err)
	}
	registry, err := newMCPRegistryWithPolicy(manager, devices, resolvedPolicy)
	if err != nil {
		return err
	}
	if selectedTransport == "stdio" {
		return mcp.Run(ctx, registry, &protocol.StdioTransport{})
	}
	return runMCPHTTP(ctx, registry, *listen, endpointPath, os.Stderr)
}

// normalizeMCPPath validates the endpoint mounted for the HTTP MCP handler.
// A fixed leading slash avoids surprising relative routes in the standard HTTP
// mux and makes the printed endpoint directly usable by MCP clients.
func normalizeMCPPath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" || !strings.HasPrefix(path, "/") {
		return "", fmt.Errorf("MCP HTTP path must start with '/': %q", path)
	}
	return path, nil
}

// runMCPHTTP listens for Streamable HTTP MCP requests until ctx is cancelled.
//
// The HTTP server is stopped with a bounded graceful shutdown. The handler owns
// only temporary MCP request state; manager-owned terminal Sessions are closed
// by runMCP after this function returns.
func runMCPHTTP(ctx context.Context, registry *tool.Registry, listen, path string, stderr io.Writer) error {
	listener, err := net.Listen("tcp", listen)
	if err != nil {
		return fmt.Errorf("listen for MCP Streamable HTTP on %q: %w", listen, err)
	}
	handler, err := mcp.NewStreamableHTTPHandler(registry)
	if err != nil {
		closeErr := listener.Close()
		if closeErr != nil {
			return errors.Join(fmt.Errorf("create MCP Streamable HTTP handler: %w", err), fmt.Errorf("close MCP listener: %w", closeErr))
		}
		return fmt.Errorf("create MCP Streamable HTTP handler: %w", err)
	}
	mux := http.NewServeMux()
	mux.Handle(path, handler)
	server := &http.Server{Handler: mux}
	endpoint := httpEndpoint(listener.Addr().String(), path)
	if !isLoopbackListen(listener.Addr().String()) {
		fmt.Fprintln(stderr, "Warning: MCP server is exposed to the network.")
		fmt.Fprintln(stderr, "Remote clients may control terminal sessions.")
		fmt.Fprintln(stderr, "Use only on a trusted network.")
	}
	fmt.Fprintf(stderr, "MCP Streamable HTTP listening on %s\n", endpoint)

	serverStopped := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = server.Shutdown(shutdownCtx)
		case <-serverStopped:
		}
	}()
	err = server.Serve(listener)
	close(serverStopped)
	if errors.Is(err, http.ErrServerClosed) || errors.Is(err, net.ErrClosed) || errors.Is(ctx.Err(), context.Canceled) {
		return nil
	}
	return fmt.Errorf("serve MCP Streamable HTTP: %w", err)
}

// httpEndpoint formats a listener address and path as a client-ready HTTP URL.
func httpEndpoint(listen, path string) string {
	return "http://" + listen + path
}

// isLoopbackListen reports whether a bound listener accepts only local clients.
// The listener address, rather than the original flag value, is used so aliases
// such as localhost are judged after the operating system resolves them.
func isLoopbackListen(listen string) bool {
	host, _, err := net.SplitHostPort(listen)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// newMCPRegistry registers the existing terminal Tools once for an MCP process.
// Session and Serial construction remain entirely inside the terminal Tool set.
func newMCPRegistry(manager *session.Manager, devices *device.Registry) (*tool.Registry, error) {
	return newMCPRegistryWithPolicy(manager, devices, connectionpolicy.Default)
}

// newMCPRegistryWithPolicy registers the terminal and discovery-decision tools
// for a single MCP process. It wires an already resolved policy into the
// read-only decision tool without allowing device discovery to create Sessions.
func newMCPRegistryWithPolicy(manager *session.Manager, devices *device.Registry, policy connectionpolicy.Policy) (*tool.Registry, error) {
	registry := tool.NewRegistry()
	application, err := app.New(app.Dependencies{Manager: manager, Devices: devices, Policy: policy})
	if err != nil {
		return nil, fmt.Errorf("create MCP application: %w", err)
	}
	tools, err := terminal.NewTools(application)
	if err != nil {
		return nil, fmt.Errorf("create terminal tools: %w", err)
	}
	for _, registered := range tools {
		if err := registry.Register(registered); err != nil {
			return nil, fmt.Errorf("register terminal tool %q: %w", registered.Name(), err)
		}
	}
	return registry, nil
}

// resolveMCPConnectionPolicy applies the documented MCP precedence: an
// explicitly supplied CLI policy overrides the configured policy, and an
// omitted configuration field resolves to the safe ask default.
func resolveMCPConnectionPolicy(file config.File, override string, overrideSet bool) (connectionpolicy.Policy, error) {
	if overrideSet {
		return connectionpolicy.Parse(override)
	}
	return file.ConnectionPolicy()
}

// newSerialDeviceRegistry adapts the existing port lister to generic discovery
// without introducing a dependency from Device Registry to serial Transport.
// Enumerating devices never opens a serial port or creates a terminal Session.
func newSerialDeviceRegistry(listPorts func() ([]serialtransport.Port, error)) (*device.Registry, error) {
	return newSerialDeviceRegistryWithStore(listPorts, nil)
}

// newSerialDeviceRegistryWithStateStore gives MCP discovery persistent device
// identities while retaining the transport-neutral Registry boundary.
func newSerialDeviceRegistryWithStateStore(listPorts func() ([]serialtransport.Port, error), store *device.StateStore) (*device.Registry, error) {
	return newSerialDeviceRegistryWithStore(listPorts, store)
}

// newSerialDeviceRegistryWithStore adapts the existing port lister to generic
// discovery. A nil store is used only by unit tests that exercise discovery
// independently from persistence.
func newSerialDeviceRegistryWithStore(listPorts func() ([]serialtransport.Port, error), store *device.StateStore) (*device.Registry, error) {
	if listPorts == nil {
		return nil, errors.New("serial port lister must not be nil")
	}
	scanner := device.ScannerFunc(func(ctx context.Context) ([]device.Endpoint, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		ports, err := listPorts()
		if err != nil {
			return nil, fmt.Errorf("list serial ports: %w", err)
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		endpoints := make([]device.Endpoint, 0, len(ports))
		for _, port := range ports {
			endpoints = append(endpoints, device.Endpoint{
				Transport: "serial",
				Endpoint:  port.Name,
				Metadata: device.SerialMetadata{
					VID:          port.VID,
					PID:          port.PID,
					USBSerial:    port.USBSerial,
					Manufacturer: port.Manufacturer,
					Product:      port.Product,
					USBPath:      port.USBPath,
				},
			})
		}
		return endpoints, nil
	})
	if store == nil {
		return device.NewRegistry(scanner)
	}
	return device.NewRegistryWithStateStore(scanner, store)
}

// runSerial resolves a serial profile, establishes its Session, and bridges
// terminal input and output. It owns local console setup and cleanup but leaves
// byte transport and receive buffering to Session and Serial Transport.
func runSerial(ctx context.Context, args []string, input io.Reader, output io.Writer, newSession serialSessionFactory) (err error) {
	return runSerialWithTarget(ctx, args, input, output, newSession, "")
}

// runSerialWithTarget performs a direct serial connection and optionally
// prints targetReference after connecting. connect supplies that reference,
// while the serial command intentionally retains its established output.
func runSerialWithTarget(ctx context.Context, args []string, input io.Reader, output io.Writer, newSession serialSessionFactory, targetReference string) (err error) {
	flags := flag.NewFlagSet("serial", flag.ContinueOnError)
	flags.SetOutput(output)
	port := flags.String("port", "", "serial port name, for example COM3")
	baudRate := flags.Int("baud", 115200, "serial baud rate")
	dataBits := flags.Int("data-bits", 8, "serial data bits: 5, 6, 7, or 8")
	parity := flags.String("parity", string(serialtransport.ParityNone), "serial parity: none, odd, even, mark, or space")
	stopBits := flags.String("stop-bits", string(serialtransport.StopBitsOne), "serial stop bits: 1, 1.5, or 2")
	flowControl := flags.String("flow-control", string(serialtransport.FlowControlNone), "serial flow control: none, software, or hardware")
	wake := flags.Bool("wake", false, "send one carriage return after connecting to an already-open shell with no prompt")
	highlightMode := flags.String("highlight", "auto", "terminal highlighting: auto, always, or never")
	profileName := flags.String("profile", "", "named serial profile to use")
	configPath := flags.String("config", "", "path to a TOML configuration file")
	saveName := flags.String("save", "", "save the resolved settings as a named serial profile")
	help := flags.Bool("help", false, "show help and exit")
	shortHelp := flags.Bool("h", false, "show help and exit")
	flags.Usage = func() {
		fmt.Fprintln(output, "Usage: channelterm serial [--profile NAME] [--port PORT] [options]")
		fmt.Fprintln(output)
		fmt.Fprintln(output, "By default, connecting does not send any characters to the serial device.")
		fmt.Fprintln(output, "Use --wake when an already-open shell has no output prompt.")
		fmt.Fprintln(output, "During a connection, Ctrl+] t toggles local shell-prompt timestamps.")
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
		return fmt.Errorf("unexpected serial argument %q", flags.Arg(0))
	}
	renderer, err := resolveHighlightRenderer(*highlightMode, output)
	if err != nil {
		return err
	}
	var outputMu sync.Mutex
	writeLocalOutput := func(data []byte) error {
		outputMu.Lock()
		defer outputMu.Unlock()
		return writeAll(output, data)
	}
	promptTimestamps := newPromptTimestampRenderer(terminalOutputWriter(output, renderer), terminalOutputFlusher(renderer), time.Now)
	writeTerminalOutput := func(data []byte) error {
		outputMu.Lock()
		defer outputMu.Unlock()
		return promptTimestamps.Write(data)
	}
	flushTerminalOutput := func() error {
		outputMu.Lock()
		defer outputMu.Unlock()
		return promptTimestamps.Flush()
	}
	togglePromptTimestamps := func() error {
		outputMu.Lock()
		defer outputMu.Unlock()
		enabled, err := promptTimestamps.Toggle()
		if err != nil {
			return err
		}
		return writeAll(output, promptTimestampStatusText(enabled))
	}
	if flagWasProvided(flags, "save") {
		if strings.TrimSpace(*saveName) == "" {
			return errors.New("serial profile name for --save must not be empty")
		}
	}
	sessionCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	application, err := newCLISerialApplication(newSession)
	if err != nil {
		return err
	}
	opened, err := application.OpenSerial(sessionCtx, app.OpenSerialRequest{
		Profile:    *profileName,
		ConfigPath: *configPath,
		Overrides:  serialOverrides(flags, port, baudRate, dataBits, parity, stopBits, flowControl, wake),
		Save:       strings.TrimSpace(*saveName),
	})
	if err != nil {
		return formatCLISerialOpenError(err, *configPath)
	}
	terminal := applicationSession{application: application, identifier: opened.Info.ID}
	defer func() {
		if _, closeErr := application.CloseSession(opened.Info.ID); closeErr != nil && err == nil {
			err = fmt.Errorf("close serial port %q: %w", opened.Profile.Port, closeErr)
		}
	}()
	stopInputEcho, err := terminalinput.MakeRaw(input)
	if err != nil {
		return fmt.Errorf("configure console input: %w", err)
	}
	defer func() {
		if restoreErr := stopInputEcho(); restoreErr != nil && err == nil {
			err = fmt.Errorf("restore console input: %w", restoreErr)
		}
	}()
	if err := writeConnectionStatus(output, opened.Profile.Port, opened.Profile.BaudRate); err != nil {
		return fmt.Errorf("write connection status: %w", err)
	}
	if targetReference != "" {
		if err := writeTargetReferenceStatus(output, targetReference); err != nil {
			return fmt.Errorf("write target reference status: %w", err)
		}
	}
	// Input has an independent goroutine because a blocked serial Write must not
	// delay output forwarding. Session's dedicated reader and bounded buffer keep
	// an independently slow stdout consumer from blocking serial Read. Raw mode
	// leaves control bytes available to the shared interactive controller, which
	// forwards Ctrl+C to the remote Session and reserves only the escape prefix
	// for ChannelTerm-local commands.
	go forwardInputWithPromptTimestamp(input, terminal, writeLocalOutput, togglePromptTimestamps, cancel)

	cursor := session.OutputCursor(0)
	lastOutputEndedLine := true
	for {
		chunk, err := terminal.ReadOutput(sessionCtx, cursor, 32*1024)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				if flushErr := flushTerminalOutput(); flushErr != nil {
					return fmt.Errorf("flush serial output: %w", flushErr)
				}
				if err := writeDisconnectStatusWriter(writeLocalOutput, lastOutputEndedLine); err != nil {
					return fmt.Errorf("write disconnect status: %w", err)
				}
				return nil
			}
			return fmt.Errorf("read serial output: %w", err)
		}
		cursor = chunk.Next
		if len(chunk.Data) == 0 {
			continue
		}
		if err := writeTerminalOutput(chunk.Data); err != nil {
			return fmt.Errorf("write serial output: %w", err)
		}
		lastOutputEndedLine = chunk.Data[len(chunk.Data)-1] == '\n'
	}
}

// terminalOutputWriter returns a local-output writer that optionally applies
// ANSI highlighting. The raw bytes remain owned by Session and are never
// altered before MCP or other consumers read them.
func terminalOutputWriter(output io.Writer, renderer *highlight.Renderer) func([]byte) error {
	if renderer == nil {
		return func(data []byte) error { return writeAll(output, data) }
	}
	return func(data []byte) error {
		_, err := renderer.Write(data)
		return err
	}
}

// terminalOutputFlusher returns orderly-disconnect cleanup for a renderer that
// may retain one incomplete safe line while waiting for a terminal delimiter.
func terminalOutputFlusher(renderer *highlight.Renderer) func() error {
	if renderer == nil {
		return func() error { return nil }
	}
	return renderer.Flush
}

// runConnect resolves a reference emitted by list and opens that local target
// in this CLI process. It does not attach to an MCP Session; attach remains the
// command for a Session owned by a running MCP host.
func runConnect(ctx context.Context, args []string, input io.Reader, output io.Writer, newSession serialSessionFactory, listPorts func() ([]serialtransport.Port, error)) error {
	return runConnectWithResolver(ctx, args, input, output, newSession, func(_ context.Context, reference string) (string, error) {
		return resolveSerialTargetReference(reference, listPorts)
	})
}

// runApplicationConnect resolves a target through Application before applying
// CLI-only serial flags and rendering the local interactive session.
func runApplicationConnect(ctx context.Context, args []string, input io.Reader, output io.Writer, newSession serialSessionFactory) error {
	return runConnectWithResolver(ctx, args, input, output, newSession, func(ctx context.Context, reference string) (string, error) {
		application, err := app.New(app.Dependencies{Manager: session.NewManager()})
		if err != nil {
			return "", err
		}
		return application.ResolveSerialTarget(ctx, reference)
	})
}

// runConnectWithResolver keeps target discovery a Core use case while leaving
// CLI syntax validation and command rendering in the adapter.
func runConnectWithResolver(ctx context.Context, args []string, input io.Reader, output io.Writer, newSession serialSessionFactory, resolve func(context.Context, string) (string, error)) error {
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		writeConnectUsage(output)
		return nil
	}
	if len(args) == 0 {
		writeConnectUsage(output)
		return errors.New("connect requires a target reference; run channelterm list --transport serial")
	}
	if strings.HasPrefix(args[0], "-") {
		writeConnectUsage(output)
		return fmt.Errorf("connect target reference must be first, got %q", args[0])
	}
	if err := rejectConnectPortOverride(args[1:]); err != nil {
		return err
	}
	port, err := resolve(ctx, args[0])
	if err != nil {
		return err
	}
	serialArgs := make([]string, 0, len(args)+1)
	serialArgs = append(serialArgs, "--port", port)
	serialArgs = append(serialArgs, args[1:]...)
	return runSerialWithTarget(ctx, serialArgs, input, output, newSession, serialTargetReference(port))
}

// writeConnectUsage documents the distinct lifetimes of direct targets and
// MCP Sessions so a short reference cannot imply that a local process can
// attach to a Session owned by another process.
func writeConnectUsage(output io.Writer) {
	fmt.Fprintln(output, "Usage: channelterm connect TARGET_REF [serial options]")
	fmt.Fprintln(output)
	fmt.Fprintln(output, "Connect directly to a serial target listed by channelterm list, for example SER-COM8.")
	fmt.Fprintln(output, "Use channelterm attach SER-1 only for an existing MCP-managed Session.")
	fmt.Fprintln(output, "The target reference must be first; --port is selected by TARGET_REF and cannot be overridden.")
}

// rejectConnectPortOverride prevents a displayed target reference and a later
// --port value from disagreeing about which hardware this invocation opens.
func rejectConnectPortOverride(args []string) error {
	for _, arg := range args {
		if arg == "--port" || strings.HasPrefix(arg, "--port=") {
			return errors.New("connect selects the serial port from TARGET_REF; remove --port")
		}
	}
	return nil
}

// resolveSerialTargetReference scans only for the requested port before
// connecting. The reference is based on the endpoint text rather than scan
// order, and the scan ensures the displayed device is still present.
func resolveSerialTargetReference(reference string, listPorts func() ([]serialtransport.Port, error)) (string, error) {
	if listPorts == nil {
		return "", errors.New("serial target lister must not be nil")
	}
	reference = strings.TrimSpace(reference)
	if !strings.HasPrefix(strings.ToUpper(reference), "SER-") {
		return "", fmt.Errorf("unsupported direct target reference %q; currently only SER-* targets are supported", reference)
	}
	ports, err := listPorts()
	if err != nil {
		return "", fmt.Errorf("list serial targets: %w", err)
	}
	for _, port := range ports {
		if strings.EqualFold(serialTargetReference(port.Name), reference) {
			return port.Name, nil
		}
	}
	return "", fmt.Errorf("serial target reference %q is not present; run channelterm list --transport serial", reference)
}

// serialOverrides retains only flags the user actually supplied. This avoids a
// CLI default value silently overriding a value inherited from a saved profile.
func serialOverrides(flags *flag.FlagSet, port *string, baudRate, dataBits *int, parity, stopBits, flowControl *string, wake *bool) config.SerialOverrides {
	overrides := config.SerialOverrides{}
	if flagWasProvided(flags, "port") {
		overrides.Port = port
	}
	if flagWasProvided(flags, "baud") {
		overrides.BaudRate = baudRate
	}
	if flagWasProvided(flags, "data-bits") {
		overrides.DataBits = dataBits
	}
	if flagWasProvided(flags, "parity") {
		overrides.Parity = parity
	}
	if flagWasProvided(flags, "stop-bits") {
		overrides.StopBits = stopBits
	}
	if flagWasProvided(flags, "flow-control") {
		overrides.FlowControl = flowControl
	}
	if flagWasProvided(flags, "wake") {
		overrides.Wake = wake
	}
	return overrides
}

// flagWasProvided distinguishes an omitted flag from a flag set to its default
// value, which flag.FlagSet otherwise represents identically to callers.
func flagWasProvided(flags *flag.FlagSet, name string) bool {
	provided := false
	flags.Visit(func(flag *flag.Flag) {
		if flag.Name == name {
			provided = true
		}
	})
	return provided
}

// writeConnectionStatus writes local status with explicit CRLF delimiters so
// terminal raw mode cannot leave subsequent messages at a stale column.
func writeConnectionStatus(output io.Writer, port string, baudRate int) error {
	// Raw mode disables the local terminal's automatic LF-to-CRLF conversion.
	// This status is local CLI output, unlike serial RX data, so it must include
	// explicit CRLF delimiters to keep subsequent local lines at column zero.
	return writeAll(output, []byte(fmt.Sprintf("Connected: %s @ %d\r\nNo wake character sent by default; use --wake for an idle shell without a prompt.\r\nEscape: Ctrl+]  |  Help: Ctrl+] ?  |  Prompt time: Ctrl+] t\r\n", port, baudRate)))
}

// writeTargetReferenceStatus prints the short stable reference used for this
// direct connection without exposing an internal Session ID.
func writeTargetReferenceStatus(output io.Writer, reference string) error {
	return writeAll(output, []byte(fmt.Sprintf("Target: %s\r\n", reference)))
}

// writeDisconnectStatus keeps local status separate from unmodified remote
// bytes by adding a line break only after unterminated remote output.
func writeDisconnectStatus(output io.Writer, lastOutputEndedLine bool) error {
	return writeDisconnectStatusWriter(func(data []byte) error { return writeAll(output, data) }, lastOutputEndedLine)
}

// writeDisconnectStatusWriter keeps disconnect output ordered with concurrent
// local escape help and remote terminal writes through one caller-owned writer.
func writeDisconnectStatusWriter(writeLocal func([]byte) error, lastOutputEndedLine bool) error {
	// Serial output is forwarded unchanged, so the CLI remembers whether its
	// final byte already ended a line. The optional prefix keeps a local shell
	// prompt from joining an unterminated remote line without adding a blank
	// line after normally terminated output.
	if !lastOutputEndedLine {
		if err := writeLocal([]byte("\r\n")); err != nil {
			return err
		}
	}
	return writeLocal([]byte("Disconnected.\r\n"))
}

// printVersion emits the stable CLI version format used by scripts and users.
func printVersion(output io.Writer) {
	fmt.Fprintf(output, "channelterm %s\n", version)
}

// newCLISerialApplication adapts the CLI's injectable Session factory to the
// shared serial application use case. Each direct CLI connection has a private
// Manager, while MCP supplies its long-lived Manager to the same use case.
func newCLISerialApplication(factory serialSessionFactory) (*app.Application, error) {
	if factory == nil {
		return app.New(app.Dependencies{Manager: session.NewManager()})
	}
	return app.New(app.Dependencies{
		Manager: session.NewManager(),
		Serial: app.SerialDependencies{NewSession: func(_ string, configuration serialtransport.Config) (app.ConnectedSession, error) {
			return factory(configuration)
		}},
	})
}

// listSerialPorts creates a short-lived Application for read-only local
// discovery. The Manager is never populated; port enumeration remains a Core
// use case rather than a CLI call to the Serial implementation.
func listSerialPorts(ctx context.Context) ([]serialtransport.Port, error) {
	application, err := app.New(app.Dependencies{Manager: session.NewManager()})
	if err != nil {
		return nil, err
	}
	return application.ListSerialPorts(ctx)
}

// applicationSession adapts one Application-managed Session to the local CLI
// I/O loop. It never exposes or owns the underlying Session; each operation
// crosses the Application boundary so CLI cannot bypass Core use cases.
type applicationSession struct {
	application *app.Application
	identifier  string
}

// ReadOutput waits for output through Application while preserving the CLI's
// private cursor ownership.
func (s applicationSession) ReadOutput(ctx context.Context, next session.OutputCursor, maxBytes int) (session.OutputChunk, error) {
	return s.application.ReadSession(ctx, s.identifier, &next, maxBytes)
}

// Write forwards local input through Application as an atomic Session write.
func (s applicationSession) Write(request session.WriteRequest) (int, error) {
	return s.application.WriteSession(context.Background(), s.identifier, request)
}

// formatCLISerialOpenError preserves the direct CLI guidance for a missing
// port while all profile resolution and Session creation stay in internal/core/app.
func formatCLISerialOpenError(err error, configuredPath string) error {
	if errors.Is(err, config.ErrProfileNotFound) {
		return fmt.Errorf("resolve serial configuration %q: %w", cliSerialConfigPath(configuredPath), rootCause(err))
	}
	if errors.Is(err, config.ErrSerialPortRequired) {
		return fmt.Errorf("%w; set serial.profiles.<name>.port in %q or pass --port", err, cliSerialConfigPath(configuredPath))
	}
	return err
}

// cliSerialConfigPath resolves the user-visible path only for CLI error
// guidance. Application owns loading and does not depend on this presentation detail.
func cliSerialConfigPath(configuredPath string) string {
	if configuredPath != "" {
		return configuredPath
	}
	path, err := config.DefaultPath()
	if err != nil {
		return configuredPath
	}
	return path
}

// rootCause removes application context when preserving a pre-existing CLI
// error format that names the configuration file at its own adapter boundary.
func rootCause(err error) error {
	for {
		next := errors.Unwrap(err)
		if next == nil {
			return err
		}
		err = next
	}
}

// forwardInput runs independently of output forwarding so a blocked Session
// write cannot stop receive display. It applies the protocol-neutral escape
// controller before writing remote bytes, keeping local CLI commands out of
// every Transport implementation.
func forwardInput(input io.Reader, terminal interface {
	Write(session.WriteRequest) (int, error)
}, writeLocal func([]byte) error, cancel context.CancelFunc) {
	forwardInputWithPromptTimestamp(input, terminal, writeLocal, nil, cancel)
}

// forwardInputWithPromptTimestamp adds local prompt-timestamp control to the
// normal input bridge. togglePromptTimestamp changes only the caller-owned
// presentation state and is never sent through Session.Write.
func forwardInputWithPromptTimestamp(input io.Reader, terminal interface {
	Write(session.WriteRequest) (int, error)
}, writeLocal func([]byte) error, togglePromptTimestamp func() error, cancel context.CancelFunc) {
	controller := interactive.NewController(interactive.DefaultEscapeByte)
	buffer := make([]byte, 4*1024)
	for {
		n, err := input.Read(buffer)
		if n > 0 {
			for _, action := range controller.Process(buffer[:n]) {
				switch action.Kind {
				case interactive.ActionRemote:
					if writeSession(terminal, session.ActorUser, action.Data) != nil {
						cancel()
						return
					}
				case interactive.ActionEscapePending:
					if writeLocal == nil || writeLocal(escapePendingText) != nil {
						cancel()
						return
					}
				case interactive.ActionQuit:
					cancel()
					return
				case interactive.ActionHelp:
					if writeLocal == nil || writeLocal(escapeHelpText) != nil {
						cancel()
						return
					}
				case interactive.ActionTogglePromptTimestamp:
					if togglePromptTimestamp == nil || togglePromptTimestamp() != nil {
						cancel()
						return
					}
				case interactive.ActionUnknownEscape:
					if writeLocal == nil || writeLocal(unknownEscapeText(action.Command)) != nil {
						cancel()
						return
					}
				}
			}
		}
		if err != nil {
			return
		}
		if n == 0 {
			// An io.Reader should not return (0, nil) with a non-empty buffer.
			// Treat it as an ended input source to avoid a CPU spin on a broken
			// console or test reader.
			return
		}
	}
}

// escapePendingText confirms locally that Ctrl+] entered escape mode. Its
// leading and trailing line breaks keep it readable beside unstructured remote
// terminal output; it is never sent to the remote Session.
var escapePendingText = []byte("\r\n[ChannelTerm] Escape: q quit | ? help | ] send Ctrl+] | t prompt time | Esc cancel\r\n")

// escapeHelpText is local CLI output and is never sent to the remote Session.
var escapeHelpText = []byte("\r\nChannelTerm escape commands:\r\n\r\n  q    Quit session\r\n  ?    Show this help\r\n  ]    Send Ctrl+] to remote\r\n  t    Toggle prompt timestamps\r\n  Esc  Cancel escape mode\r\n")

// promptTimestampStatusText reports a CLI-local presentation setting. It is
// written only to the current terminal output and never enters Session data.
func promptTimestampStatusText(enabled bool) []byte {
	state := "OFF"
	if enabled {
		state = "ON"
	}
	return []byte(fmt.Sprintf("\r\n[ChannelTerm] Prompt timestamps: %s\r\n", state))
}

// unknownEscapeText explains a discarded local command without sending its
// byte to the remote endpoint, then leaves the controller in normal mode.
func unknownEscapeText(command byte) []byte {
	return []byte(fmt.Sprintf("\r\nUnknown escape command %q. Use Ctrl+] ? for help.\r\n", command))
}

// writeSession retries short successful writes so serial input is forwarded as
// a complete byte sequence instead of silently truncating it.
func writeSession(terminal interface {
	Write(session.WriteRequest) (int, error)
}, actor session.Actor, data []byte) error {
	for len(data) > 0 {
		n, err := terminal.Write(session.WriteRequest{Actor: actor, Data: data})
		if n > 0 {
			data = data[n:]
		}
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
	}
	return nil
}

// writeAll applies the same complete-write guarantee to local output, where a
// short writer must not split status or received terminal data.
func writeAll(output io.Writer, data []byte) error {
	for len(data) > 0 {
		n, err := output.Write(data)
		if n > 0 {
			data = data[n:]
		}
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
	}
	return nil
}
