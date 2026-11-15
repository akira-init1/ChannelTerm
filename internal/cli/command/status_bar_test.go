package command

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/cli/terminalinput"
	"github.com/akira-init1/ChannelTerm/internal/core/session"
	protocol "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/muesli/termenv"
)

func testStatusBar(out io.Writer) *statusBar {
	return &statusBar{output: out, size: terminalinput.Size{Columns: 80, Rows: 24}, active: true, text: "board | unknown"}
}

func TestStatusBarSplitSequencesAndUnicode(t *testing.T) {
	payload := []byte("prompt> 中文 e\x1b[31mred\x1b[0m\x1b[2D\x1b[K\r\n\x1b[?2004h")
	for split := 0; split <= len(payload); split++ {
		var out bytes.Buffer
		b := testStatusBar(&out)
		if err := b.update(b.text); err != nil {
			t.Fatal(err)
		}
		out.Reset()
		if _, err := b.Write(payload[:split]); err != nil {
			t.Fatal(err)
		}
		if _, err := b.Write(payload[split:]); err != nil {
			t.Fatal(err)
		}
		if !b.active || !bytes.Equal(out.Bytes(), payload) {
			t.Fatalf("split %d: %q", split, out.String())
		}
	}
}

func TestStatusBarUnsupportedControlsRestoreBeforeForwarding(t *testing.T) {
	for _, control := range []string{"\x1b[2J", "\x1b[H", "\x1b[24;1H", "\x1b[?1049h", "\x1b[?6h", "\x1b[1;24r", "\x1b]0;title\a", "\x1b7", "\x1b[" + strings.Repeat("1", 80), "\v", "\u009b2J", string([]byte{0xff})} {
		for split := 0; split <= len(control); split++ {
			var out bytes.Buffer
			b := testStatusBar(&out)
			_ = b.update(b.text)
			out.Reset()
			_, _ = b.Write([]byte(control[:split]))
			_, _ = b.Write([]byte(control[split:]))
			if b.active || !strings.HasSuffix(out.String(), control) {
				t.Fatalf("not preserved: %q -> %q", control, out.String())
			}
			if strings.Index(out.String(), "\x1b[r") > len(out.String())-len(control) {
				t.Fatal("restoration after remote control")
			}
			out.Reset()
			_ = b.update("must not redraw")
			_, _ = b.Write([]byte("full screen\x1b[H"))
			if out.String() != "full screen\x1b[H" {
				t.Fatal("disabled writer changed bytes")
			}
		}
	}
}

func TestStatusBarResizeAndCloseDoNotEraseReflowedOutput(t *testing.T) {
	var out bytes.Buffer
	b := testStatusBar(&out)
	_ = b.update(b.text)
	out.Reset()
	b.measure = func() (terminalinput.Size, error) { return terminalinput.Size{Columns: 100, Rows: 30}, nil }
	_ = b.update("changed")
	if b.active || strings.Contains(out.String(), "[24;1H") || strings.Contains(out.String(), "[2K") {
		t.Fatal(out.String())
	}
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
	closed := out.String()
	_ = b.Close()
	if out.String() != closed {
		t.Fatal("duplicate cleanup wrote bytes")
	}
	_, _ = b.Write([]byte("after"))
	if !strings.HasSuffix(out.String(), "after") {
		t.Fatal("caller-owned output closed")
	}
}

type statusFailOnce struct {
	bytes.Buffer
	fail bool
}

func (w *statusFailOnce) Write(p []byte) (int, error) {
	if w.fail {
		w.fail = false
		return 0, io.ErrClosedPipe
	}
	return w.Buffer.Write(p)
}
func TestStatusBarRestoresEvenWhenClearingFooterFails(t *testing.T) {
	out := &statusFailOnce{}
	b := testStatusBar(out)
	_ = b.update(b.text)
	out.Reset()
	out.fail = true
	if err := b.Close(); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "\x1b[r") {
		t.Fatal("failed clear skipped scroll-region restoration")
	}
}

func TestStatusBarPendingTailAndConcurrentUpdates(t *testing.T) {
	var out bytes.Buffer
	b := testStatusBar(&out)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Go(func() {
			for j := 0; j < 50; j++ {
				_, _ = b.Write([]byte("line\r\n"))
				_ = b.update("board | observer")
			}
		})
	}
	wg.Wait()
	_, _ = b.Write([]byte("\x1b["))
	_ = b.Close()
	if !strings.HasSuffix(out.String(), "\x1b[") {
		t.Fatal("incomplete original bytes lost")
	}
	var untouched bytes.Buffer
	neverStarted := testStatusBar(&untouched)
	_ = neverStarted.Close()
	_, _ = neverStarted.Write([]byte("plain"))
	if untouched.String() != "plain" {
		t.Fatal("closed unstarted footer activated")
	}
}

func TestStatusBarWidthSanitizationAndOptOut(t *testing.T) {
	for _, s := range []string{"中文界界", "e\u0301👨‍👩‍👧‍👦", "untrusted\x1b[2J\n\u202ealias"} {
		got := statusCells(s, 7)
		if termenv.String(got).Width() > 7 || strings.ContainsAny(got, "\x1b\n\u202e") {
			t.Fatalf("unsafe footer %q", got)
		}
	}
	var out bytes.Buffer
	if newConsoleStatusBar(strings.NewReader("input"), &out, true) != nil {
		t.Fatal("pipes gained footer")
	}
	flags := flag.NewFlagSet("test", flag.ContinueOnError)
	options := defineTerminalFlags(flags)
	if !options.statusBar {
		t.Fatal("interactive default disabled")
	}
	if err := flags.Parse([]string{"--status-bar=false"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(options.args(), " "), "--status-bar=false") {
		t.Fatal("target-first forwarding lost opt-out")
	}
}

func TestStatusBarPollingCancellationAndUnknownState(t *testing.T) {
	var out bytes.Buffer
	b := testStatusBar(&out)
	_ = b.update("owner=old; input allowed")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stop := runStatusBar(ctx, b, func(ctx context.Context) (string, error) { return "", errors.New("secret diagnostic") }, cancel)
	deadline := time.Now().Add(time.Second)
	for {
		b.mu.Lock()
		unknown := strings.Contains(b.text, "unknown")
		b.mu.Unlock()
		if unknown {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("stale controller retained")
		}
		time.Sleep(time.Millisecond)
	}
	stop()
	if strings.Contains(out.String(), "secret diagnostic") {
		t.Fatal("raw diagnostic exposed")
	}
	started := make(chan struct{})
	stop = runStatusBar(ctx, b, func(ctx context.Context) (string, error) { close(started); <-ctx.Done(); return "", ctx.Err() }, cancel)
	<-started
	now := time.Now()
	stop()
	if time.Since(now) > 250*time.Millisecond {
		t.Fatal("slow Host blocked cancellation")
	}
}

type statusListClient struct {
	result any
	calls  int
}

func (c *statusListClient) Close() error { return nil }
func (c *statusListClient) CallTool(_ context.Context, q *protocol.CallToolParams) (*protocol.CallToolResult, error) {
	c.calls++
	if q.Name != "terminal_list_sessions" {
		return nil, errors.New("non-observational tool")
	}
	encoded, _ := json.Marshal(q.Arguments)
	if string(encoded) != "{}" {
		return nil, errors.New("poll acquired a Session use")
	}
	return &protocol.CallToolResult{StructuredContent: c.result}, nil
}
func TestStatusBarPublicStatePinsRegistrationAndDoesNotGrantWrites(t *testing.T) {
	c := &statusListClient{result: map[string]any{"sessions": []any{map[string]any{"session_id": "opaque", "session_ref": "SER-1", "label": "板卡", "state": "open", "control": map[string]any{"session_id": "opaque", "version": "v1", "state": "recovering", "writable": false, "ai_paused": true, "client": map[string]any{"kind": "agent", "name": "helper"}}}}}}
	attached := &mcpAttachSession{id: "SER-1", recoveryReference: "SER-1", client: c}
	text, err := attached.statusBarText(context.Background())
	if err != nil || !strings.Contains(text, "read-only") || !strings.Contains(text, "recovering") || !strings.Contains(text, "AI paused") || !strings.Contains(text, "agent/helper") {
		t.Fatal(text, err)
	}
	attached.recoveryReference = "SER-2"
	if _, err := attached.statusBarText(context.Background()); !errors.Is(err, ErrAttachedSessionNotFound) {
		t.Fatal("successor accepted", err)
	}
}

func TestStatusBarKeepsIndependentMCPOutputAndInputRaw(t *testing.T) {
	host := newAttachTestHost(t)
	defer host.close()
	attached, err := newMCPAttachSession(context.Background(), host.server.URL, "board")
	if err != nil {
		t.Fatal(err)
	}
	defer attached.Close()
	observer, err := newMCPAttachSession(context.Background(), host.server.URL, "board")
	if err != nil {
		t.Fatal(err)
	}
	defer observer.Close()
	raw := []byte("board> 中文\r\n")
	host.device.emit(raw)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	chunk, err := attached.ReadOutput(ctx, 0, 128)
	if err != nil {
		t.Fatal(err)
	}
	var display bytes.Buffer
	b := testStatusBar(&display)
	defer b.Close()
	state, err := attached.(*mcpAttachSession).statusBarText(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = b.update(state); err != nil {
		t.Fatal(err)
	}
	if _, err = b.Write(chunk.Data); err != nil {
		t.Fatal(err)
	}
	observed, err := observer.ReadOutput(ctx, 0, 128)
	if err != nil || !bytes.Equal(observed.Data, raw) {
		t.Fatalf("independent output = %q: %v", observed.Data, err)
	}
	input := []byte("echo 中文\r")
	if _, err = attached.Write(session.WriteRequest{Actor: session.ActorUser, Data: input}); err != nil {
		t.Fatal(err)
	}
	if got := host.device.waitWritten(t); !bytes.Equal(got, input) {
		t.Fatalf("input = %q", got)
	}
}

func TestStatusBarLargeHorizontalCountCannotOverflow(t *testing.T) {
	var out bytes.Buffer
	b := testStatusBar(&out)
	if _, err := b.Write([]byte("prefix\x1b[9223372036854775807CX")); err != nil {
		t.Fatal(err)
	}
	if b.active {
		t.Fatal("right edge must downgrade even for huge CSI counts")
	}
}

func TestStatusBarInputPermissionRequiresMatchingOpenSnapshot(t *testing.T) {
	for _, tc := range []struct {
		name, state, controlID, version string
		writable                        bool
		want                            string
	}{
		{"open", "open", "opaque", "v1", true, "input allowed"},
		{"closed", "closed", "opaque", "v1", true, "read-only"},
		{"denied", "open", "opaque", "v1", false, "read-only"},
		{"mismatch", "open", "other", "v1", true, "control unknown"},
		{"unversioned", "open", "opaque", "", true, "control unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := &statusListClient{result: map[string]any{"sessions": []any{map[string]any{
				"session_id": "opaque", "session_ref": "SER-1", "state": tc.state,
				"control": map[string]any{"session_id": tc.controlID, "version": tc.version, "state": "idle", "writable": tc.writable},
				"lease":   map[string]any{"type": "file-transfer"},
			}}}}
			attached := &mcpAttachSession{id: "opaque", recoveryReference: "SER-1", client: c}
			got, err := attached.statusBarText(context.Background())
			if err != nil || !strings.Contains(got, tc.want) {
				t.Fatal(got, err)
			}
			if tc.want != "control unknown" && !strings.Contains(got, "op=file-transfer") {
				t.Fatal("missing operation", got)
			}
		})
	}
}

// TestStatusBarVTTrace optionally exports actual coordinator bytes for the
// independent xterm display checks. Ordinary Go tests need no Electron runtime.
func TestStatusBarVTTrace(t *testing.T) {
	type step struct {
		Data   string `json:"data"`
		Update bool   `json:"update,omitempty"`
		Resize bool   `json:"resize,omitempty"`
	}
	traces := map[string][]step{}
	for _, name := range []string{"line", "wrap", "clear", "absolute", "alternate", "resize"} {
		var out bytes.Buffer
		b := testStatusBar(&out)
		b.size = terminalinput.Size{Columns: 40, Rows: 6}
		emit := func(update, resize bool) {
			traces[name] = append(traces[name], step{out.String(), update, resize})
			out.Reset()
		}
		_ = b.update("board | user | input allowed")
		emit(false, false)
		for _, part := range []string{"long> " + strings.Repeat("x", 5), "Y", "\r\n中文> abc\b \b", "\x1b[2DXY\x1b[K"} {
			_, _ = b.Write([]byte(part))
			emit(false, false)
			_ = b.update("board | user | busy")
			emit(true, false)
		}
		switch name {
		case "line":
			_, _ = b.Write([]byte("\r\n" + strings.Repeat("log\r\n", 20) + "final> 中文"))
		case "wrap":
			_, _ = b.Write([]byte("\r\n" + strings.Repeat("x", 40) + "Y"))
		case "clear":
			_, _ = b.Write([]byte("\x1b[2J\x1b[Hclear-ok"))
		case "absolute":
			_, _ = b.Write([]byte("\x1b[6;1Habsolute-ok"))
		case "alternate":
			_, _ = b.Write([]byte("\x1b[?1049h\x1b[Halternate-ok"))
		case "resize":
			b.measure = func() (terminalinput.Size, error) { return terminalinput.Size{Columns: 60, Rows: 10}, nil }
			_ = b.update("new size")
		}
		emit(false, name == "resize")
		if name == "alternate" {
			_, _ = b.Write([]byte("\x1b[?1049l"))
			emit(false, false)
		}
		_ = b.Close()
		emit(false, false)
	}
	if file := os.Getenv("CHANNELTERM_TEST_STATUS_TRACE"); file != "" {
		data, err := json.Marshal(traces)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(file, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
}

// TestStatusBarNativeConsole is opt-in: it reads an exact synthetic keyboard
// sample from a real local terminal, never opens a Session or device. EOT ends it.
func TestStatusBarNativeConsole(t *testing.T) {
	expected := os.Getenv("CHANNELTERM_TEST_STATUS_INPUT")
	if expected == "" {
		t.Skip("requires an interactive terminal and explicit keyboard sample")
	}
	inputFile, outputFile := statusConsoleFiles(t)
	b := newConsoleStatusBar(inputFile, outputFile, true)
	if b == nil {
		t.Fatal("native console unavailable")
	}
	input, restore, err := terminalinput.MakeRaw(inputFile)
	if err != nil {
		t.Fatal(err)
	}
	defer restore()
	defer b.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stop := runStatusBar(ctx, b, func(context.Context) (string, error) { return "probe | local user | input unchanged", nil }, cancel)
	defer stop()
	_, _ = b.Write([]byte("INPUT-PROBE> "))
	var received []byte
	data := make([]byte, 256)
	for len(received) < 4096 {
		n, err := input.Read(data)
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range data[:n] {
			if c == 4 {
				want, err := base64.StdEncoding.DecodeString(expected)
				if err != nil || !bytes.Equal(received, want) {
					t.Fatalf("input mismatch: %x != %x (%v)", received, want, err)
				}
				return
			}
			received = append(received, c)
		}
		if _, err := b.Write(data[:n]); err != nil {
			t.Fatal(err)
		}
	}
	t.Fatal("keyboard sample exceeded bound")
}

// statusConsoleCapture drains the pseudo terminal while the child runs and while
// its console closes. Holding all bytes is bounded by this tiny synthetic probe.
type statusConsoleCapture struct {
	mu     sync.Mutex
	buffer bytes.Buffer
	ready  chan struct{}
	once   sync.Once
}

func (c *statusConsoleCapture) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.buffer.Len() < 65536 {
		_, _ = c.buffer.Write(p)
	}
	if strings.Contains(c.buffer.String(), "INPUT-PROBE>") {
		c.once.Do(func() { close(c.ready) })
	}
	return len(p), nil
}
func (c *statusConsoleCapture) snapshot() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buffer.String()
}
