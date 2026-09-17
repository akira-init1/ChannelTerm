package highlight

import (
	"strconv"
	"strings"
)

const maxCSIParameterBytes = 128

type vtParserState uint8

const (
	vtGround vtParserState = iota
	vtEscape
	vtCSI
	vtOSC
	vtOSCEscape
	vtString
	vtStringEscape
)

const (
	sgrIntensity uint16 = 1 << iota
	sgrItalic
	sgrUnderline
	sgrBlink
	sgrInverse
	sgrConceal
	sgrStrike
	sgrFrame
	sgrOverline
	sgrForeground
	sgrBackground
	sgrUnderlineColor
	sgrUnknown
)

// vtTracker follows only the persistent presentation state needed to decide
// when line-oriented semantic highlighting can safely resume. It is not a
// terminal emulator and does not interpret screen contents or cursor position.
type vtTracker struct {
	state           vtParserState
	csiParameters   []byte
	alternateScreen bool
	sgrAttributes   uint16
}

// feed consumes one VT byte and reports whether a presentation-neutral
// sequence just established a safe point for ordinary prompt recognition.
// Cursor/screen controls deliberately do not produce that signal.
func (t *vtTracker) feed(value byte) bool {
	if value == 0x18 || value == 0x1a { // CAN and SUB abort an in-progress sequence.
		t.state = vtGround
		t.csiParameters = t.csiParameters[:0]
		return false
	}

	switch t.state {
	case vtGround:
		if value == 0x1b {
			t.state = vtEscape
		}
	case vtEscape:
		switch value {
		case '[':
			t.state = vtCSI
			t.csiParameters = t.csiParameters[:0]
		case ']':
			t.state = vtOSC
		case 'P', 'X', '^', '_':
			t.state = vtString
		case 'c': // RIS resets terminal presentation state.
			t.alternateScreen = false
			t.sgrAttributes = 0
			t.state = vtGround
			return true
		case 0x1b:
			t.state = vtEscape
		default:
			t.state = vtGround
		}
	case vtCSI:
		switch {
		case value == 0x1b:
			t.state = vtEscape
		case value >= 0x30 && value <= 0x3f:
			if len(t.csiParameters) < maxCSIParameterBytes {
				t.csiParameters = append(t.csiParameters, value)
			}
		case value >= 0x20 && value <= 0x2f:
			// Intermediate bytes do not affect the small state subset tracked here.
		case value >= 0x40 && value <= 0x7e:
			promptBoundary := t.finishCSI(value)
			t.state = vtGround
			return promptBoundary && t.canResumeHighlighting()
		default:
			t.state = vtGround
		}
	case vtOSC:
		switch value {
		case 0x07:
			t.state = vtGround
			return t.canResumeHighlighting()
		case 0x1b:
			t.state = vtOSCEscape
		}
	case vtOSCEscape:
		if value == '\\' {
			t.state = vtGround
			return t.canResumeHighlighting()
		} else if value != 0x1b {
			t.state = vtOSC
		}
	case vtString:
		if value == 0x1b {
			t.state = vtStringEscape
		}
	case vtStringEscape:
		if value == '\\' {
			t.state = vtGround
		} else if value != 0x1b {
			t.state = vtString
		}
	}
	return false
}

func (t *vtTracker) finishCSI(final byte) bool {
	parameters := string(t.csiParameters)
	t.csiParameters = t.csiParameters[:0]
	switch final {
	case 'm':
		t.applySGR(parameters)
		return true
	case 'h', 'l':
		if strings.HasPrefix(parameters, "?") {
			enabled := final == 'h'
			promptBoundary := true
			for _, parameter := range strings.Split(strings.TrimPrefix(parameters, "?"), ";") {
				mode, err := strconv.Atoi(parameter)
				if err != nil {
					promptBoundary = false
					continue
				}
				switch mode {
				case 25, 2004:
					// Cursor visibility and bracketed-paste mode do not alter
					// text attributes. Shells commonly set them immediately
					// before emitting their first prompt.
				case 47, 1047, 1049:
					t.alternateScreen = enabled
					promptBoundary = promptBoundary && !enabled
				default:
					promptBoundary = false
				}
			}
			return promptBoundary
		}
	}
	return false
}

func (t *vtTracker) applySGR(parameters string) {
	if parameters == "" {
		t.sgrAttributes = 0
		return
	}
	parts := strings.Split(parameters, ";")
	for index := 0; index < len(parts); index++ {
		part := parts[index]
		if part == "" {
			part = "0"
		}
		if separator := strings.IndexByte(part, ':'); separator >= 0 {
			code, err := strconv.Atoi(part[:separator])
			if err != nil {
				t.sgrAttributes |= sgrUnknown
				continue
			}
			t.setSGRCode(code)
			continue
		}
		code, err := strconv.Atoi(part)
		if err != nil {
			t.sgrAttributes |= sgrUnknown
			continue
		}
		if code == 38 || code == 48 || code == 58 {
			t.setSGRCode(code)
			index += extendedColorParameterCount(parts[index+1:])
			continue
		}
		t.setSGRCode(code)
	}
}

func extendedColorParameterCount(parameters []string) int {
	if len(parameters) == 0 {
		return 0
	}
	switch parameters[0] {
	case "5":
		return min(2, len(parameters))
	case "2":
		return min(4, len(parameters))
	default:
		return 0
	}
}

func (t *vtTracker) setSGRCode(code int) {
	switch {
	case code == 0:
		t.sgrAttributes = 0
	case code == 1 || code == 2:
		t.sgrAttributes |= sgrIntensity
	case code == 3:
		t.sgrAttributes |= sgrItalic
	case code == 4 || code == 21:
		t.sgrAttributes |= sgrUnderline
	case code == 5 || code == 6:
		t.sgrAttributes |= sgrBlink
	case code == 7:
		t.sgrAttributes |= sgrInverse
	case code == 8:
		t.sgrAttributes |= sgrConceal
	case code == 9:
		t.sgrAttributes |= sgrStrike
	case code == 22:
		t.sgrAttributes &^= sgrIntensity
	case code == 23:
		t.sgrAttributes &^= sgrItalic
	case code == 24:
		t.sgrAttributes &^= sgrUnderline
	case code == 25:
		t.sgrAttributes &^= sgrBlink
	case code == 27:
		t.sgrAttributes &^= sgrInverse
	case code == 28:
		t.sgrAttributes &^= sgrConceal
	case code == 29:
		t.sgrAttributes &^= sgrStrike
	case code >= 30 && code <= 37 || code >= 90 && code <= 97 || code == 38:
		t.sgrAttributes |= sgrForeground
	case code == 39:
		t.sgrAttributes &^= sgrForeground
	case code >= 40 && code <= 47 || code >= 100 && code <= 107 || code == 48:
		t.sgrAttributes |= sgrBackground
	case code == 49:
		t.sgrAttributes &^= sgrBackground
	case code == 51 || code == 52:
		t.sgrAttributes |= sgrFrame
	case code == 53:
		t.sgrAttributes |= sgrOverline
	case code == 54:
		t.sgrAttributes &^= sgrFrame
	case code == 55:
		t.sgrAttributes &^= sgrOverline
	case code == 58:
		t.sgrAttributes |= sgrUnderlineColor
	case code == 59:
		t.sgrAttributes &^= sgrUnderlineColor
	default:
		t.sgrAttributes |= sgrUnknown
	}
}

func (t *vtTracker) canResumeHighlighting() bool {
	return t.state == vtGround && !t.alternateScreen && t.sgrAttributes == 0
}
