package command

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/cli/terminalinput"
	"github.com/akira-init1/ChannelTerm/internal/core/app"
	"github.com/akira-init1/ChannelTerm/internal/core/session"
	serialtransport "github.com/akira-init1/ChannelTerm/internal/core/transport/serial"
	protocol "github.com/modelcontextprotocol/go-sdk/mcp"
)

const defaultMCPEndpoint = "http://" + defaultMCPListen + defaultMCPPath

var (
	// ErrAttachSessionIDRequired is returned when attach receives no Session ID.
	ErrAttachSessionIDRequired = errors.New("session ID is required")
	// ErrAttachedSessionNotFound is returned when the MCP host does not manage
	// the requested Session.
	ErrAttachedSessionNotFound = errors.New("attached session not found")
)

// attachSession is the CLI-facing view of a Session managed by another process.
//
// Close only releases the MCP client connection. It never closes the remote
// Session, whose lifecycle remains owned by the MCP host's Manager.
type attachSession interface {
	ReadRecent(context.Context, int) (session.OutputChunk, error)
	ReadOutput(context.Context, session.OutputCursor, int) (session.OutputChunk, error)
	ReadRecentActivity(context.Context, int) (session.ActivityChunk, error)
	ReadActivity(context.Context, session.ActivityCursor, int) (session.ActivityChunk, error)
	Write(session.WriteRequest) (int, error)
	Close() error
}

// attachSessionFactory creates a detached client view for an existing Session.
type attachSessionFactory func(context.Context, string, string) (attachSession, error)

// mcpAttachSession translates the CLI's byte-oriented Session operations to
// existing MCP terminal tools. The remote Manager continues to own the actual
// Session and its single Transport reader.
type mcpAttachSession struct {
	id     string
	client *protocol.ClientSession
}

// newMCPAttachSession connects to an MCP HTTP host and verifies that id is
// currently registered there. endpoint must be the complete Streamable HTTP
// MCP endpoint, such as http://127.0.0.1:37099/mcp.
func newMCPAttachSession(ctx context.Context, endpoint, id string) (_ attachSession, err error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, ErrAttachSessionIDRequired
	}
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return nil, errors.New("MCP endpoint is required")
	}
	remote, err := connectMCPClient(ctx, endpoint)
	if err != nil {
		return nil, fmt.Errorf("connect MCP endpoint %q: %w", endpoint, err)
	}
	defer func() {
		if err != nil {
			_ = remote.Close()
		}
	}()

	attached := &mcpAttachSession{id: id, client: remote}
	if err := attached.verify(ctx); err != nil {
		return nil, err
	}
	return attached, nil
}

// runAttach is the unified user terminal entry point. A Session ID or short
// Session reference attaches to an existing host session, while a serial target
// reference such as SER-COM8 creates or reuses one shared host session first.
func runAttach(ctx context.Context, args []string, input io.Reader, output io.Writer, newAttach attachSessionFactory) error {
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		return runAttachTargetFirst(ctx, "SER-COM8", args, input, output, newAttach)
	}
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		return runAttachTargetFirst(ctx, args[0], args[1:], input, output, newAttach)
	}
	return runAttachSession(ctx, args, input, output, newAttach)
}

// runAttachSession creates a client-specific cursor and bridges local terminal
// I/O to an already-open Session. Cancellation detaches only this CLI client;
// it does not call terminal_close or otherwise change the Manager-owned lifecycle.
func runAttachSession(ctx context.Context, args []string, input io.Reader, output io.Writer, newAttach attachSessionFactory) (err error) {
	flags := flag.NewFlagSet("attach", flag.ContinueOnError)
	flags.SetOutput(output)
	endpoint := flags.String("endpoint", defaultMCPEndpoint, "MCP Streamable HTTP endpoint")
	highlightMode := flags.String("highlight", "auto", "terminal highlighting: auto, always, or never")
	help := flags.Bool("help", false, "show help and exit")
	shortHelp := flags.Bool("h", false, "show help and exit")
	flags.Usage = func() {
		writeAttachTargetUsage(output)
		fmt.Fprintln(output)
		fmt.Fprintln(output, "For compatibility, flags may also precede an existing SESSION reference.")
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
	if flags.NArg() == 0 {
		return ErrAttachSessionIDRequired
	}
	if flags.NArg() > 1 {
		return fmt.Errorf("unexpected attach argument %q", flags.Arg(1))
	}
	renderer, err := resolveHighlightRenderer(*highlightMode, output)
	if err != nil {
		return err
	}
	if newAttach == nil {
		return errors.New("attach session factory must not be nil")
	}

	attached, err := newAttach(ctx, *endpoint, flags.Arg(0))
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := attached.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("detach session %q: %w", flags.Arg(0), closeErr)
		}
	}()
	rawInput, stopInputEcho, err := terminalinput.MakeRaw(input)
	if err != nil {
		return fmt.Errorf("configure console input: %w", err)
	}
	input = rawInput
	defer func() {
		if restoreErr := stopInputEcho(); restoreErr != nil && err == nil {
			err = fmt.Errorf("restore console input: %w", restoreErr)
		}
	}()

	attachCtx, cancel := context.WithCancel(ctx)
	var activityWait sync.WaitGroup
	defer func() {
		cancel()
		activityWait.Wait()
	}()
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
	go forwardInputWithPromptTimestamp(input, attached, writeLocalOutput, togglePromptTimestamps, cancel)

	activity, err := attached.ReadRecentActivity(attachCtx, 1)
	if err != nil && !errors.Is(err, context.Canceled) {
		return fmt.Errorf("read attached session activity: %w", err)
	}
	activityCursor := activity.Next
	activityWait.Add(1)
	go func() {
		defer activityWait.Done()
		forwardAgentActivity(attachCtx, attached, activityCursor, writeLocalOutput)
	}()

	chunk, err := attached.ReadRecent(attachCtx, session.DefaultAIReadLimit)
	if err != nil && !errors.Is(err, context.Canceled) {
		return fmt.Errorf("read attached session output: %w", err)
	}
	cursor := chunk.Next
	lastOutputEndedLine := true
	if len(chunk.Data) > 0 {
		if err := writeTerminalOutput(chunk.Data); err != nil {
			return fmt.Errorf("write attached session output: %w", err)
		}
		lastOutputEndedLine = chunk.Data[len(chunk.Data)-1] == '\n'
	}
	for {
		chunk, err := attached.ReadOutput(attachCtx, cursor, 32*1024)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				// The Activity observer shares the local Writer. Wait for its context
				// cancellation before emitting the detach status so local-only blocks
				// cannot interleave with this final terminal status line.
				activityWait.Wait()
				if flushErr := flushTerminalOutput(); flushErr != nil {
					return fmt.Errorf("flush attached session output: %w", flushErr)
				}
				if err := writeDetachStatus(output, lastOutputEndedLine); err != nil {
					return fmt.Errorf("write detach status: %w", err)
				}
				return nil
			}
			return fmt.Errorf("read attached session output: %w", err)
		}
		cursor = chunk.Next
		if len(chunk.Data) == 0 {
			continue
		}
		if err := writeTerminalOutput(chunk.Data); err != nil {
			return fmt.Errorf("write attached session output: %w", err)
		}
		lastOutputEndedLine = chunk.Data[len(chunk.Data)-1] == '\n'
	}
}

// runAttachTargetFirst supports the normal human-oriented order of target
// first and options afterwards. It keeps the older flag-first Session form
// available through runAttachSession for scripts that already use it.
func runAttachTargetFirst(ctx context.Context, target string, args []string, input io.Reader, output io.Writer, newAttach attachSessionFactory) error {
	flags := flag.NewFlagSet("attach", flag.ContinueOnError)
	flags.SetOutput(output)
	endpoint := flags.String("endpoint", defaultMCPEndpoint, "Session Host endpoint")
	private := flags.Bool("private", false, "open a private local connection without Session Host or MCP")
	flags.BoolVar(private, "no-mcp", false, "alias for --private")
	highlightMode := flags.String("highlight", "auto", "terminal highlighting: auto, always, or never")
	help := flags.Bool("help", false, "show help and exit")
	shortHelp := flags.Bool("h", false, "show help and exit")
	defineAttachSerialFlags(flags)
	flags.Usage = func() {
		writeAttachTargetUsage(output)
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
		return fmt.Errorf("unexpected attach argument %q", flags.Arg(0))
	}
	if isSerialTargetReference(target) && !isShortSessionReference(target) {
		serialArgs := stripAttachControlFlags(args)
		if *private {
			serialArgs = append(serialArgs, "--highlight", *highlightMode)
			return runApplicationConnect(ctx, append([]string{target}, serialArgs...), input, output, nil)
		}
		opened, reused, err := openSharedSerialTarget(ctx, strings.TrimSpace(*endpoint), target, serialArgs)
		if err != nil {
			return err
		}
		state := "created"
		if reused {
			state = "reused"
		}
		if _, err := fmt.Fprintf(output, "Shared Session %s: %s (%s)\r\n", state, opened.Reference, opened.ID); err != nil {
			return err
		}
		return runAttachSession(ctx, []string{"--endpoint", *endpoint, "--highlight", *highlightMode, opened.ID}, input, output, newAttach)
	}
	if *private {
		return errors.New("--private requires a serial target reference such as SER-COM8")
	}
	if hasAttachSerialOption(flags) {
		return errors.New("serial options require a serial target reference such as SER-COM8")
	}
	return runAttachSession(ctx, []string{"--endpoint", *endpoint, "--highlight", *highlightMode, target}, input, output, newAttach)
}

// defineAttachSerialFlags accepts the same connection settings as serial and
// connect. The shared host receives only options the caller explicitly set.
func defineAttachSerialFlags(flags *flag.FlagSet) {
	flags.String("profile", "", "named serial profile to use")
	flags.String("config", "", "path to a TOML configuration file")
	flags.Int("baud", 115200, "serial baud rate")
	flags.Int("data-bits", 8, "serial data bits: 5, 6, 7, or 8")
	flags.String("parity", string(serialtransport.ParityNone), "serial parity: none, odd, even, mark, or space")
	flags.String("stop-bits", string(serialtransport.StopBitsOne), "serial stop bits: 1, 1.5, or 2")
	flags.String("flow-control", string(serialtransport.FlowControlNone), "serial flow control: none, software, or hardware")
	flags.Bool("wake", false, "send one carriage return after creating a session")
	flags.String("label", "", "display label for a newly created shared session")
	flags.String("save", "", "save resolved settings as a named serial profile")
}

// writeAttachTargetUsage describes the target-first form without exposing the
// internal MCP protocol as a separate action users must learn.
func writeAttachTargetUsage(output io.Writer) {
	fmt.Fprintln(output, "Usage: channelterm attach TARGET_OR_SESSION [options]")
	fmt.Fprintln(output)
	fmt.Fprintln(output, "attach SER-COM8 creates or joins a shared local Session Host connection.")
	fmt.Fprintln(output, "attach SER-1 or a full session_id joins an existing shared Session.")
	fmt.Fprintln(output, "--private (or --no-mcp) opens a local connection that MCP and other users cannot join.")
	fmt.Fprintln(output, "Ctrl+C is sent to the remote session. Use Ctrl+] q to leave this CLI window; Ctrl+] t toggles local prompt timestamps.")
}

// hasAttachSerialOption detects settings that cannot apply when target names a
// Session rather than a physical serial endpoint.
func hasAttachSerialOption(flags *flag.FlagSet) bool {
	for _, name := range []string{"profile", "config", "baud", "data-bits", "parity", "stop-bits", "flow-control", "wake", "label", "save"} {
		if flagWasProvided(flags, name) {
			return true
		}
	}
	return false
}

// stripAttachControlFlags leaves only serial options for connect or the MCP
// open tool. endpoint and privacy select the attachment mode, not the serial
// transport configuration.
func stripAttachControlFlags(args []string) []string {
	serialArgs := make([]string, 0, len(args))
	for index := 0; index < len(args); index++ {
		arg := args[index]
		switch arg {
		case "--private", "--no-mcp":
			continue
		case "--endpoint", "--highlight":
			index++
			continue
		}
		if strings.HasPrefix(arg, "--endpoint=") || strings.HasPrefix(arg, "--highlight=") {
			continue
		}
		serialArgs = append(serialArgs, arg)
	}
	return serialArgs
}

// isShortSessionReference distinguishes Manager references such as SER-1 from
// endpoint references such as SER-COM8 without assuming future transport names.
func isShortSessionReference(value string) bool {
	separator := strings.LastIndex(strings.TrimSpace(value), "-")
	if separator <= 0 || separator == len(strings.TrimSpace(value))-1 {
		return false
	}
	_, err := strconv.ParseUint(strings.TrimSpace(value)[separator+1:], 10, 64)
	return err == nil
}

// isSerialTargetReference recognizes the deterministic serial endpoint
// references emitted by list. Session references are handled separately.
func isSerialTargetReference(value string) bool {
	return strings.HasPrefix(strings.ToUpper(strings.TrimSpace(value)), "SER-")
}

// openSharedSerialTarget ensures the local Session Host exists, then uses its
// normal MCP tools to atomically create or reuse a serial Session. The Host is
// the sole physical-port owner; this CLI only attaches afterwards.
func openSharedSerialTarget(ctx context.Context, endpoint, target string, serialArgs []string) (mcpListedSession, bool, error) {
	if !isLocalMCPEndpoint(endpoint) {
		return mcpListedSession{}, false, errors.New("opening a target reference requires a local Session Host endpoint")
	}
	application, err := app.New(app.Dependencies{Manager: session.NewManager()})
	if err != nil {
		return mcpListedSession{}, false, err
	}
	port, err := application.ResolveSerialTarget(ctx, target)
	if err != nil {
		return mcpListedSession{}, false, err
	}
	if err := ensureMCPHost(ctx, endpoint); err != nil {
		return mcpListedSession{}, false, err
	}
	client, err := connectMCPClient(ctx, endpoint)
	if err != nil {
		return mcpListedSession{}, false, fmt.Errorf("connect Session Host %q: %w", endpoint, err)
	}
	defer func() { _ = client.Close() }()
	var listed struct {
		Sessions []mcpListedSession `json:"sessions"`
	}
	if err := callMCPTool(ctx, client, "terminal_list_sessions", map[string]any{}, &listed); err != nil {
		return mcpListedSession{}, false, err
	}
	if !attachOptionIsProvided(serialArgs, "save") {
		for _, candidate := range listed.Sessions {
			if candidate.Transport == "serial" && strings.EqualFold(candidate.Endpoint, port) && candidate.State == "open" {
				return candidate, true, nil
			}
		}
	}
	arguments, err := attachSerialOpenArguments(serialArgs, port)
	if err != nil {
		return mcpListedSession{}, false, err
	}
	var opened struct {
		ID        string `json:"session_id"`
		Reference string `json:"session_ref"`
		Reused    bool   `json:"reused"`
	}
	if err := callMCPTool(ctx, client, "terminal_open_serial", arguments, &opened); err != nil {
		return mcpListedSession{}, false, err
	}
	return mcpListedSession{ID: opened.ID, Reference: opened.Reference, Transport: "serial", Endpoint: port, State: "open"}, opened.Reused, nil
}

// attachSerialOpenArguments translates explicitly supplied attach options to
// the MCP terminal_open_serial schema. Target selection always supplies port.
func attachSerialOpenArguments(args []string, port string) (map[string]any, error) {
	flags := flag.NewFlagSet("attach serial options", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	profile := flags.String("profile", "", "")
	configPath := flags.String("config", "", "")
	baud := flags.Int("baud", 115200, "")
	dataBits := flags.Int("data-bits", 8, "")
	parity := flags.String("parity", string(serialtransport.ParityNone), "")
	stopBits := flags.String("stop-bits", string(serialtransport.StopBitsOne), "")
	flowControl := flags.String("flow-control", string(serialtransport.FlowControlNone), "")
	wake := flags.Bool("wake", false, "")
	label := flags.String("label", "", "")
	save := flags.String("save", "", "")
	if err := flags.Parse(args); err != nil {
		return nil, err
	}
	if flags.NArg() != 0 {
		return nil, fmt.Errorf("unexpected serial option %q", flags.Arg(0))
	}
	arguments := map[string]any{"port": port}
	if flagWasProvided(flags, "profile") {
		arguments["profile"] = *profile
	}
	if flagWasProvided(flags, "config") {
		arguments["config_path"] = *configPath
	}
	if flagWasProvided(flags, "baud") {
		arguments["baud"] = *baud
	}
	if flagWasProvided(flags, "data-bits") {
		arguments["data_bits"] = *dataBits
	}
	if flagWasProvided(flags, "parity") {
		arguments["parity"] = *parity
	}
	if flagWasProvided(flags, "stop-bits") {
		arguments["stop_bits"] = *stopBits
	}
	if flagWasProvided(flags, "flow-control") {
		arguments["flow_control"] = *flowControl
	}
	if flagWasProvided(flags, "wake") {
		arguments["wake"] = *wake
	}
	if flagWasProvided(flags, "label") {
		arguments["label"] = *label
	}
	if flagWasProvided(flags, "save") {
		arguments["save"] = *save
	}
	return arguments, nil
}

// attachOptionIsProvided checks one serial option without connecting. It is
// used for --save because saving configuration must still run when the target
// already has a shared Session.
func attachOptionIsProvided(args []string, option string) bool {
	for _, arg := range args {
		if arg == "--"+option || strings.HasPrefix(arg, "--"+option+"=") {
			return true
		}
	}
	return false
}

// ensureMCPHost starts the default loopback Host when it is absent. The child
// owns Sessions independently from the attaching CLI, so Ctrl+C in this window
// cannot terminate a shared connection that other users or Agents still use.
func ensureMCPHost(ctx context.Context, endpoint string) error {
	if _, err := listMCPSessions(ctx, endpoint); err == nil {
		return nil
	}
	if endpoint != defaultMCPEndpoint {
		return fmt.Errorf("Session Host %q is offline; start it explicitly before attach", endpoint)
	}
	command := exec.Command(os.Args[0], "mcp", "--transport", "http")
	command.Stdin = nil
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	if err := command.Start(); err != nil {
		return fmt.Errorf("start Session Host: %w", err)
	}
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return errors.New("Session Host did not become ready within 5 seconds")
		case <-tick.C:
			if _, err := listMCPSessions(ctx, endpoint); err == nil {
				return nil
			}
		}
	}
}

// verify rejects a misspelled or already-closed Session before changing local
// console mode. The lookup runs in the MCP host, where the Manager owns the
// authoritative Session map.
func (s *mcpAttachSession) verify(ctx context.Context) error {
	var listed struct {
		Sessions []struct {
			ID        string `json:"session_id"`
			Reference string `json:"session_ref"`
		} `json:"sessions"`
	}
	if err := s.call(ctx, "terminal_list_sessions", map[string]any{}, &listed); err != nil {
		return err
	}
	for _, candidate := range listed.Sessions {
		if candidate.ID == s.id || candidate.Reference == s.id {
			return nil
		}
	}
	return fmt.Errorf("%w: %q", ErrAttachedSessionNotFound, s.id)
}

// ReadRecent snapshots the latest retained output and yields the cursor for
// later waits. This avoids replaying an entire potentially 16 MiB Ring Buffer
// when a user attaches to a long-running Session.
func (s *mcpAttachSession) ReadRecent(ctx context.Context, maxBytes int) (session.OutputChunk, error) {
	return s.read(ctx, "terminal_read", nil, maxBytes)
}

// ReadOutput waits at next through terminal_wait, leaving cursor ownership with
// this CLI process rather than any MCP Agent using the same remote Session.
func (s *mcpAttachSession) ReadOutput(ctx context.Context, next session.OutputCursor, maxBytes int) (session.OutputChunk, error) {
	return s.read(ctx, "terminal_wait", &next, maxBytes)
}

// ReadRecentActivity snapshots activity only to establish the attach client's
// private cursor. Existing events are intentionally not replayed as local AI
// blocks when a user attaches to a long-running shared Session.
func (s *mcpAttachSession) ReadRecentActivity(ctx context.Context, maxEvents int) (session.ActivityChunk, error) {
	return s.readActivity(ctx, "terminal_read_activity", nil, maxEvents)
}

// ReadActivity waits through terminal_wait_activity without consuming an Agent
// cursor held by any other MCP or CLI client.
func (s *mcpAttachSession) ReadActivity(ctx context.Context, next session.ActivityCursor, maxEvents int) (session.ActivityChunk, error) {
	return s.readActivity(ctx, "terminal_wait_activity", &next, maxEvents)
}

// Write forwards the complete caller payload in Base64 so raw local terminal
// bytes remain lossless. The remote Session's write lock provides atomicity
// with MCP Agent terminal_write calls.
func (s *mcpAttachSession) Write(request session.WriteRequest) (int, error) {
	return s.WriteContext(context.Background(), request)
}

// WriteContext forwards one complete caller payload while allowing non-
// interactive CLI use cases such as file transfer to cancel an in-flight MCP
// request. The host-owned Session still provides the actual write serialization.
func (s *mcpAttachSession) WriteContext(ctx context.Context, request session.WriteRequest) (int, error) {
	if !request.Actor.Valid() {
		return 0, fmt.Errorf("%w: %q", session.ErrInvalidActor, request.Actor)
	}
	var result struct {
		BytesWritten int `json:"bytes_written"`
	}
	if err := s.call(ctx, "terminal_write", map[string]any{
		"session_id": s.id,
		"data":       base64.StdEncoding.EncodeToString(request.Data),
		"encoding":   "base64",
		"actor":      request.Actor,
	}, &result); err != nil {
		return 0, err
	}
	return result.BytesWritten, nil
}

// Close releases only the MCP client connection and deliberately omits
// terminal_close, preserving the shared Session after local detach.
func (s *mcpAttachSession) Close() error { return s.client.Close() }

// read decodes the lossless Base64 result emitted by the existing terminal
// tools into the core cursor representation used by the local output loop.
func (s *mcpAttachSession) read(ctx context.Context, toolName string, cursor *session.OutputCursor, maxBytes int) (session.OutputChunk, error) {
	arguments := map[string]any{"session_id": s.id, "max_bytes": maxBytes, "encoding": "base64"}
	if cursor != nil {
		arguments["cursor"] = uint64(*cursor)
	}
	var result struct {
		Data    string `json:"data"`
		Next    uint64 `json:"next"`
		Dropped bool   `json:"dropped"`
	}
	if err := s.call(ctx, toolName, arguments, &result); err != nil {
		return session.OutputChunk{}, err
	}
	data, err := base64.StdEncoding.DecodeString(result.Data)
	if err != nil {
		return session.OutputChunk{}, fmt.Errorf("decode %s output: %w", toolName, err)
	}
	return session.OutputChunk{Data: data, Next: session.OutputCursor(result.Next), Dropped: result.Dropped}, nil
}

// readActivity decodes lossless Base64 event payloads returned by the dedicated
// Activity tools. It never calls terminal_read, preserving the separation
// between transport bytes and local Session metadata.
func (s *mcpAttachSession) readActivity(ctx context.Context, toolName string, cursor *session.ActivityCursor, maxEvents int) (session.ActivityChunk, error) {
	arguments := map[string]any{"session_id": s.id, "max_events": maxEvents}
	if cursor != nil {
		arguments["cursor"] = uint64(*cursor)
	}
	var result struct {
		Events []struct {
			Timestamp string `json:"timestamp"`
			Actor     string `json:"actor"`
			Operation string `json:"operation"`
			Data      string `json:"data"`
			Encoding  string `json:"encoding"`
		} `json:"events"`
		Next    uint64 `json:"next"`
		Dropped bool   `json:"dropped"`
	}
	if err := s.call(ctx, toolName, arguments, &result); err != nil {
		return session.ActivityChunk{}, err
	}
	events := make([]session.SessionEvent, 0, len(result.Events))
	for _, encoded := range result.Events {
		if encoded.Encoding != "base64" {
			return session.ActivityChunk{}, fmt.Errorf("decode %s event: unsupported encoding %q", toolName, encoded.Encoding)
		}
		timestamp, err := time.Parse(time.RFC3339Nano, encoded.Timestamp)
		if err != nil {
			return session.ActivityChunk{}, fmt.Errorf("decode %s event timestamp: %w", toolName, err)
		}
		data, err := base64.StdEncoding.DecodeString(encoded.Data)
		if err != nil {
			return session.ActivityChunk{}, fmt.Errorf("decode %s event data: %w", toolName, err)
		}
		events = append(events, session.SessionEvent{Timestamp: timestamp, Actor: session.Actor(encoded.Actor), Operation: session.Operation(encoded.Operation), Data: data})
	}
	return session.ActivityChunk{Events: events, Next: session.ActivityCursor(result.Next), Dropped: result.Dropped}, nil
}

// forwardAgentActivity waits independently from raw output and renders only
// Agent writes. Its context is cancelled during detach so an idle activity wait
// cannot retain the attach client after raw output forwarding has exited.
func forwardAgentActivity(ctx context.Context, attached attachSession, cursor session.ActivityCursor, writeOutput func([]byte) error) {
	for {
		chunk, err := attached.ReadActivity(ctx, cursor, 32)
		if err != nil {
			return
		}
		cursor = chunk.Next
		for _, event := range chunk.Events {
			block := renderAgentActivity(event)
			if len(block) == 0 {
				continue
			}
			if writeOutput(block) != nil {
				return
			}
		}
	}
}

// renderAgentActivity formats local-only AI activity without changing the
// original event. Pure carriage-return and line-feed writes remain recorded but
// do not generate a visually noisy block for split command submissions.
func renderAgentActivity(event session.SessionEvent) []byte {
	if event.Actor != session.ActorAgent || event.Operation != session.OperationWrite || isLineEnding(event.Data) {
		return nil
	}
	payload := strings.TrimRight(string(event.Data), "\r\n")
	if payload == "" {
		return nil
	}
	return []byte(fmt.Sprintf("\r\n──────── AI ────────\r\n[%s] >> %s\r\n────────────────────\r\n", event.Timestamp.Format("15:04:05"), payload))
}

// isLineEnding reports whether data consists exclusively of CR and LF bytes.
func isLineEnding(data []byte) bool {
	if len(data) == 0 {
		return false
	}
	for _, value := range data {
		if value != '\r' && value != '\n' {
			return false
		}
	}
	return true
}

// call invokes one existing MCP terminal tool and unmarshals its structured
// result. Tool errors stay distinct from MCP transport errors for clear CLI
// diagnostics when a Session disappears while attach is waiting.
func (s *mcpAttachSession) call(ctx context.Context, name string, arguments map[string]any, destination any) error {
	return callMCPTool(ctx, s.client, name, arguments, destination)
}

// connectMCPClient opens a short-lived or attached MCP HTTP client. endpoint
// must identify a Streamable HTTP handler; this function does not open a
// terminal Session or send terminal bytes.
func connectMCPClient(ctx context.Context, endpoint string) (*protocol.ClientSession, error) {
	client := protocol.NewClient(&protocol.Implementation{Name: "channelterm-cli", Version: version}, nil)
	remote, err := client.Connect(ctx, &protocol.StreamableClientTransport{
		Endpoint:             endpoint,
		DisableStandaloneSSE: true,
	}, nil)
	if err != nil {
		return nil, fmt.Errorf("connect MCP endpoint %q: %w", endpoint, err)
	}
	return remote, nil
}

// callMCPTool invokes one MCP tool and decodes its structured result. It keeps
// tool-level failures distinct from transport failures so both attach and list
// can report an unavailable host without hiding a server-side diagnostic.
func callMCPTool(ctx context.Context, client *protocol.ClientSession, name string, arguments map[string]any, destination any) error {
	result, err := client.CallTool(ctx, &protocol.CallToolParams{Name: name, Arguments: arguments})
	if err != nil {
		return fmt.Errorf("call MCP tool %q: %w", name, err)
	}
	if result.IsError {
		return fmt.Errorf("MCP tool %q: %s", name, resultMessage(result))
	}
	encoded, err := json.Marshal(result.StructuredContent)
	if err != nil {
		return fmt.Errorf("encode MCP tool %q result: %w", name, err)
	}
	if err := json.Unmarshal(encoded, destination); err != nil {
		return fmt.Errorf("decode MCP tool %q result: %w", name, err)
	}
	return nil
}

// resultMessage extracts the server's concise tool diagnostic without exposing
// protocol implementation details in normal CLI failures.
func resultMessage(result *protocol.CallToolResult) string {
	for _, content := range result.Content {
		if text, ok := content.(*protocol.TextContent); ok {
			return text.Text
		}
	}
	return "remote tool failed"
}

// writeDetachStatus distinguishes a local client detach from a Session close.
func writeDetachStatus(output io.Writer, lastOutputEndedLine bool) error {
	if !lastOutputEndedLine {
		if err := writeAll(output, []byte("\r\n")); err != nil {
			return err
		}
	}
	return writeAll(output, []byte("Detached.\r\n"))
}
