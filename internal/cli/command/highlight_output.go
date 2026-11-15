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
// semantic highlighting is disabled. An optional destination coordinates local
// drawing while capability detection still uses the original terminal writer.
func resolveHighlightRenderer(enabled bool, output io.Writer, destination ...io.Writer) *highlight.Renderer {
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
	sink := output
	if len(destination) != 0 {
		sink = destination[0]
	}
	return highlight.New(sink, highlight.WithColorProfile(profile))
}
