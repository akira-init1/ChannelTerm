package command

import (
	"errors"
	"io"
	"os"
	"strings"

	"github.com/akira-init1/ChannelTerm/internal/cli/highlight"
	"golang.org/x/term"
)

var errInvalidHighlightMode = errors.New("highlight mode must be auto, always, or never")

// resolveHighlightRenderer chooses whether one CLI terminal consumer receives
// presentation-only ANSI styling. It never changes Session or MCP output.
func resolveHighlightRenderer(mode string, output io.Writer) (*highlight.Renderer, error) {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "never":
		return nil, nil
	case "always":
		return highlight.New(output), nil
	case "auto":
		file, ok := output.(*os.File)
		if !ok || !term.IsTerminal(int(file.Fd())) || !enableANSIOutput(file) {
			return nil, nil
		}
		return highlight.New(output), nil
	default:
		return nil, errInvalidHighlightMode
	}
}
