package command

import (
	"bytes"
	"testing"
)

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
