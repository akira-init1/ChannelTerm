package command

import (
	"io"
	"os"

	"github.com/akira-init1/ChannelTerm/internal/cli/highlight"
	"github.com/muesli/termenv"
	"golang.org/x/term"
)

// resolveHighlightRenderer enables local ANSI processing and chooses whether
// one CLI terminal consumer receives presentation-only semantic styling. It
// never changes Session or MCP output. Remote ANSI remains usable when local
// semantic highlighting is disabled.
func resolveHighlightRenderer(enabled bool, output io.Writer) *highlight.Renderer {
	if file, ok := output.(*os.File); ok && term.IsTerminal(int(file.Fd())) {
		if !enableANSIOutput(file) {
			return nil
		}
	}
	if !enabled {
		return nil
	}
	profile := termenv.NewOutput(output).EnvColorProfile()
	if profile == termenv.Ascii {
		return nil
	}
	return highlight.New(output, highlight.WithColorProfile(profile))
}
