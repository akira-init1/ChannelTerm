package command

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/akira-init1/ChannelTerm/internal/cli/highlight"
)

func TestBootOutputThroughPresentation(t *testing.T) {
	const probe = "\x1b7\x1b[r\x1b[999;999H\x1b[6n\x1b8"
	const input = "hwclock: can't open '/dev/misc/rtc': No such file or directory\r\r\n" +
		"Starting tcf-agent: OK\r\n\r\n" + probe + "root@board:~# "
	stripStyles := regexp.MustCompile(`\x1b\[[0-9;]*m`)
	for _, timestamps := range []bool{false, true} {
		for size := 1; size <= len(input); size++ {
			t.Run(fmt.Sprintf("timestamps=%t/chunk=%d", timestamps, size), func(t *testing.T) {
				var output bytes.Buffer
				highlighter := highlight.New(&output)
				renderer := newPromptTimestampRenderer(terminalOutputWriter(&output, highlighter), terminalOutputFlusher(highlighter), fixedPromptTimestampClock)
				if timestamps {
					if _, err := renderer.Toggle(); err != nil {
						t.Fatal(err)
					}
				}
				for start := 0; start < len(input); start += size {
					if err := renderer.Write([]byte(input[start:min(start+size, len(input))])); err != nil {
						t.Fatal(err)
					}
				}
				for _, token := range []string{
					"\x1b[38;2;255;95;95mcan't open\x1b[0m",
					"\x1b[38;2;255;95;95mNo such\x1b[0m",
					"\x1b[31mroot\x1b[0m",
				} {
					if !strings.Contains(output.String(), token) {
						t.Fatalf("missing styled token %q in %q", token, &output)
					}
				}
				// Cursor controls bypass timestamp insertion, but both modes must
				// preserve every remote byte and allow downstream highlighting.
				plain := stripStyles.ReplaceAllString(output.String(), "")
				if plain != input {
					t.Fatalf("remote bytes changed or delayed: got %q want %q", plain, input)
				}
				beforeEcho := output.String()
				for _, key := range []byte("echo No") {
					if err := renderer.Write([]byte{key}); err != nil {
						t.Fatal(err)
					}
					beforeEcho += string(key)
					if output.String() != beforeEcho {
						t.Fatalf("echo delayed or styled: %q", &output)
					}
				}
				if err := renderer.Write([]byte("\r\nroot@board:~# ")); err != nil {
					t.Fatal(err)
				}
				wantSuffix := "\r\nroot@board:~# "
				if timestamps {
					wantSuffix = "\r\n[14:05:01] root@board:~# "
				}
				if got := stripStyles.ReplaceAllString(output.String(), ""); !strings.HasSuffix(got, wantSuffix) {
					t.Fatalf("next prompt did not recover: %q", got)
				}
			})
		}
	}
}

// TestBootloaderPromptEchoThroughPresentation covers the two buffering layers
// together: neither may wait for Enter before exposing a prompt or echoed key.
func TestBootloaderPromptEchoThroughPresentation(t *testing.T) {
	for _, timestamps := range []bool{false, true} {
		for _, prompt := range []string{"Zynq> ", "ZynqMP> ", "=> ", "U-Boot> ", "\x1b[32mZynq> \x1b[0m"} {
			for split := 0; split <= len(prompt)+4; split++ {
				t.Run(fmt.Sprintf("timestamps=%t/prompt=%q/split=%d", timestamps, prompt, split), func(t *testing.T) {
					t.Setenv("NO_COLOR", "")
					t.Setenv("CLICOLOR_FORCE", "1")
					var output bytes.Buffer
					highlighter := resolveHighlightRenderer(true, &output)
					if highlighter == nil {
						t.Fatal("test requires active highlighting")
					}
					renderer := newPromptTimestampRenderer(terminalOutputWriter(&output, highlighter), terminalOutputFlusher(highlighter), fixedPromptTimestampClock)
					if timestamps {
						if _, err := renderer.Toggle(); err != nil {
							t.Fatal(err)
						}
					}
					// The previous output ends a line; the new prompt has no newline.
					want := "autoboot stopped\r\n"
					if err := renderer.Write([]byte(want)); err != nil {
						t.Fatal(err)
					}
					input := prompt + "help"
					for _, chunk := range []string{input[:split], input[split:]} {
						if err := renderer.Write([]byte(chunk)); err != nil {
							t.Fatal(err)
						}
					}
					want += input
					if output.String() != want {
						t.Fatalf("prompt/echo withheld before Enter: got %q want %q", output.String(), want)
					}
					for _, echo := range []string{" ", "1", "\b \b", "\r", "\n", prompt} {
						if err := renderer.Write([]byte(echo)); err != nil {
							t.Fatal(err)
						}
						want += echo
						if output.String() != want {
							t.Fatalf("delayed or changed echo: got %q want %q", output.String(), want)
						}
					}
				})
			}
		}
	}
}

// TestColoredPromptEchoThroughPresentation verifies the actual timestamp and
// highlighting pipeline exposes every echoed key before a newline or flush.
func TestColoredPromptEchoThroughPresentation(t *testing.T) {
	for _, timestamps := range []bool{false, true} {
		t.Run(map[bool]string{false: "timestamps-off", true: "timestamps-on"}[timestamps], func(t *testing.T) {
			t.Setenv("NO_COLOR", "")
			t.Setenv("CLICOLOR_FORCE", "1")
			var output bytes.Buffer
			highlighter := resolveHighlightRenderer(true, &output)
			if highlighter == nil {
				t.Fatal("test requires active semantic highlighting")
			}
			renderer := newPromptTimestampRenderer(terminalOutputWriter(&output, highlighter), terminalOutputFlusher(highlighter), fixedPromptTimestampClock)
			want := ""
			if timestamps {
				if _, err := renderer.Toggle(); err != nil {
					t.Fatal(err)
				}
				want = "[14:05:01] "
			}
			const prompt = "\x1b[32muser@host\x1b[0m:\x1b[34m~\x1b[0m$ "
			for _, value := range []byte(prompt) {
				if err := renderer.Write([]byte{value}); err != nil {
					t.Fatal(err)
				}
			}
			want += prompt
			if output.String() != want {
				t.Fatalf("prompt withheld: %q", &output)
			}
			for _, echo := range []string{"l", "s", "\b \b", "s"} {
				if err := renderer.Write([]byte(echo)); err != nil {
					t.Fatal(err)
				}
				want += echo
				if output.String() != want {
					t.Fatalf("echo withheld before Enter: got %q want %q", output.String(), want)
				}
			}
		})
	}
}

func TestResolveHighlightRendererUsesEnvironmentColorPolicy(t *testing.T) {
	tests := []struct {
		name      string
		enabled   bool
		noColor   string
		force     string
		wantFound bool
	}{
		{name: "redirected output stays plain", enabled: true},
		{name: "forced color", enabled: true, force: "1", wantFound: true},
		{name: "no color wins", enabled: true, noColor: "1", force: "1"},
		{name: "flag disabled", enabled: false, force: "1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("NO_COLOR", tt.noColor)
			t.Setenv("CLICOLOR", "")
			t.Setenv("CLICOLOR_FORCE", tt.force)
			var output bytes.Buffer
			got := resolveHighlightRenderer(tt.enabled, &output)
			if (got != nil) != tt.wantFound {
				t.Errorf("resolveHighlightRenderer() found = %t, want %t", got != nil, tt.wantFound)
			}
		})
	}
}
