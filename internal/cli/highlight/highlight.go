// Package highlight renders safe, line-oriented ANSI emphasis for terminal output.
//
// It is deliberately a presentation-only layer. Callers retain the original
// terminal bytes in Session and pass a separate output stream to Renderer. Its
// generated styles use bright ANSI colors so important terminal text remains
// visibly distinct in mainstream default dark terminal themes.
package highlight

import (
	"bytes"
	"io"
	"regexp"
	"sort"
	"unicode/utf8"
)

const maxPendingBytes = 8 * 1024

const (
	// Use bright colors rather than their normal counterparts. In particular,
	// normal ANSI blue is too dark on the default black backgrounds used by many
	// desktop terminals, which made shell paths such as "~" difficult to read.
	styleError    style = "\x1b[1;91m"
	styleWarning  style = "\x1b[1;93m"
	styleNegation style = "\x1b[1;93m"
	styleSuccess  style = "\x1b[1;92m"
	styleInfo     style = "\x1b[1;96m"
	styleDebug    style = "\x1b[1;95m"
	// Keep the established prompt hierarchy for user and host. Only the path
	// deviates from the original prompt palette because normal blue is too dark.
	stylePromptUser style = "\x1b[1;31m"
	stylePromptHost style = "\x1b[36m"
	stylePromptPath style = "\x1b[1;94m"
	styleReset      style = "\x1b[0m"
)

// Renderer incrementally renders terminal text to one local output stream.
//
// Renderer is not safe for concurrent use. One attached terminal client must
// own one Renderer and serialize Write and Flush calls around any unrelated
// local status output. It never modifies the caller's input slice.
type Renderer struct {
	output          io.Writer
	pending         []byte
	rawUntilNewline bool
}

// New creates a Renderer that writes styled terminal output to output.
//
// A nil output discards rendered bytes. It is useful for callers that need to
// exercise parser behavior without a local terminal writer.
func New(output io.Writer) *Renderer {
	if output == nil {
		output = io.Discard
	}
	return &Renderer{output: output}
}

// Write accepts terminal bytes and forwards complete safe lines with ANSI
// emphasis. Lines containing ANSI or screen-control bytes are forwarded
// unchanged so the renderer cannot corrupt interactive terminal programs.
//
// A recognized shell prompt is emitted before a newline so a remote shell can
// remain interactive. The following echoed command is deliberately forwarded
// unchanged until its newline.
func (r *Renderer) Write(p []byte) (int, error) {
	r.pending = append(r.pending, p...)
	if err := r.drain(); err != nil {
		return 0, err
	}
	return len(p), nil
}

// Flush writes any incomplete terminal line. It is intended for orderly
// disconnect handling; callers should invoke it before printing a local
// disconnect status so buffered remote output remains ordered.
func (r *Renderer) Flush() error {
	if len(r.pending) == 0 {
		return nil
	}
	if err := r.writeLine(r.pending, r.rawUntilNewline); err != nil {
		return err
	}
	r.pending = nil
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
		if rendered, ok := renderPrompt(r.pending); ok {
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
	return writeAll(r.output, renderText(line))
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
func renderText(line []byte) []byte {
	matches := make([]match, 0)
	for _, rule := range builtinRules {
		for _, index := range rule.pattern.FindAllIndex(line, -1) {
			matches = append(matches, match{start: index[0], end: index[1], style: rule.style, priority: rule.priority})
		}
	}
	return renderMatches(line, matches)
}

// renderPrompt recognizes common POSIX and BusyBox prompt forms and styles
// their user, host, path, and privilege marker separately. The path uses bright
// blue because normal ANSI blue is low contrast on default dark backgrounds.
func renderPrompt(line []byte) ([]byte, bool) {
	index := promptPattern.FindSubmatchIndex(line)
	if index == nil {
		return nil, false
	}
	userStart, userEnd := index[2], index[3]
	markerStart, markerEnd := index[8], index[9]
	return renderMatches(line, []match{
		{start: userStart, end: userEnd, style: stylePromptUser, priority: 1},
		{start: index[4], end: index[5], style: stylePromptHost, priority: 1},
		{start: index[6], end: index[7], style: stylePromptPath, priority: 1},
		{start: markerStart, end: markerEnd, style: stylePromptUser, priority: 1},
	}), true
}

// renderMatches selects non-overlapping ranges before inserting ANSI sequences
// so a broad negation rule cannot nest styling within a stronger failure rule.
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
		if candidate.start == candidate.end || overlaps(candidate, selected) {
			continue
		}
		selected = append(selected, candidate)
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

// overlaps reports whether candidate would style bytes that an earlier,
// higher-priority match already owns.
func overlaps(candidate match, selected []match) bool {
	for _, existing := range selected {
		if candidate.start < existing.end && existing.start < candidate.end {
			return true
		}
	}
	return false
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

type rule struct {
	pattern  *regexp.Regexp
	style    style
	priority int
}

type match struct {
	start    int
	end      int
	style    style
	priority int
}

var builtinRules = []rule{
	{regexp.MustCompile(`(?i)\b(?:fatal|panic|critical|abort|error|failed|failure|unable|denied|invalid)\b`), styleError, 100},
	{regexp.MustCompile(`(?i)\bcan(?:[ \t]*not|['’]t|[ \t]+n['’]t)\b`), styleError, 95},
	{regexp.MustCompile(`(?i)\bnot\b[ \t]+(?:found|open|ready|available|connected|enabled|mounted|running|configured|valid)\b`), styleError, 90},
	{regexp.MustCompile(`(?i)\b(?:warn|warning|timeout|retry)\b`), styleWarning, 80},
	{regexp.MustCompile(`(?i)\b(?:ok|ready|success|passed|connected|started)\b`), styleSuccess, 70},
	{regexp.MustCompile(`(?i)\bnot\b(?:[ \t]+[[:alnum:]_./:'’-]+){1,6}`), styleNegation, 50},
	{regexp.MustCompile(`(?i)\b(?:info|notice)\b`), styleInfo, 40},
	{regexp.MustCompile(`(?i)\b(?:debug|trace)\b`), styleDebug, 30},
}

var promptPattern = regexp.MustCompile(`^(?:\([^)\r\n]+\)[ \t]+)?([A-Za-z0-9_.-]+)@([A-Za-z0-9_.-]+):([^ \t\r\n]+)([$#])[ \t]*$`)
