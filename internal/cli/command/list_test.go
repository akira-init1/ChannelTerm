package command

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/akira-init1/ChannelTerm/internal/core/app"
	"github.com/akira-init1/ChannelTerm/internal/core/config"
	serialtransport "github.com/akira-init1/ChannelTerm/internal/core/transport/serial"
)

func TestRunListHelpUsesCurrentSerialExample(t *testing.T) {
	sources := listSources{
		configPath:   func() (string, error) { return "config.toml", nil },
		loadConfig:   func(string) (config.File, error) { return config.File{}, nil },
		listPorts:    func() ([]serialtransport.Port, error) { return nil, nil },
		listSessions: func(context.Context, string) ([]mcpListedSession, error) { return nil, nil },
	}
	var output bytes.Buffer
	if err := runListWithSources(context.Background(), []string{"--help"}, &output, sources); err != nil {
		t.Fatalf("runListWithSources(--help) error = %v", err)
	}
	got := output.String()
	if !strings.Contains(got, "comma-separated transport names, for example serial") {
		t.Errorf("list help = %q, want current serial transport example", got)
	}
	if strings.Contains(got, "serial or ssh") {
		t.Errorf("list help = %q, must not present unimplemented SSH as a current example", got)
	}
}

func TestRunListMergesLocalSessionIntoTargetRow(t *testing.T) {
	sources := listSources{
		configPath: func() (string, error) { return "config.toml", nil },
		loadConfig: func(string) (config.File, error) {
			return config.File{Serial: config.Serial{Profiles: map[string]config.SerialProfile{
				"board": {Port: "COM8", BaudRate: 115200},
			}}}, nil
		},
		listPorts: func() ([]serialtransport.Port, error) {
			return []serialtransport.Port{{Name: "COM8"}}, nil
		},
		listSessions: func(context.Context, string) ([]mcpListedSession, error) {
			return []mcpListedSession{
				{ID: "serial-opaque", Reference: "SER-1", Transport: "serial", Endpoint: "COM8", State: "open"},
				{ID: "ssh-opaque", Reference: "SSH-1", Transport: "ssh", Endpoint: "board.example:22", State: "open"},
			}, nil
		},
	}
	var output bytes.Buffer
	if err := runListWithSources(context.Background(), []string{"--transport", "serial", "--json"}, &output, sources); err != nil {
		t.Fatalf("runListWithSources() error = %v", err)
	}
	var report listReport
	if err := json.Unmarshal(output.Bytes(), &report); err != nil {
		t.Fatalf("decode JSON output: %v\n%s", err, output.String())
	}
	if report.MCP.State != "online" || len(report.Items) != 1 {
		t.Fatalf("report = %#v, want one merged local target", report)
	}
	item := report.Items[0]
	if item.Reference != "SER-COM8" || item.MCPReference != "SER-1" || item.SessionID != "serial-opaque" {
		t.Errorf("merged item = %#v, want target and MCP session references", item)
	}
	if item.Occupancy != "owned by ChannelTerm" || item.Source != "local+config+mcp" || item.Label != "board" {
		t.Errorf("merged item = %#v, want ownership and combined sources", item)
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("encode report: %v", err)
	}
	if strings.Contains(string(encoded), "ssh-opaque") || strings.Contains(string(encoded), "board.example") {
		t.Errorf("serial-filtered report = %s, must not contain SSH session", encoded)
	}
}

func TestRunListShowsSessionLeaseOccupancy(t *testing.T) {
	sources := listSources{
		listPorts:    func() ([]serialtransport.Port, error) { return nil, nil },
		listProfiles: func(context.Context, string) ([]app.SerialProfileInfo, error) { return nil, nil },
		listSessions: func(context.Context, string) ([]mcpListedSession, error) {
			return []mcpListedSession{{ID: "session-id", Reference: "SER-1", Transport: "serial", Endpoint: "COM8", State: "open", Lease: &mcpSessionLease{Type: "file-transfer", State: "active"}}}, nil
		},
	}
	var output bytes.Buffer
	if err := runListWithSources(context.Background(), []string{"--kind", "session", "--json"}, &output, sources); err != nil {
		t.Fatalf("runListWithSources() error = %v", err)
	}
	var report listReport
	if err := json.Unmarshal(output.Bytes(), &report); err != nil {
		t.Fatalf("decode JSON output: %v", err)
	}
	if len(report.Items) != 1 || report.Items[0].Occupancy != "locked by file-transfer" {
		t.Errorf("list report = %#v, want file-transfer occupancy", report.Items)
	}
}

func TestRunListRendersTargetReferenceWithoutMCP(t *testing.T) {
	sources := listSources{
		configPath: func() (string, error) { return "config.toml", nil },
		loadConfig: func(string) (config.File, error) { return config.File{}, nil },
		listPorts: func() ([]serialtransport.Port, error) {
			return []serialtransport.Port{{Name: "COM9"}}, nil
		},
		listSessions: func(context.Context, string) ([]mcpListedSession, error) {
			t.Fatal("--no-mcp must skip session discovery")
			return nil, nil
		},
	}
	var output bytes.Buffer
	if err := runListWithSources(context.Background(), []string{"--kind", "device", "--no-mcp"}, &output, sources); err != nil {
		t.Fatalf("runListWithSources() error = %v", err)
	}
	got := output.String()
	for _, want := range []string{"SER-COM9", "COM9", "MCP SESSION", "-"} {
		if !strings.Contains(got, want) {
			t.Errorf("list output = %q, want %q", got, want)
		}
	}
}

func TestRunListLongFormatControlsFullSessionID(t *testing.T) {
	sources := listSources{
		configPath: func() (string, error) { return "config.toml", nil },
		loadConfig: func(string) (config.File, error) { return config.File{}, nil },
		listPorts: func() ([]serialtransport.Port, error) {
			return []serialtransport.Port{{Name: "COM8"}}, nil
		},
		listSessions: func(context.Context, string) ([]mcpListedSession, error) {
			return []mcpListedSession{{ID: "0123456789abcdef", Reference: "SER-1", Transport: "serial", Endpoint: "COM8", State: "open"}}, nil
		},
	}
	for _, tt := range []struct {
		name       string
		args       []string
		wantID     bool
		wantHeader bool
	}{
		{name: "compact", wantID: false, wantHeader: false},
		{name: "short long", args: []string{"-l"}, wantID: true, wantHeader: true},
		{name: "long", args: []string{"--long"}, wantID: true, wantHeader: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var output bytes.Buffer
			if err := runListWithSources(context.Background(), tt.args, &output, sources); err != nil {
				t.Fatalf("runListWithSources() error = %v", err)
			}
			got := output.String()
			if contains := strings.Contains(got, "0123456789abcdef"); contains != tt.wantID {
				t.Errorf("session ID presence = %t, want %t in %q", contains, tt.wantID, got)
			}
			if contains := strings.Contains(got, "SESSION ID"); contains != tt.wantHeader {
				t.Errorf("long header presence = %t, want %t in %q", contains, tt.wantHeader, got)
			}
		})
	}
}

func TestRunListDoesNotMergeRemoteMCPSessionWithLocalPort(t *testing.T) {
	sources := listSources{
		configPath: func() (string, error) { return "config.toml", nil },
		loadConfig: func(string) (config.File, error) { return config.File{}, nil },
		listPorts: func() ([]serialtransport.Port, error) {
			return []serialtransport.Port{{Name: "COM8"}}, nil
		},
		listSessions: func(context.Context, string) ([]mcpListedSession, error) {
			return []mcpListedSession{{ID: "remote-id", Reference: "SER-1", Transport: "serial", Endpoint: "COM8", State: "open"}}, nil
		},
	}
	var output bytes.Buffer
	if err := runListWithSources(context.Background(), []string{"--endpoint", "http://mcp.example.test/mcp", "--json"}, &output, sources); err != nil {
		t.Fatalf("runListWithSources() error = %v", err)
	}
	var report listReport
	if err := json.Unmarshal(output.Bytes(), &report); err != nil {
		t.Fatalf("decode JSON output: %v\n%s", err, output.String())
	}
	if len(report.Items) != 2 {
		t.Fatalf("report = %#v, want local device and remote MCP session", report)
	}
}

func TestRunListJSONSkipsExcludedLocalSources(t *testing.T) {
	localSourceCalled := false
	sources := listSources{
		configPath: func() (string, error) { return "config.toml", nil },
		loadConfig: func(string) (config.File, error) {
			localSourceCalled = true
			return config.File{}, errors.New("config source should not run")
		},
		listPorts: func() ([]serialtransport.Port, error) {
			localSourceCalled = true
			return nil, errors.New("port source should not run")
		},
		listSessions: func(context.Context, string) ([]mcpListedSession, error) {
			return []mcpListedSession{{ID: "ssh-opaque", Reference: "SSH-1", Transport: "ssh", Endpoint: "board.example:22", State: "open", Label: "board"}}, nil
		},
	}
	var output bytes.Buffer
	if err := runListWithSources(context.Background(), []string{"--kind", "session", "--transport", "ssh", "--json"}, &output, sources); err != nil {
		t.Fatalf("runListWithSources() error = %v", err)
	}
	if localSourceCalled {
		t.Error("SSH session filter invoked a local serial source")
	}
	var report listReport
	if err := json.Unmarshal(output.Bytes(), &report); err != nil {
		t.Fatalf("decode JSON output: %v\n%s", err, output.String())
	}
	if report.MCP.State != "online" || len(report.Items) != 1 {
		t.Fatalf("report = %#v, want online with one SSH session", report)
	}
	item := report.Items[0]
	if item.Reference != "SSH-1" || item.SessionID != "ssh-opaque" || item.Transport != "ssh" || item.Occupancy != "owned by ChannelTerm" {
		t.Errorf("JSON item = %#v, want SSH short reference and canonical ID", item)
	}
}

func TestRunListKeepsLocalResultsWhenMCPIsOffline(t *testing.T) {
	sources := listSources{
		configPath: func() (string, error) { return "config.toml", nil },
		loadConfig: func(string) (config.File, error) { return config.File{}, nil },
		listPorts: func() ([]serialtransport.Port, error) {
			return []serialtransport.Port{{Name: "COM9"}}, nil
		},
		// This fake models an absent MCP server; list must remain useful for local hardware.
		listSessions: func(context.Context, string) ([]mcpListedSession, error) {
			return nil, errors.New("connection refused")
		},
	}
	var output bytes.Buffer
	if err := runListWithSources(context.Background(), nil, &output, sources); err != nil {
		t.Fatalf("runListWithSources() error = %v", err)
	}
	if got := output.String(); !strings.Contains(got, "MCP: offline") || !strings.Contains(got, "COM9") {
		t.Errorf("offline list output = %q, want MCP offline and COM9", got)
	}
	if err := runListWithSources(context.Background(), []string{"--kind", "unknown"}, &output, sources); err == nil || !strings.Contains(err.Error(), "unsupported list kind") {
		t.Errorf("invalid kind error = %v, want unsupported list kind", err)
	}
}
