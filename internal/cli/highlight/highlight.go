// Package highlight renders safe, line-oriented ANSI emphasis for terminal output.
//
// It is deliberately a presentation-only layer. Callers retain the original
// terminal bytes in Session and pass a separate output stream to Renderer. Its
// WinTerm-inspired semantic theme adapts to the local terminal's TrueColor,
// 256-color, or 16-color capability.
package highlight

import (
	"bytes"
	"io"
	"regexp"
	"sort"
	"strconv"
	"unicode/utf8"

	"github.com/muesli/termenv"
)

const maxPendingBytes = 8 * 1024

const styleReset style = "\x1b[0m"

// Option configures one Renderer.
type Option func(*Renderer)

// WithColorProfile selects the ANSI color capability used for generated
// presentation styles. Callers should select the profile from the local output
// terminal; it does not describe the remote terminal.
func WithColorProfile(profile termenv.Profile) Option {
	return func(renderer *Renderer) {
		renderer.theme = newTheme(profile)
	}
}

// Renderer incrementally renders terminal text to one local output stream.
//
// Renderer is not safe for concurrent use. One attached terminal client must
// own one Renderer and serialize Write and Flush calls around any unrelated
// local status output. ReleasePending must use the same serialization. It never
// modifies the caller's input slice.
type Renderer struct {
	output          io.Writer
	pending         []byte
	rawUntilNewline bool
	vtPassthrough   bool
	vt              vtTracker
	theme           theme
}

// New creates a Renderer that writes styled terminal output to output. It uses
// TrueColor unless an Option selects another local color profile.
//
// A nil output discards rendered bytes. It is useful for callers that need to
// exercise parser behavior without a local terminal writer.
func New(output io.Writer, options ...Option) *Renderer {
	if output == nil {
		output = io.Discard
	}
	renderer := &Renderer{output: output, theme: newTheme(termenv.TrueColor)}
	for _, option := range options {
		option(renderer)
	}
	return renderer
}

// Write accepts terminal bytes and forwards complete safe lines with ANSI
// emphasis. Unsafe line controls are forwarded unchanged. Remote escape
// sequences switch presentation to byte-transparent output until their line
// and any persistent SGR or alternate-screen state safely end.
//
// A recognized shell or bootloader prompt is emitted before a newline so the
// remote console remains interactive. The following echoed command is forwarded
// unchanged until its newline.
func (r *Renderer) Write(p []byte) (int, error) {
	writtenInput := len(p)
	for len(p) > 0 {
		if r.vtPassthrough {
			consumed, err := r.writeVTPassthrough(p)
			if err != nil {
				return 0, err
			}
			p = p[consumed:]
			continue
		}

		escape := bytes.IndexByte(p, '\x1b')
		if escape < 0 {
			for _, value := range p {
				r.vt.feed(value)
			}
			r.pending = append(r.pending, p...)
			if err := r.drain(); err != nil {
				return 0, err
			}
			break
		}

		// Preserve all remote bytes from the escape onward. Complete safe lines
		// before it may still be highlighted; any incomplete prefix is emitted raw.
		// Track plain text too: a later SGR reset inside a colored prompt is not
		// a fresh line boundary, and its suffix/echo must remain byte-transparent.
		for _, value := range p[:escape] {
			r.vt.feed(value)
		}
		r.pending = append(r.pending, p[:escape]...)
		if err := r.drain(); err != nil {
			return 0, err
		}
		if len(r.pending) > 0 {
			if err := writeAll(r.output, r.pending); err != nil {
				return 0, err
			}
			r.pending = nil
		}
		r.rawUntilNewline = false
		r.vtPassthrough = true
		p = p[escape:]
	}
	return writtenInput, nil
}

// writeVTPassthrough forwards one raw segment while tracking enough VT state
// to resume semantic highlighting only at a safe line boundary. It deliberately
// does not emulate the terminal screen.
func (r *Renderer) writeVTPassthrough(p []byte) (int, error) {
	consumed := len(p)
	resume := false
	for index, value := range p {
		if r.vt.feed(value) {
			consumed = index + 1
			resume = true
			break
		}
		if value == '\n' && r.vt.canResumeHighlighting() {
			consumed = index + 1
			resume = true
			break
		}
	}
	if err := writeAll(r.output, p[:consumed]); err != nil {
		return 0, err
	}
	if resume {
		r.vtPassthrough = false
	}
	return consumed, nil
}

// Flush writes any incomplete terminal line and ends prompt-echo state.
// Callers invoke it before local status output or orderly disconnect so later
// remote prompts are rendered independently and output remains ordered.
func (r *Renderer) Flush() error {
	if len(r.pending) > 0 {
		// Flush is also used before local ChannelTerm status output. An incomplete
		// remote line cannot be classified safely because more bytes may follow, so
		// preserve it rather than injecting styles into a partial terminal record.
		if err := writeAll(r.output, r.pending); err != nil {
			return err
		}
		r.pending = nil
	}
	r.rawUntilNewline = false
	// Local status output establishes a new prompt presentation boundary.
	r.vt.lineHasText = false
	return nil
}

// ReleasePending emits an incomplete line raw and keeps subsequent bytes raw
// until newline. Interactive callers use it periodically so an unknown prompt
// never waits indefinitely for Enter. Unlike Flush, this does not establish a
// local status boundary or reset remote VT state. An empty buffer is a no-op.
func (r *Renderer) ReleasePending() error {
	if len(r.pending) == 0 {
		return nil
	}
	if err := writeAll(r.output, r.pending); err != nil {
		return err
	}
	r.pending = nil
	r.rawUntilNewline = true
	return nil
}

// drain emits complete lines and recognized no-newline console prompts while
// keeping a bounded suffix for output that arrives in arbitrary read chunks.
func (r *Renderer) drain() error {
	for len(r.pending) > 0 {
		if newline := bytes.IndexByte(r.pending, '\n'); newline >= 0 {
			line := r.pending[:newline+1]
			if err := r.writeLine(line, r.rawUntilNewline); err != nil {
				return err
			}
			r.pending = r.pending[newline+1:]
			r.rawUntilNewline = false
			continue
		}
		if !r.rawUntilNewline && r.pending[len(r.pending)-1] == '\r' {
			// Serial drivers commonly split CRLF at an arbitrary read boundary.
			// Keep the CR until the next chunk determines whether it is an ordinary
			// line ending or a screen-overwrite control sequence.
			return nil
		}
		if r.rawUntilNewline || containsScreenControl(r.pending) || len(r.pending) > maxPendingBytes {
			if err := writeAll(r.output, r.pending); err != nil {
				return err
			}
			r.pending = nil
			continue
		}
		if HasInteractivePromptPrefix(r.pending) {
			// Interactive prompts have no newline. Emit them and their echo raw
			// rather than holding the interactive line until the user presses Enter.
			if err := writeAll(r.output, r.pending); err != nil {
				return err
			}
			r.pending = nil
			r.rawUntilNewline = true
		} else if rendered, ok := r.renderPrompt(r.pending); ok {
			if err := writeAll(r.output, rendered); err != nil {
				return err
			}
			r.pending = nil
			r.rawUntilNewline = true
		}
		return nil
	}
	return nil
}

// writeLine chooses raw forwarding for unsafe or echoed input and otherwise
// applies the built-in rules to one complete logical line.
func (r *Renderer) writeLine(line []byte, raw bool) error {
	if raw || containsScreenControl(line) || HasInteractivePromptPrefix(line) {
		return writeAll(r.output, line)
	}
	content, ending := splitLineEnding(line)
	if rendered, ok := r.renderPrompt(content); ok {
		if err := writeAll(r.output, rendered); err != nil {
			return err
		}
		return writeAll(r.output, ending)
	}
	return writeAll(r.output, renderText(line, r.theme))
}

func splitLineEnding(line []byte) ([]byte, []byte) {
	if bytes.HasSuffix(line, []byte("\n")) {
		start := len(line) - 1
		// Serial console output may acquire more than one CR during newline
		// translation. Keep the entire ending intact and outside prompt matching.
		for start > 0 && line[start-1] == '\r' {
			start--
		}
		return line[:start], line[start:]
	}
	return line, nil
}

// containsScreenControl identifies bytes whose presentation cannot be safely
// reconstructed by a line-oriented renderer. Tab, newline, and CR runs directly
// before LF are retained; carriage returns within text still force raw output.
func containsScreenControl(data []byte) bool {
	if !utf8.Valid(data) {
		return true
	}
	for index := 0; index < len(data); index++ {
		value := data[index]
		if value == '\t' || value == '\n' {
			continue
		}
		if value == '\r' {
			for index+1 < len(data) && data[index+1] == '\r' {
				index++
			}
			if index+1 < len(data) && data[index+1] == '\n' {
				continue
			}
			return true
		}
		if value < 0x20 || value == 0x7f {
			return true
		}
	}
	return false
}

// renderText applies the highest-priority non-overlapping rule matches to one
// safe line. Rules operate on byte indexes because regexp indexes and ANSI
// insertion both use the original UTF-8 byte representation.
func renderText(line []byte, colors theme) []byte {
	matches := make([]match, 0)
	for _, rule := range builtinRules {
		for _, index := range rule.pattern.FindAllSubmatchIndex(line, -1) {
			group := rule.group * 2
			if group+1 >= len(index) || index[group] < 0 || index[group] == index[group+1] {
				continue
			}
			matches = append(matches, match{
				start:    index[group],
				end:      index[group+1],
				style:    colors.style(rule.kind),
				priority: rule.priority,
			})
		}
	}
	return renderMatches(line, matches)
}

// renderPrompt styles user@host prompts and forwards minimal shell prompts raw.
// Both forms must end line buffering so subsequent key echo is visible before
// Enter. The styled path uses bright blue for contrast on dark backgrounds.
func (r *Renderer) renderPrompt(line []byte) ([]byte, bool) {
	index := promptPattern.FindSubmatchIndex(line)
	if index == nil {
		if minimalShellPromptPattern.Match(line) {
			return append([]byte(nil), line...), true
		}
		return nil, false
	}
	userStart, userEnd := index[2], index[3]
	markerStart, markerEnd := index[8], index[9]
	return renderMatches(line, []match{
		{start: userStart, end: userEnd, style: r.theme.promptUser, priority: 1},
		{start: index[4], end: index[5], style: r.theme.promptHost, priority: 1},
		{start: index[6], end: index[7], style: r.theme.promptPath, priority: 1},
		{start: markerStart, end: markerEnd, style: r.theme.promptUser, priority: 1},
	}), true
}

// renderMatches gives higher-priority tokens ownership of their exact byte
// ranges. Lower-priority spans are retained around those ranges, allowing a
// field such as mmcblk0boot1 to keep its field/interface color while each
// embedded number uses the shared numeric color.
func renderMatches(line []byte, matches []match) []byte {
	if len(matches) == 0 {
		return append([]byte(nil), line...)
	}
	sort.Slice(matches, func(left, right int) bool {
		if matches[left].priority != matches[right].priority {
			return matches[left].priority > matches[right].priority
		}
		if matches[left].start != matches[right].start {
			return matches[left].start < matches[right].start
		}
		return matches[left].end > matches[right].end
	})
	selected := make([]match, 0, len(matches))
	for _, candidate := range matches {
		if candidate.start == candidate.end || candidate.style == "" {
			continue
		}
		selected = append(selected, uncoveredMatchFragments(candidate, selected)...)
	}
	sort.Slice(selected, func(left, right int) bool {
		return selected[left].start < selected[right].start
	})

	rendered := make([]byte, 0, len(line)+len(selected)*len(styleReset)*2)
	next := 0
	for _, candidate := range selected {
		rendered = append(rendered, line[next:candidate.start]...)
		rendered = append(rendered, candidate.style...)
		rendered = append(rendered, line[candidate.start:candidate.end]...)
		rendered = append(rendered, styleReset...)
		next = candidate.end
	}
	rendered = append(rendered, line[next:]...)
	return rendered
}

// uncoveredMatchFragments subtracts ranges already owned by earlier,
// higher-priority matches without discarding the candidate's remaining text.
func uncoveredMatchFragments(candidate match, selected []match) []match {
	fragments := []match{candidate}
	for _, blocker := range selected {
		next := make([]match, 0, len(fragments)+1)
		for _, fragment := range fragments {
			if fragment.end <= blocker.start || blocker.end <= fragment.start {
				next = append(next, fragment)
				continue
			}
			if fragment.start < blocker.start {
				left := fragment
				left.end = blocker.start
				next = append(next, left)
			}
			if blocker.end < fragment.end {
				right := fragment
				right.start = blocker.end
				next = append(next, right)
			}
		}
		fragments = next
		if len(fragments) == 0 {
			break
		}
	}
	return fragments
}

// writeAll preserves io.Writer's short-write contract for rendered output.
func writeAll(output io.Writer, data []byte) error {
	for len(data) > 0 {
		written, err := output.Write(data)
		if written > len(data) {
			written = len(data)
		}
		data = data[written:]
		if err != nil {
			return err
		}
		if written == 0 {
			return io.ErrShortWrite
		}
	}
	return nil
}

type style string

type tokenKind string

const (
	tokenError     tokenKind = "error"
	tokenWarning   tokenKind = "warning"
	tokenSuccess   tokenKind = "success"
	tokenHeading   tokenKind = "heading"
	tokenField     tokenKind = "field"
	tokenAddress   tokenKind = "address"
	tokenNumber    tokenKind = "number"
	tokenVersion   tokenKind = "version"
	tokenTimestamp tokenKind = "timestamp"
	tokenUnit      tokenKind = "unit"
	tokenString    tokenKind = "string"
	tokenInfo      tokenKind = "info"
	tokenDebug     tokenKind = "debug"
	tokenInterface tokenKind = "interface"
	tokenDelimiter tokenKind = "delimiter"
)

type theme struct {
	styles     map[tokenKind]style
	promptUser style
	promptHost style
	promptPath style
}

func newTheme(profile termenv.Profile) theme {
	styles := make(map[tokenKind]style, len(defaults.Palette))
	for kind, color := range defaults.Palette {
		styles[tokenKind(kind)] = color.render(profile)
	}
	return theme{
		styles:     styles,
		promptUser: styles["promptUser"],
		promptHost: styles["promptHost"],
		promptPath: styles["promptPath"],
	}
}

func (t theme) style(kind tokenKind) style {
	return t.styles[kind]
}

func colorStyle(profile termenv.Profile, hex string) style {
	color := profile.Color(hex)
	if color == nil {
		return ""
	}
	sequence := color.Sequence(false)
	if sequence == "" {
		return ""
	}
	return style("\x1b[" + sequence + "m")
}

func ansiPaletteStyle(profile termenv.Profile, code int) style {
	if profile == termenv.Ascii {
		return ""
	}
	return style("\x1b[" + strconv.Itoa(code) + "m")
}

type rule struct {
	pattern  *regexp.Regexp
	group    int
	kind     tokenKind
	priority int
}

type match struct {
	start    int
	end      int
	style    style
	priority int
}

var builtinRules = compileRules(defaults)

var promptPattern = regexp.MustCompile(defaults.Prompts["promptPattern"])

// Minimal prompts end buffering but stay unstyled, including any echoed input.
var minimalShellPromptPattern = regexp.MustCompile(defaults.Prompts["minimalShellPromptPattern"])

// HasBootloaderPromptPrefix recognizes a plain line starting with "=> " or a
// name followed by "> ", including an already echoed command. Trailing prompt
// whitespace is required so an incomplete marker cannot trigger recognition.
// This is only a presentation heuristic, never evidence of device boot state.
// Timestamp and highlighting renderers share it to avoid buffering each other.
func HasBootloaderPromptPrefix(line []byte) bool {
	return bootloaderPromptPattern.Match(line)
}

var bootloaderPromptPattern = regexp.MustCompile(defaults.Prompts["bootloaderPromptPattern"])

var defaultTheme = newTheme(termenv.TrueColor)

var (
	styleError      = defaultTheme.style(tokenError)
	styleWarning    = defaultTheme.style(tokenWarning)
	styleSuccess    = defaultTheme.style(tokenSuccess)
	styleInfo       = defaultTheme.style(tokenInfo)
	styleDebug      = defaultTheme.style(tokenDebug)
	stylePromptUser = defaultTheme.promptUser
	stylePromptHost = defaultTheme.promptHost
	stylePromptPath = defaultTheme.promptPath
)
