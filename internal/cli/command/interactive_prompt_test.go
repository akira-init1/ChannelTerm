package command

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/akira-init1/ChannelTerm/internal/cli/highlight"
)

// TestInteractivePromptFamilies gives each console family its own regression
// node through the presentation pipeline shared by serial, attach, and SSH.
func TestInteractivePromptFamilies(t *testing.T) {
	cases := []struct {
		name    string
		prompts []string
		stamped bool
	}{
		{name: "BashUser", prompts: []string{"sh-5.1$ ", "bash-5.2$ ", "[user@board ~]$ "}, stamped: true},
		{name: "POSIXAndContinuation", prompts: []string{"# ", "$ ", "> "}},
		{name: "UBoot", prompts: []string{"BBG U-Boot > ", "U-Boot > ", "=>", "Zynq>", "ZynqMP>"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, prompt := range tc.prompts {
				for _, colors := range []bool{false, true} {
					for _, timestamps := range []bool{false, true} {
						input := prompt + "echo ERROR 123"
						for size := 1; size <= len(input); size++ {
							t.Run(fmt.Sprintf("%q/colors=%t/time=%t/chunk=%d", prompt, colors, timestamps, size), func(t *testing.T) {
								var output bytes.Buffer
								var highlighter *highlight.Renderer
								if colors {
									highlighter = highlight.New(&output)
								}
								renderer := newPromptTimestampRenderer(terminalOutputWriter(&output, highlighter), terminalOutputFlusher(highlighter), fixedPromptTimestampClock)
								stamp := ""
								if timestamps {
									if _, err := renderer.Toggle(); err != nil {
										t.Fatal(err)
									}
									if tc.stamped {
										stamp = "[14:05:01] "
									}
								}
								want := ""
								for range 2 {
									for start := 0; start < len(input); start += size {
										if err := renderer.Write([]byte(input[start:min(start+size, len(input))])); err != nil {
											t.Fatal(err)
										}
									}
									want += stamp + input
									if output.String() != want {
										t.Fatalf("prompt/echo delayed or styled: got %q want %q", output.String(), want)
									}
									for _, echo := range []string{"x", "\b \b", "\r", "\n"} {
										if err := renderer.Write([]byte(echo)); err != nil {
											t.Fatal(err)
										}
										want += echo
										if output.String() != want {
											t.Fatalf("echo delayed or styled: got %q want %q", output.String(), want)
										}
									}
								}
								if err := renderer.Flush(); err != nil {
									t.Fatal(err)
								}
								if output.String() != want {
									t.Fatal("flush duplicated remote output")
								}
							})
						}
					}
				}
			}
		})
	}
}
