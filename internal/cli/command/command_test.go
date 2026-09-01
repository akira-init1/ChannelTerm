package command

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/core/config"
	"github.com/akira-init1/ChannelTerm/internal/core/connectionpolicy"
	"github.com/akira-init1/ChannelTerm/internal/core/device"
	"github.com/akira-init1/ChannelTerm/internal/core/session"
	serialtransport "github.com/akira-init1/ChannelTerm/internal/core/transport/serial"
)

// newSerialSession is retained only as a test dependency for validation paths
// that intentionally reach concrete serial construction. Production CLI calls
// construct sessions through core/app.Application.
func newSerialSession(config serialtransport.Config) (cliSession, error) {
	transport, err := serialtransport.New(config)
	if err != nil {
		return nil, err
	}
	return session.New(config.Port, transport)
}

func TestRun(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{name: "no command prints help"},
		{name: "help command", args: []string{"help"}},
		{name: "help flag", args: []string{"--help"}},
		{name: "short help flag", args: []string{"-h"}},
		{name: "unknown command", args: []string{"connectty"}, wantErr: "unknown command"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var output bytes.Buffer
			err := run(tt.args, &output)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("run() error = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("run() error = %v", err)
			}
			if !strings.Contains(output.String(), "Usage: channelterm") {
				t.Errorf("run() output = %q, want usage", output.String())
			}
		})
	}
}

func TestRunHelpDescribesSharedHumanAndAIAccess(t *testing.T) {
	var output bytes.Buffer
	if err := run([]string{"--help"}, &output); err != nil {
		t.Fatalf("run(--help) error = %v", err)
	}
	got := output.String()
	if !strings.Contains(got, "A shared terminal-session core for human and AI access.") {
		t.Errorf("run(--help) output = %q, want shared human and AI access description", got)
	}
	for _, unsupported := range []string{"SSH", "Telnet"} {
		if strings.Contains(got, unsupported) {
			t.Errorf("run(--help) output = %q, must not claim %s support", got, unsupported)
		}
	}
}

func TestRunPrintsVersion(t *testing.T) {
	for _, args := range [][]string{{"version"}, {"--version"}} {
		var output bytes.Buffer
		if err := run(args, &output); err != nil {
			t.Fatalf("run(%q) error = %v", args, err)
		}
		if got := output.String(); got != "channelterm "+version+"\n" {
			t.Errorf("run(%q) output = %q, want version", args, got)
		}
	}
}

func TestRunMCPHelpAndRejectsInvalidHTTPOptions(t *testing.T) {
	var output bytes.Buffer
	if err := runMCP(context.Background(), []string{"--help"}, &output); err != nil {
		t.Fatalf("runMCP(--help) error = %v", err)
	}
	if !strings.Contains(output.String(), "HTTP defaults to 127.0.0.1:37099/mcp") {
		t.Errorf("mcp help = %q, want default HTTP endpoint", output.String())
	}
	for _, option := range []string{"-transport", "-listen", "-path", "-connection-policy"} {
		if !strings.Contains(output.String(), option) {
			t.Errorf("mcp help = %q, want %s", output.String(), option)
		}
	}

	for _, tt := range []struct {
		args []string
		want string
	}{
		{args: []string{"--transport", "tcp"}, want: "unsupported MCP transport"},
		{args: []string{"--transport", "http", "--path", "mcp"}, want: "path must start"},
		{args: []string{"--connection-policy", "unexpected"}, want: "connection default policy is invalid"},
	} {
		if err := runMCP(context.Background(), tt.args, io.Discard); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("runMCP(%q) error = %v, want containing %q", tt.args, err, tt.want)
		}
	}
}

func TestResolveMCPConnectionPolicyAppliesCLIConfigAndDefaultPrecedence(t *testing.T) {
	tests := []struct {
		name        string
		configured  string
		override    string
		overrideSet bool
		want        connectionpolicy.Policy
	}{
		{name: "default", want: connectionpolicy.PolicyAsk},
		{name: "config", configured: "auto", want: connectionpolicy.PolicyAuto},
		{name: "CLI override", configured: "auto", override: "deny", overrideSet: true, want: connectionpolicy.PolicyDeny},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveMCPConnectionPolicy(config.File{Connection: config.Connection{DefaultPolicy: tt.configured}}, tt.override, tt.overrideSet)
			if err != nil {
				t.Fatalf("resolveMCPConnectionPolicy() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("resolveMCPConnectionPolicy() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRunMCPHTTPShutsDownOnContextCancellation(t *testing.T) {
	manager := session.NewManager()
	defer func() { _ = manager.Close() }()
	registry, err := newMCPRegistry(manager, newTestDeviceRegistry(t))
	if err != nil {
		t.Fatalf("newMCPRegistry() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var stderr bytes.Buffer
	if err := runMCPHTTP(ctx, registry, "127.0.0.1:0", "/mcp", &stderr); err != nil {
		t.Fatalf("runMCPHTTP() error = %v", err)
	}
	if !strings.Contains(stderr.String(), "MCP Streamable HTTP listening on http://127.0.0.1:") || !strings.Contains(stderr.String(), "/mcp") {
		t.Errorf("HTTP startup output = %q, want listening endpoint", stderr.String())
	}
}

func TestRunMCPHTTPWarnsWhenNetworkExposed(t *testing.T) {
	manager := session.NewManager()
	defer func() { _ = manager.Close() }()
	registry, err := newMCPRegistry(manager, newTestDeviceRegistry(t))
	if err != nil {
		t.Fatalf("newMCPRegistry() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var stderr bytes.Buffer
	if err := runMCPHTTP(ctx, registry, "0.0.0.0:0", "/mcp", &stderr); err != nil {
		t.Fatalf("runMCPHTTP() error = %v", err)
	}
	for _, warning := range []string{
		"Warning: MCP server is exposed to the network.",
		"Remote clients may control terminal sessions.",
		"Use only on a trusted network.",
	} {
		if !strings.Contains(stderr.String(), warning) {
			t.Errorf("network startup output = %q, want %q", stderr.String(), warning)
		}
	}
}

func TestMCPHTTPAddressSafety(t *testing.T) {
	for _, tt := range []struct {
		address string
		want    bool
	}{
		{address: "127.0.0.1:37099", want: true},
		{address: "[::1]:37099", want: true},
		{address: "0.0.0.0:37099", want: false},
		{address: "192.168.1.20:37099", want: false},
	} {
		if got := isLoopbackListen(tt.address); got != tt.want {
			t.Errorf("isLoopbackListen(%q) = %t, want %t", tt.address, got, tt.want)
		}
	}
}

func TestDefaultMCPListen(t *testing.T) {
	if defaultMCPListen != "127.0.0.1:37099" {
		t.Errorf("defaultMCPListen = %q, want 127.0.0.1:37099", defaultMCPListen)
	}
}

// newTestDeviceRegistry supplies MCP construction tests with discovery state
// while deliberately returning no endpoints and never opening a serial port.
func newTestDeviceRegistry(t *testing.T) *device.Registry {
	t.Helper()
	registry, err := device.NewRegistry(device.ScannerFunc(func(context.Context) ([]device.Endpoint, error) {
		return nil, nil
	}))
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	t.Cleanup(registry.Close)
	return registry
}

func TestSerialDeviceRegistryPreservesEndpointsWhenMetadataIsEmpty(t *testing.T) {
	registry, err := newSerialDeviceRegistry(func() ([]serialtransport.Port, error) {
		return []serialtransport.Port{
			{Name: "COM6", DeviceMetadata: serialtransport.DeviceMetadata{VID: "0403", PID: "6010", USBSerial: "ABC123"}},
			{Name: "COM8", DeviceMetadata: serialtransport.DeviceMetadata{VID: "1a86", PID: "7523"}},
			{Name: "/dev/ttyS0"},
		}, nil
	})
	if err != nil {
		t.Fatalf("newSerialDeviceRegistry() error = %v", err)
	}
	defer registry.Close()
	if err := registry.Start(context.Background()); err != nil {
		t.Fatalf("Registry.Start() error = %v", err)
	}
	devices := registry.List()
	if len(devices) != 3 || devices[0].Endpoint != "/dev/ttyS0" || devices[1].Endpoint != "COM6" || devices[2].Endpoint != "COM8" {
		t.Fatalf("List() = %#v, want three present endpoints", devices)
	}
	if got := devices[1].Metadata; got.VID != "0403" || got.PID != "6010" || got.USBSerial != "ABC123" {
		t.Errorf("COM6 metadata = %#v, want full provided metadata", got)
	}
	if got := devices[2]; got.State != device.StatePresent || got.Metadata.USBSerial != "" || got.Metadata.VID != "1a86" {
		t.Errorf("COM8 = %#v, want present endpoint despite missing USB serial", got)
	}
	if got := devices[0]; got.State != device.StatePresent || got.Metadata != (device.SerialMetadata{}) {
		t.Errorf("non-USB endpoint = %#v, want present with empty metadata", got)
	}
}

func TestRunSerialForwardsOutputAndClosesOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	terminal := &fakeCLISession{onFirstOutput: cancel}
	var gotConfig serialtransport.Config
	newSession := func(config serialtransport.Config) (cliSession, error) {
		gotConfig = config
		return terminal, nil
	}
	var output bytes.Buffer
	err := runWithIO(ctx, serialArgsWithConfig(t,
		"serial", "--port", "COM3", "--baud", "115200", "--data-bits", "7", "--parity", "even", "--stop-bits", "2",
	), strings.NewReader("status\n"), &output, newSession)
	if err != nil {
		t.Fatalf("runWithIO() error = %v", err)
	}
	if got := output.String(); got != "Connected: COM3 @ 115200\r\nNo wake character sent by default; use --wake for an idle shell without a prompt.\r\nEscape: Ctrl+]  |  Help: Ctrl+] ?  |  Prompt time: Ctrl+] t\r\nready\nDisconnected.\r\n" {
		t.Errorf("output = %q, want CRLF status, unchanged serial data, and disconnect status", got)
	}
	wantConfig := serialtransport.Config{Port: "COM3", BaudRate: 115200, DataBits: 7, Parity: serialtransport.ParityEven, StopBits: serialtransport.StopBitsTwo, FlowControl: serialtransport.FlowControlNone}
	if gotConfig != wantConfig {
		t.Errorf("serial config = %+v, want %+v", gotConfig, wantConfig)
	}
	if terminal.connectCount() != 1 || terminal.closeCount() != 1 {
		t.Errorf("Connect/Close calls = %d/%d, want 1/1", terminal.connectCount(), terminal.closeCount())
	}
}

func TestRunSerialSeparatesDisconnectStatusFromUnterminatedRemoteOutput(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	terminal := &fakeCLISession{outputData: []byte("root@board:~# "), onFirstOutput: cancel}
	var output bytes.Buffer
	err := runWithIO(ctx, serialArgsWithConfig(t, "serial", "--port", "COM8"), strings.NewReader(""), &output, func(serialtransport.Config) (cliSession, error) {
		return terminal, nil
	})
	if err != nil {
		t.Fatalf("runWithIO() error = %v", err)
	}
	if got := output.String(); got != "Connected: COM8 @ 115200\r\nNo wake character sent by default; use --wake for an idle shell without a prompt.\r\nEscape: Ctrl+]  |  Help: Ctrl+] ?  |  Prompt time: Ctrl+] t\r\nroot@board:~# \r\nDisconnected.\r\n" {
		t.Errorf("output = %q, want disconnect status on a new line", got)
	}
}

func TestRunSerialHighlightAlwaysStylesOnlyTerminalOutput(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	terminal := &fakeCLISession{outputData: []byte("driver is not open\n"), onFirstOutput: cancel}
	var output bytes.Buffer
	err := runWithIO(ctx, serialArgsWithConfig(t, "serial", "--port", "COM8", "--highlight", "always"), strings.NewReader(""), &output, func(serialtransport.Config) (cliSession, error) {
		return terminal, nil
	})
	if err != nil {
		t.Fatalf("runWithIO() error = %v", err)
	}
	const highlighted = "driver is \x1b[1;91mnot open\x1b[0m\n"
	if !strings.Contains(output.String(), highlighted) {
		t.Errorf("output = %q, want highlighted terminal phrase %q", output.String(), highlighted)
	}
	if strings.Contains(output.String(), "\x1b[1;91mConnected:") {
		t.Errorf("output = %q, want local connection status to remain plain", output.String())
	}
}

func TestRunSerialHighlightNeverPreservesTerminalBytes(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	terminal := &fakeCLISession{outputData: []byte("driver is not open\n"), onFirstOutput: cancel}
	var output bytes.Buffer
	err := runWithIO(ctx, serialArgsWithConfig(t, "serial", "--port", "COM8", "--highlight", "never"), strings.NewReader(""), &output, func(serialtransport.Config) (cliSession, error) {
		return terminal, nil
	})
	if err != nil {
		t.Fatalf("runWithIO() error = %v", err)
	}
	if !strings.Contains(output.String(), "driver is not open\n") || strings.Contains(output.String(), "\x1b[") {
		t.Errorf("output = %q, want unchanged terminal bytes", output.String())
	}
}

func TestRunSerialReturnsOpenDiagnostic(t *testing.T) {
	source := errors.New("serial port \"COM8\" may be in use by another program: Access is denied.")
	terminal := &fakeCLISession{connectErr: source}
	err := runWithIO(context.Background(), serialArgsWithConfig(t, "serial", "--port", "COM8"), strings.NewReader(""), io.Discard, func(serialtransport.Config) (cliSession, error) {
		return terminal, nil
	})
	if err == nil || !strings.Contains(err.Error(), "connect serial port \"COM8\": serial port \"COM8\" may be in use by another program") {
		t.Fatalf("runWithIO() error = %v, want Windows port-in-use guidance", err)
	}
	if !errors.Is(err, source) {
		t.Errorf("runWithIO() error = %v, does not retain source error %v", err, source)
	}
}

func TestRunSerialDoesNotWakeRemoteByDefault(t *testing.T) {
	tests := []struct {
		name   string
		output string
	}{
		{name: "idle Linux shell", output: "root@imx6ull:~# "},
		{name: "Linux login prompt", output: "login: "},
		{name: "Linux boot log", output: "Starting kernel ...\r\n"},
		{name: "U-Boot prompt", output: "U-Boot=> "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			terminal := &fakeCLISession{outputData: []byte(tt.output), onFirstOutput: cancel}
			var output bytes.Buffer
			err := runWithIO(ctx, serialArgsWithConfig(t, "serial", "--port", "COM8", "--baud", "115200"), strings.NewReader(""), &output, func(serialtransport.Config) (cliSession, error) {
				return terminal, nil
			})
			if err != nil {
				t.Fatalf("runWithIO() error = %v", err)
			}
			if got := terminal.writtenData(); len(got) != 0 {
				t.Errorf("default connection wrote %q, want no remote input", got)
			}
			if !strings.HasPrefix(output.String(), "Connected: COM8 @ 115200\r\nNo wake character sent by default; use --wake for an idle shell without a prompt.\r\nEscape: Ctrl+]  |  Help: Ctrl+] ?  |  Prompt time: Ctrl+] t\r\n") {
				t.Errorf("output = %q, want connection status", output.String())
			}
		})
	}
}

func TestRunSerialWakeSendsExactlyOneCarriageReturn(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	terminal := &fakeCLISession{onFirstOutput: cancel}
	var output bytes.Buffer
	err := runWithIO(ctx, serialArgsWithConfig(t, "serial", "--port", "COM8", "--baud", "115200", "--wake"), strings.NewReader(""), &output, func(serialtransport.Config) (cliSession, error) {
		return terminal, nil
	})
	if err != nil {
		t.Fatalf("runWithIO() error = %v", err)
	}
	if got := terminal.writtenData(); !bytes.Equal(got, []byte{0x0D}) {
		t.Errorf("wake input = %x, want 0d", got)
	}
}

func TestRunSerialRejectsInvalidArguments(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "missing port", args: []string{"serial"}, want: "serial port is required"},
		{name: "bad parity", args: []string{"serial", "--port", "COM3", "--parity", "bad"}, want: "serial parity"},
		{name: "unexpected positional", args: []string{"serial", "--port", "COM3", "extra"}, want: "unexpected serial argument"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var output bytes.Buffer
			err := runWithIO(context.Background(), serialArgsWithConfig(t, tt.args...), strings.NewReader(""), &output, newSerialSession)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("runWithIO() error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestRunSerialHelp(t *testing.T) {
	var output bytes.Buffer
	if err := runWithIO(context.Background(), []string{"serial", "--help"}, strings.NewReader(""), &output, newSerialSession); err != nil {
		t.Fatalf("runWithIO() error = %v", err)
	}
	for _, option := range []string{"-port", "-baud", "-data-bits", "-parity", "-stop-bits", "-wake", "-highlight", "-profile", "-config", "-save"} {
		if !strings.Contains(output.String(), option) {
			t.Errorf("output = %q, want %s", output.String(), option)
		}
	}
	if !strings.Contains(output.String(), "Usage: channelterm serial") {
		t.Errorf("output = %q, want serial usage", output.String())
	}
	for _, explanation := range []string{
		"By default, connecting does not send any characters to the serial device.",
		"Use --wake when an already-open shell has no output prompt.",
	} {
		if !strings.Contains(output.String(), explanation) {
			t.Errorf("output = %q, want explanation %q", output.String(), explanation)
		}
	}
}

func TestRunSerialResolvesProfilesAndCLIOverrides(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	file := config.File{Serial: config.Serial{
		Default: "imx6ull",
		Profiles: map[string]config.SerialProfile{
			"imx6ull": {Port: "/dev/ttyUSB0", BaudRate: 115200},
			"zynq":    {Port: "/dev/ttyUSB1", BaudRate: 57600, DataBits: 7, Parity: "even", StopBits: "2"},
		},
	}}
	if err := config.Save(path, file); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	tests := []struct {
		name string
		args []string
		want serialtransport.Config
	}{
		{name: "default profile", args: []string{"serial", "--config", path}, want: serialtransport.Config{Port: "/dev/ttyUSB0", BaudRate: 115200, DataBits: 8, Parity: serialtransport.ParityNone, StopBits: serialtransport.StopBitsOne, FlowControl: serialtransport.FlowControlNone}},
		{name: "selected profile with CLI baud", args: []string{"serial", "--config", path, "--profile", "zynq", "--baud", "9600"}, want: serialtransport.Config{Port: "/dev/ttyUSB1", BaudRate: 9600, DataBits: 7, Parity: serialtransport.ParityEven, StopBits: serialtransport.StopBitsTwo, FlowControl: serialtransport.FlowControlNone}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			terminal := &fakeCLISession{onFirstOutput: cancel}
			var got serialtransport.Config
			err := runWithIO(ctx, tt.args, strings.NewReader(""), io.Discard, func(value serialtransport.Config) (cliSession, error) {
				got = value
				return terminal, nil
			})
			if err != nil {
				t.Fatalf("runWithIO() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("serial configuration = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestRunSerialSaveWritesNamedProfile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "custom.toml")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	terminal := &fakeCLISession{onFirstOutput: cancel}
	err := runWithIO(ctx, []string{"serial", "--config", path, "--port", "COM9", "--baud", "9600", "--wake", "--save", "lab"}, strings.NewReader(""), io.Discard, func(serialtransport.Config) (cliSession, error) {
		return terminal, nil
	})
	if err != nil {
		t.Fatalf("runWithIO() error = %v", err)
	}
	file, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if file.Serial.Default != "lab" {
		t.Errorf("default profile = %q, want lab after first save", file.Serial.Default)
	}
	profile, err := file.ResolveSerial("lab")
	if err != nil {
		t.Fatalf("ResolveSerial(lab) error = %v", err)
	}
	if profile.Port != "COM9" || profile.BaudRate != 9600 || !profile.Wake {
		t.Errorf("saved profile = %+v, want saved CLI values", profile)
	}
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	terminal = &fakeCLISession{onFirstOutput: cancel}
	var got serialtransport.Config
	err = runWithIO(ctx, []string{"serial", "--config", path}, strings.NewReader(""), io.Discard, func(value serialtransport.Config) (cliSession, error) {
		got = value
		return terminal, nil
	})
	if err != nil {
		t.Fatalf("runWithIO() with saved default error = %v", err)
	}
	if got.Port != "COM9" || got.BaudRate != 9600 {
		t.Errorf("default saved configuration = %+v, want lab profile", got)
	}
}

func TestRunSerialDoesNotWriteExistingConfigWithoutSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	before := "# Keep this file unchanged.\n\n[serial]\n"
	if err := os.WriteFile(path, []byte(before), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	terminal := &fakeCLISession{onFirstOutput: cancel}
	err := runWithIO(ctx, []string{"serial", "--config", path, "--port", "COM3"}, strings.NewReader(""), io.Discard, func(serialtransport.Config) (cliSession, error) {
		return terminal, nil
	})
	if err != nil {
		t.Fatalf("runWithIO() error = %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(after) != before {
		t.Errorf("configuration after normal connection = %q, want %q", after, before)
	}
}

func TestRunSerialRejectsUnknownProfile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := config.Save(path, config.File{}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	err := runWithIO(context.Background(), []string{"serial", "--config", path, "--profile", "missing"}, strings.NewReader(""), io.Discard, newSerialSession)
	if err == nil || !strings.Contains(err.Error(), "serial profile not found") {
		t.Fatalf("runWithIO() error = %v, want unknown profile error", err)
	}
}

func serialArgsWithConfig(t *testing.T, args ...string) []string {
	t.Helper()
	return append(args, "--config", filepath.Join(t.TempDir(), "config.toml"))
}

func TestRunSerialShortHelp(t *testing.T) {
	var output bytes.Buffer
	if err := runWithIO(context.Background(), []string{"serial", "-h"}, strings.NewReader(""), &output, newSerialSession); err != nil {
		t.Fatalf("runWithIO() error = %v", err)
	}
	if !strings.Contains(output.String(), "Usage: channelterm serial") {
		t.Errorf("output = %q, want serial usage", output.String())
	}
}

func TestForwardInputWritesAllBytes(t *testing.T) {
	terminal := &fakeCLISession{}
	done := make(chan struct{})
	go func() {
		forwardInput(strings.NewReader("first\rsecond\n"), terminal, func([]byte) error { return nil }, func() {})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("forwardInput() did not finish")
	}
	if got := string(terminal.writtenData()); got != "first\rsecond\n" {
		t.Errorf("written input = %q, want first\\rsecond\\n", got)
	}
}

func TestForwardInputForwardsControlCWithoutCancelling(t *testing.T) {
	terminal := &fakeCLISession{}
	var local bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	forwardInput(strings.NewReader("ls\r\x03ignored"), terminal, func(data []byte) error {
		_, err := local.Write(data)
		return err
	}, cancel)
	if ctx.Err() != nil {
		t.Fatal("forwardInput() cancelled on Ctrl+C")
	}
	if got := string(terminal.writtenData()); got != "ls\r\x03ignored" {
		t.Errorf("written input = %q, want ls\\r\\x03ignored", got)
	}
	if got := local.String(); got != "" {
		t.Errorf("local output = %q, want no escape feedback for Ctrl+C", got)
	}
}

// TestForwardInputControlCCancelsEscapePending verifies that Ctrl+C both exits
// local escape mode and reaches the remote Session without unknown-command
// feedback.
func TestForwardInputControlCCancelsEscapePending(t *testing.T) {
	terminal := &fakeCLISession{}
	var local bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	forwardInput(strings.NewReader("\x1d\x03next"), terminal, func(data []byte) error {
		_, err := local.Write(data)
		return err
	}, cancel)
	if ctx.Err() != nil {
		t.Fatal("forwardInput() cancelled on Ctrl+C during escape mode")
	}
	if got := string(terminal.writtenData()); got != "\x03next" {
		t.Errorf("written input = %q, want remote Ctrl+C followed by normal input", got)
	}
	if got := local.String(); got != string(escapePendingText) {
		t.Errorf("local output = %q, want only escape-pending feedback", got)
	}
}

// TestForwardInputDisplaysEscapePendingLocally verifies that an escape prefix
// produces presentation-only feedback and no Session write.
func TestForwardInputDisplaysEscapePendingLocally(t *testing.T) {
	terminal := &fakeCLISession{}
	var local bytes.Buffer
	forwardInput(strings.NewReader("\x1d"), terminal, func(data []byte) error {
		_, err := local.Write(data)
		return err
	}, func() {})
	if got := terminal.writtenData(); len(got) != 0 {
		t.Errorf("written input = %q, want no remote input", got)
	}
	const want = "\r\n[ChannelTerm] Escape: q quit | ? help | ] send Ctrl+] | t prompt time | Esc cancel\r\n"
	if got := local.String(); got != want {
		t.Errorf("local output = %q, want %q", got, want)
	}
}

// TestForwardInputCancelsEscapeLocally verifies that Ctrl+] followed by Esc
// produces only local feedback, writes neither control byte remotely, and
// leaves subsequent input in normal remote-input mode.
func TestForwardInputCancelsEscapeLocally(t *testing.T) {
	terminal := &fakeCLISession{}
	var local bytes.Buffer
	forwardInput(strings.NewReader("\x1d\x1bnext"), terminal, func(data []byte) error {
		_, err := local.Write(data)
		return err
	}, func() {})
	if got, want := string(terminal.writtenData()), "next"; got != want {
		t.Errorf("remote input = %q, want %q", got, want)
	}
	if got, want := local.String(), string(escapePendingText)+string(escapeCancelledText); got != want {
		t.Errorf("local output = %q, want %q", got, want)
	}
}

// TestForwardInputCancelsWhenEscapeCancelledOutputFails verifies a failed
// cancellation notice stops the local input bridge without leaking Esc to the
// remote Session.
func TestForwardInputCancelsWhenEscapeCancelledOutputFails(t *testing.T) {
	terminal := &fakeCLISession{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	writes := 0
	forwardInput(strings.NewReader("\x1d\x1b"), terminal, func([]byte) error {
		writes++
		if writes == 2 {
			return errors.New("cancel feedback failed")
		}
		return nil
	}, cancel)
	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatal("forwardInput() did not cancel after cancellation feedback failed")
	}
	if got := terminal.writtenData(); len(got) != 0 {
		t.Errorf("remote input = %x, want no escape bytes", got)
	}
}

// TestForwardInputHandlesEscapeCommands verifies that the CLI bridge writes
// only remote payload bytes and reserves escape actions for local handling.
func TestForwardInputHandlesEscapeCommands(t *testing.T) {
	terminal := &fakeCLISession{}
	var local bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	forwardInput(strings.NewReader("a\x1d?b\x1d]c\x1dxd\x1dqignored"), terminal, func(data []byte) error {
		_, err := local.Write(data)
		return err
	}, cancel)
	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatal("forwardInput() did not cancel on Ctrl+] q")
	}
	if got := string(terminal.writtenData()); got != "ab\x1dcd" {
		t.Errorf("written input = %q, want ab\\x1dcd", got)
	}
	if got := local.String(); strings.Count(got, string(escapePendingText)) != 4 || !strings.Contains(got, "ChannelTerm escape commands:") || !strings.Contains(got, "Esc  Cancel escape mode") || !strings.Contains(got, "Unknown escape command 'x'") {
		t.Errorf("local escape output = %q, want pending prompts plus help and unknown command messages", got)
	}
}

// TestForwardInputTogglesPromptTimestampsLocally verifies that the new escape
// action never becomes a Session write and leaves ordinary input untouched.
func TestForwardInputTogglesPromptTimestampsLocally(t *testing.T) {
	terminal := &fakeCLISession{}
	var local bytes.Buffer
	toggles := 0
	forwardInputWithPromptTimestamp(strings.NewReader("a\x1dtb\x1dtc"), terminal, func(data []byte) error {
		_, err := local.Write(data)
		return err
	}, func() error {
		toggles++
		_, err := local.Write(promptTimestampStatusText(toggles%2 == 1))
		return err
	}, func() {})
	if got, want := string(terminal.writtenData()), "abc"; got != want {
		t.Errorf("remote input = %q, want %q", got, want)
	}
	if toggles != 2 {
		t.Errorf("toggle count = %d, want 2", toggles)
	}
	if got := local.String(); !strings.Contains(got, "Prompt timestamps: ON") || !strings.Contains(got, "Prompt timestamps: OFF") {
		t.Errorf("local output = %q, want ON and OFF status", got)
	}
}

// TestForwardInputCancelsWhenLocalEscapeOutputFails ensures a failed local
// console write cannot leave a raw-mode Session running without its input loop.
func TestForwardInputCancelsWhenLocalEscapeOutputFails(t *testing.T) {
	terminal := &fakeCLISession{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	forwardInput(strings.NewReader("\x1d?"), terminal, func([]byte) error {
		return errors.New("local output failed")
	}, cancel)
	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatal("forwardInput() did not cancel after local escape output failed")
	}
	if got := terminal.writtenData(); len(got) != 0 {
		t.Errorf("written input = %q, want no remote input", got)
	}
}

func TestWriteAllPreservesCRLF(t *testing.T) {
	var output bytes.Buffer
	if err := writeAll(&output, []byte("line one\r\nline two\r\n")); err != nil {
		t.Fatalf("writeAll() error = %v", err)
	}
	if got := output.String(); got != "line one\r\nline two\r\n" {
		t.Errorf("output = %q, want exact CRLF", got)
	}
}

func TestWriteConnectionStatusUsesCRLF(t *testing.T) {
	var output bytes.Buffer
	if err := writeConnectionStatus(&output, "/dev/ttyUSB0", 115200); err != nil {
		t.Fatalf("writeConnectionStatus() error = %v", err)
	}
	if got := output.String(); got != "Connected: /dev/ttyUSB0 @ 115200\r\nNo wake character sent by default; use --wake for an idle shell without a prompt.\r\nEscape: Ctrl+]  |  Help: Ctrl+] ?  |  Prompt time: Ctrl+] t\r\n" {
		t.Errorf("status = %q, want explicit CRLF delimiters", got)
	}
}

func TestWriteTargetReferenceStatusUsesCRLF(t *testing.T) {
	var output bytes.Buffer
	if err := writeTargetReferenceStatus(&output, "SER-COM8"); err != nil {
		t.Fatalf("writeTargetReferenceStatus() error = %v", err)
	}
	if got := output.String(); got != "Target: SER-COM8\r\n" {
		t.Errorf("target status = %q, want explicit CRLF delimiters", got)
	}
}

func TestResolveSerialTargetReference(t *testing.T) {
	tests := []struct {
		name      string
		reference string
		ports     []serialtransport.Port
		want      string
		wantErr   string
	}{
		{name: "listed Windows port", reference: "SER-COM8", ports: []serialtransport.Port{{Name: "COM8"}}, want: "COM8"},
		{name: "case insensitive reference", reference: "ser-com8", ports: []serialtransport.Port{{Name: "COM8"}}, want: "COM8"},
		{name: "missing port", reference: "SER-COM9", ports: []serialtransport.Port{{Name: "COM8"}}, wantErr: "is not present"},
		{name: "future transport", reference: "SSH-1", wantErr: "currently only SER-*"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveSerialTargetReference(tt.reference, func() ([]serialtransport.Port, error) {
				return tt.ports, nil
			})
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("resolveSerialTargetReference() error = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveSerialTargetReference() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("resolveSerialTargetReference() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestWriteDisconnectStatusUsesOneLineBreak(t *testing.T) {
	tests := []struct {
		name                string
		lastOutputEndedLine bool
		want                string
	}{
		{name: "terminated remote output", lastOutputEndedLine: true, want: "Disconnected.\r\n"},
		{name: "unterminated remote output", want: "\r\nDisconnected.\r\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var output bytes.Buffer
			if err := writeDisconnectStatus(&output, tt.lastOutputEndedLine); err != nil {
				t.Fatalf("writeDisconnectStatus() error = %v", err)
			}
			if got := output.String(); got != tt.want {
				t.Errorf("disconnect status = %q, want %q", got, tt.want)
			}
		})
	}
}

// fakeCLISession returns one configurable output chunk and then blocks until
// cancellation, allowing CLI tests to control connection and shutdown timing.
type fakeCLISession struct {
	mu sync.Mutex

	outputSent    bool
	onFirstOutput func()
	outputData    []byte
	connectErr    error
	written       []byte
	connects      int
	closes        int
}

func (s *fakeCLISession) ID() string { return "test" }

func (s *fakeCLISession) State() session.SessionState { return session.StateOpen }

func (s *fakeCLISession) Connect(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.connects++
	return s.connectErr
}

func (s *fakeCLISession) ReadOutput(ctx context.Context, next session.OutputCursor, maxBytes int) (session.OutputChunk, error) {
	s.mu.Lock()
	if !s.outputSent {
		s.outputSent = true
		onFirstOutput := s.onFirstOutput
		s.mu.Unlock()
		if onFirstOutput != nil {
			onFirstOutput()
		}
		data := s.outputData
		if data == nil {
			data = []byte("ready\n")
		}
		return session.OutputChunk{Data: data, Next: session.OutputCursor(len(data))}, nil
	}
	s.mu.Unlock()
	<-ctx.Done()
	return session.OutputChunk{}, ctx.Err()
}

func (s *fakeCLISession) ReadRecent(int) (session.OutputChunk, error) {
	return session.OutputChunk{}, io.EOF
}

// ReadActivity keeps unrelated serial CLI tests focused on output forwarding.
func (s *fakeCLISession) ReadActivity(ctx context.Context, _ session.ActivityCursor, _ int) (session.ActivityChunk, error) {
	<-ctx.Done()
	return session.ActivityChunk{}, ctx.Err()
}

// ReadRecentActivity reports an empty activity stream for serial CLI tests.
func (*fakeCLISession) ReadRecentActivity(int) (session.ActivityChunk, error) {
	return session.ActivityChunk{}, nil
}

func (s *fakeCLISession) Write(request session.WriteRequest) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.written = append(s.written, request.Data...)
	return len(request.Data), nil
}

func (s *fakeCLISession) Resize(uint16, uint16) error { return errors.New("not implemented") }

func (s *fakeCLISession) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closes++
	return nil
}

func (s *fakeCLISession) connectCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.connects
}

func (s *fakeCLISession) closeCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closes
}

func (s *fakeCLISession) writtenData() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]byte(nil), s.written...)
}
