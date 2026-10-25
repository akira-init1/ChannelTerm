package highlight

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"

	"github.com/muesli/termenv"
)

// defaultsJSON is the versioned presentation data also loaded by the desktop
// client. It is compiled into the CLI, never read from user configuration.
//
//go:embed defaults.json
var defaultsJSON []byte

type colorDefinition struct {
	Hex      string `json:"hex"`
	ANSI     int    `json:"ansi"`
	ExactRGB bool   `json:"exactRGB"`
}

type ruleDefinition struct {
	Pattern  string    `json:"pattern"`
	Flags    string    `json:"flags"`
	Group    int       `json:"group"`
	Kind     tokenKind `json:"kind"`
	Priority int       `json:"priority"`
}

type highlightDefinitions struct {
	Version int                        `json:"version"`
	Palette map[string]colorDefinition `json:"palette"`
	Rules   []ruleDefinition           `json:"rules"`
	Prompts map[string]string          `json:"prompts"`
}

// Invalid embedded definitions are a build defect, not recoverable terminal
// input. Tests exercise this initialization before a binary is handed off.
var defaults = func() highlightDefinitions {
	var definitions highlightDefinitions
	if err := json.Unmarshal(defaultsJSON, &definitions); err != nil {
		panic(fmt.Sprintf("invalid embedded highlight definitions: %v", err))
	}
	if definitions.Version != 1 {
		panic("unsupported embedded highlight definition version")
	}
	return definitions
}()

func compileRules(definitions highlightDefinitions) []rule {
	rules := make([]rule, 0, len(definitions.Rules))
	for _, definition := range definitions.Rules {
		pattern := definition.Pattern
		if definition.Flags == "i" {
			pattern = "(?i)" + pattern
		} else if definition.Flags != "" {
			panic("unsupported embedded highlight flags")
		}
		compiled := regexp.MustCompile(pattern)
		if definition.Group < 0 || definition.Group > compiled.NumSubexp() {
			panic("invalid embedded highlight capture group")
		}
		if _, ok := definitions.Palette[string(definition.Kind)]; !ok {
			panic("unknown embedded highlight category")
		}
		rules = append(rules, rule{compiled, definition.Group, definition.Kind, definition.Priority})
	}
	return rules
}

func (color colorDefinition) render(profile termenv.Profile) style {
	if color.ANSI != 0 {
		return ansiPaletteStyle(profile, color.ANSI)
	}
	if color.ExactRGB && profile == termenv.TrueColor {
		// Preserve exact RGB for colors whose termenv conversion rounds a channel.
		rgb, err := strconv.ParseUint(color.Hex[1:], 16, 24)
		if err != nil {
			panic("invalid embedded highlight RGB color")
		}
		return style(fmt.Sprintf("\x1b[38;2;%d;%d;%dm", rgb>>16, (rgb>>8)&255, rgb&255))
	}
	return colorStyle(profile, color.Hex)
}
