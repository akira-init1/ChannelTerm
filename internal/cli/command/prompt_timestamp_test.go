package command

import (
	"bytes"
	"testing"
	"time"
)

func TestPromptTimestampRendererDefaultsOffAndToggles(t *testing.T) {
	var output bytes.Buffer
	renderer := newPromptTimestampRenderer(func(data []byte) error {
		_, err := output.Write(data)
		return err
	}, nil, fixedPromptTimestampClock)

	if renderer.Enabled() {
		t.Fatal("renderer starts enabled, want disabled")
	}
	if err := renderer.Write([]byte("root@board:~# ")); err != nil {
		t.Fatalf("Write() while disabled error = %v", err)
	}
	if got, want := output.String(), "root@board:~# "; got != want {
		t.Errorf("disabled output = %q, want %q", got, want)
	}
	if enabled, err := renderer.Toggle(); err != nil || !enabled {
		t.Errorf("first Toggle() = %t, %v; want true, nil", enabled, err)
	}
	if enabled, err := renderer.Toggle(); err != nil || enabled {
		t.Errorf("second Toggle() = %t, %v; want false, nil", enabled, err)
	}
}

func TestPromptTimestampRendererAddsTimestampOnlyToPrompts(t *testing.T) {
	tests := []struct {
		name   string
		chunks [][]byte
		want   string
	}{
		{name: "ordinary output", chunks: [][]byte{[]byte("Linux version\r\nCPU: arm\r\n")}, want: "Linux version\r\nCPU: arm\r\n"},
		{name: "root prompt", chunks: [][]byte{[]byte("root@board:~# ")}, want: "[14:05:01] root@board:~# "},
		{name: "user prompt", chunks: [][]byte{[]byte("user@host:/path$ ")}, want: "[14:05:01] user@host:/path$ "},
		{name: "busybox prompt", chunks: [][]byte{[]byte("/ # ")}, want: "[14:05:01] / # "},
		{name: "bracket prompt", chunks: [][]byte{[]byte("[root@board ~]# ")}, want: "[14:05:01] [root@board ~]# "},
		{name: "prompt split across reads", chunks: [][]byte{[]byte("\r\nroot@pd"), []byte("prj:~"), []byte("# ")}, want: "\r\n[14:05:01] root@pdprj:~# "},
		{name: "colored prompt preserves bytes", chunks: [][]byte{[]byte("\x1b[32mroot@board\x1b[0m:~# ")}, want: "[14:05:01] \x1b[32mroot@board\x1b[0m:~# "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var output bytes.Buffer
			renderer := newPromptTimestampRenderer(func(data []byte) error {
				_, err := output.Write(data)
				return err
			}, nil, fixedPromptTimestampClock)
			if enabled, err := renderer.Toggle(); err != nil || !enabled {
				t.Fatalf("Toggle() = %t, %v; want true, nil", enabled, err)
			}
			for _, chunk := range tt.chunks {
				if err := renderer.Write(chunk); err != nil {
					t.Fatalf("Write(%q) error = %v", chunk, err)
				}
			}
			if got := output.String(); got != tt.want {
				t.Errorf("rendered output = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPromptTimestampRendererPassesScreenControlThrough(t *testing.T) {
	const screen = "\x1b[2J\x1b[Htop output\rnext frame"
	var output bytes.Buffer
	renderer := newPromptTimestampRenderer(func(data []byte) error {
		_, err := output.Write(data)
		return err
	}, nil, fixedPromptTimestampClock)
	if _, err := renderer.Toggle(); err != nil {
		t.Fatalf("Toggle() error = %v", err)
	}
	if err := renderer.Write([]byte(screen)); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if got := output.String(); got != screen {
		t.Errorf("screen output = %q, want unchanged %q", got, screen)
	}
}

func TestPromptTimestampRendererRecognizesPromptAfterCREchoTerminator(t *testing.T) {
	var output bytes.Buffer
	renderer := newPromptTimestampRenderer(func(data []byte) error {
		_, err := output.Write(data)
		return err
	}, nil, fixedPromptTimestampClock)
	if _, err := renderer.Toggle(); err != nil {
		t.Fatalf("Toggle() error = %v", err)
	}
	for _, chunk := range [][]byte{[]byte("root@board:~# "), []byte("top\r"), []byte("\nroot@board:~# ")} {
		if err := renderer.Write(chunk); err != nil {
			t.Fatalf("Write(%q) error = %v", chunk, err)
		}
	}
	want := "[14:05:01] root@board:~# top\r\n[14:05:01] root@board:~# "
	if got := output.String(); got != want {
		t.Errorf("rendered output = %q, want %q", got, want)
	}
}

func TestPromptTimestampRenderersKeepStateIndependent(t *testing.T) {
	var first, second bytes.Buffer
	one := newPromptTimestampRenderer(func(data []byte) error { _, err := first.Write(data); return err }, nil, fixedPromptTimestampClock)
	two := newPromptTimestampRenderer(func(data []byte) error { _, err := second.Write(data); return err }, nil, fixedPromptTimestampClock)
	if _, err := one.Toggle(); err != nil {
		t.Fatalf("first Toggle() error = %v", err)
	}
	for _, renderer := range []*promptTimestampRenderer{one, two} {
		if err := renderer.Write([]byte("root@board:~# ")); err != nil {
			t.Fatalf("Write() error = %v", err)
		}
	}
	if got, want := first.String(), "[14:05:01] root@board:~# "; got != want {
		t.Errorf("first output = %q, want %q", got, want)
	}
	if got, want := second.String(), "root@board:~# "; got != want {
		t.Errorf("second output = %q, want %q", got, want)
	}
}

func fixedPromptTimestampClock() time.Time {
	return time.Date(2026, time.August, 23, 14, 5, 1, 328000000, time.Local)
}
