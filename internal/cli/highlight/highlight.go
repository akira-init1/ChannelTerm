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
// local status output. It never modifies the caller's input slice.
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
// A recognized shell prompt is emitted before a newline so a remote shell can
// remain interactive. The following echoed command is deliberately forwarded
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
			r.pending = append(r.pending, p...)
			if err := r.drain(); err != nil {
				return 0, err
			}
			break
		}

		// Preserve all remote bytes from the escape onward. Complete safe lines
		// before it may still be highlighted; any incomplete prefix is emitted raw.
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
	return nil
}

// drain emits complete lines and the special no-newline shell prompt while
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
		if rendered, ok := r.renderPrompt(r.pending); ok {
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
	if raw || containsScreenControl(line) {
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
	if bytes.HasSuffix(line, []byte("\r\n")) {
		return line[:len(line)-2], line[len(line)-2:]
	}
	if bytes.HasSuffix(line, []byte("\n")) {
		return line[:len(line)-1], line[len(line)-1:]
	}
	return line, nil
}

// containsScreenControl identifies bytes whose presentation cannot be safely
// reconstructed by a line-oriented renderer. Tab, newline, and CRLF line
// endings are retained; standalone carriage returns still force raw output.
func containsScreenControl(data []byte) bool {
	if !utf8.Valid(data) {
		return true
	}
	for index, value := range data {
		if value == '\t' || value == '\n' {
			continue
		}
		if value == '\r' {
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

// renderPrompt recognizes common POSIX and BusyBox prompt forms and styles
// their user, host, path, and privilege marker separately. The path uses bright
// blue because normal ANSI blue is low contrast on default dark backgrounds.
func (r *Renderer) renderPrompt(line []byte) ([]byte, bool) {
	index := promptPattern.FindSubmatchIndex(line)
	if index == nil {
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

type tokenKind uint8

const (
	tokenError tokenKind = iota
	tokenWarning
	tokenSuccess
	tokenHeading
	tokenField
	tokenAddress
	tokenNumber
	tokenVersion
	tokenTimestamp
	tokenUnit
	tokenString
	tokenInfo
	tokenDebug
	tokenInterface
	tokenDelimiter
)

type theme struct {
	styles     map[tokenKind]style
	promptUser style
	promptHost style
	promptPath style
}

func newTheme(profile termenv.Profile) theme {
	numberStyle := colorStyle(profile, "#AF87FF")
	errorStyle := colorStyle(profile, "#FF5F5F")
	return theme{
		styles: map[tokenKind]style{
			tokenError:     errorStyle,
			tokenWarning:   errorStyle,
			tokenSuccess:   colorStyle(profile, "#5FFF87"),
			tokenHeading:   colorStyle(profile, "#CBA94B"),
			tokenField:     colorStyle(profile, "#569CD6"),
			tokenAddress:   numberStyle,
			tokenNumber:    numberStyle,
			tokenVersion:   numberStyle,
			tokenTimestamp: colorStyle(profile, "#87D7AF"),
			tokenUnit:      colorStyle(profile, "#CBA94B"),
			tokenString:    colorStyle(profile, "#CBA94B"),
			tokenInfo:      colorStyle(profile, "#5FD7FF"),
			tokenDebug:     ansiPaletteStyle(profile, 95),
			tokenInterface: colorStyle(profile, "#4EC9B0"),
			tokenDelimiter: delimiterStyle(profile),
		},
		// Keep the original shell-prompt palette while omitting its former SGR 1
		// attribute: red user/marker, cyan host, and bright-blue path.
		promptUser: ansiPaletteStyle(profile, 31),
		promptHost: ansiPaletteStyle(profile, 36),
		promptPath: ansiPaletteStyle(profile, 94),
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

func delimiterStyle(profile termenv.Profile) style {
	if profile == termenv.TrueColor {
		// Keep the user-selected #6DB129 exact. termenv's generic hex
		// conversion rounds this color's blue channel down by one.
		return "\x1b[38;2;109;177;41m"
	}
	return colorStyle(profile, "#6DB129")
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

var builtinRules = []rule{
	{regexp.MustCompile(`(?:^|[^[:alnum:]_])('[^'\r\n]*'|"[^"\r\n]*"|“[^”\r\n]*”|‘[^’\r\n]*’)`), 1, tokenString, 160},
	{regexp.MustCompile(`(?:\(|\)|\[|\]|\{|\})`), 0, tokenDelimiter, 150},
	{regexp.MustCompile(`(?i)\b(?:fatal|panic|critical|abort|error|failed|failure|unable|denied|invalid|disabled|unknown)\b`), 0, tokenError, 120},
	{regexp.MustCompile(`(?i)\bcan(?:[ \t]*not|['’]t|[ \t]+n['’]t)[ \t]+open\b`), 0, tokenError, 119},
	{regexp.MustCompile(`(?i)\bnot[ \t]+properly\b`), 0, tokenError, 118},
	{regexp.MustCompile(`(?i)\bno[ \t]+such\b`), 0, tokenError, 117},
	{regexp.MustCompile(`(?i)\bcan(?:[ \t]*not|['’]t|[ \t]+n['’]t)\b`), 0, tokenError, 118},
	{regexp.MustCompile(`(?i)\bnot\b[ \t]+(?:found|open|ready|available|connected|enabled|mounted|running|configured|valid)\b`), 0, tokenError, 116},
	{regexp.MustCompile(`(?i)\bnot\b`), 0, tokenError, 115},
	{regexp.MustCompile(`(?i)\bno\b`), 0, tokenError, 115},
	{regexp.MustCompile(`(?i)\b(?:warn|warning|timeout|retry|skipped)\b`), 0, tokenWarning, 110},
	{regexp.MustCompile(`(?i)\b(?:ok|ready|success|passed|connected|started|enabled|available)\b`), 0, tokenSuccess, 105},
	{regexp.MustCompile(`\b(?:Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec)[ \t]+[0-9]{1,2}[ \t]+[0-9]{4}[ \t]+-[ \t]+[0-9]{2}:[0-9]{2}:[0-9]{2}(?:[ \t]+[+-][0-9]{4})?\b`), 0, tokenTimestamp, 130},
	{regexp.MustCompile(`\b(?:(?:Mon|Tue|Wed|Thu|Fri|Sat|Sun)[ \t]+)?(?:Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec)[ \t]+[0-9]{1,2}[ \t]+[0-9]{2}:[0-9]{2}:[0-9]{2}(?:[ \t]+[A-Z]{2,5})?(?:[ \t]+[0-9]{4})?\b`), 0, tokenTimestamp, 130},
	{regexp.MustCompile(`\b[0-9]{4}-[0-9]{2}-[0-9]{2}(?:[ T][0-9]{2}:[0-9]{2}:[0-9]{2}(?:Z|[+-][0-9]{2}:?[0-9]{2})?)?\b`), 0, tokenTimestamp, 129},
	{regexp.MustCompile(`(?i)\b0x[0-9a-f]+\b`), 0, tokenAddress, 95},
	{regexp.MustCompile(`(?i)\b0b[01]+\b`), 0, tokenNumber, 95},
	{regexp.MustCompile(`(?i)\b0o[0-7]+\b`), 0, tokenNumber, 95},
	{regexp.MustCompile(`(?i)\b[0-9]+'[hbod][0-9a-f_xz]+\b`), 0, tokenNumber, 95},
	{regexp.MustCompile(`\b[0-9A-Fa-f]{8,16}\b`), 0, tokenAddress, 92},
	{regexp.MustCompile(`(?i)\bv?[0-9]+(?:\.[0-9]+){1,4}(?:[-+._][[:alnum:]-]+)*\b`), 0, tokenVersion, 90},
	{regexp.MustCompile(`\[[ \t]*[0-9]+(?:\.[0-9]+)?\]`), 0, tokenTimestamp, 88},
	{regexp.MustCompile(`\b[0-9]{2}:[0-9]{2}:[0-9]{2}\b|\b(?:Mon|Tue|Wed|Thu|Fri|Sat|Sun|Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec|UTC)\b|[+-][0-9]{4}\b`), 0, tokenTimestamp, 86},
	{regexp.MustCompile(`(?i)\b(?:spi(?:-nor|flash)?|can|i2c|i2s|uart|serial|tty(?:ps|s)?|mmc(?:blk)?|sdhci|mtd|nand|nor|usb(?:core|hid|-storage)?|ehci|xhci|pcie?|gpio|alsa|ethernet|eth|macb|mdio|rgmii|phy|fpga|dmac?|adma|vdma|jtag|rtc|watchdog|wdt|scsi|sata|net|tcp|udp|ipv[46]|rpc|nfs|cpu|smp|srcu|rcu|gic|l2c|vfp|armv[4-9]|mips|riscv)\b`), 0, tokenInterface, 80},
	{regexp.MustCompile(`(?i)\b(mmcblk|ttyps|ttys|sdhci|serial|pcie|uart|spi|can|i2c|i2s|tty|mmc|mtd|usb|ehci|xhci|pci|gpio|eth|mdio|fpga|dmac|dma|adma|vdma|rtc|cpu|armv)[0-9]`), 1, tokenInterface, 80},
	{regexp.MustCompile(`(?i)[0-9]+(?:\.[0-9]+)?(?:e[+-]?[0-9]+)?`), 0, tokenNumber, 70},
	{regexp.MustCompile(`(?i)\b(?:bytes?|KiB|MiB|GiB|KB|MB|GB|kB/s|MB/s|MHz|GHz|BogoMIPS|ms|ns|us|jiffies|bits?)\b`), 0, tokenUnit, 68},
	{regexp.MustCompile(`^[ \t]*([[:alpha:]_][[:alnum:]_./ -]{0,30}:)`), 1, tokenField, 60},
	{regexp.MustCompile(`(?i)\b(?:U-Boot|Linux version|Starting kernel|Booting Linux|Booting kernel|Loading Kernel Image|Loading Device Tree|Flattened Device Tree)\b`), 0, tokenHeading, 58},
	{regexp.MustCompile(`\*\*\*`), 0, tokenHeading, 56},
	{regexp.MustCompile(`##`), 0, tokenDelimiter, 56},
	{regexp.MustCompile(`(?i)\b(?:info|notice)\b`), 0, tokenInfo, 40},
	{regexp.MustCompile(`(?i)\b(?:debug|trace)\b`), 0, tokenDebug, 30},
}

var promptPattern = regexp.MustCompile(`^(?:\[[0-9]{2}:[0-9]{2}:[0-9]{2}\][ \t]+)?(?:\([^)\r\n]+\)[ \t]+)?([A-Za-z0-9_.-]+)@([A-Za-z0-9_.-]+):([^ \t\r\n]+)([$#])[ \t]*`)

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
