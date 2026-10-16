package command

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// previewTerminalText bounds local metadata rendering without changing retained
// data. Escape control/invalid bytes so reported text cannot execute terminal
// sequences. Limits count rendered bytes and logical lines; UTF-8 is never split.
func previewTerminalText(text string, maxBytes, maxLines int) (string, bool) {
	var out strings.Builder
	lines := 1
	for offset := 0; offset < len(text); {
		r, size := utf8.DecodeRuneInString(text[offset:])
		part := string(r)
		switch {
		case r == utf8.RuneError && size == 1:
			part = fmt.Sprintf("\\x%02x", text[offset])
		case r == '\n':
			if lines >= maxLines {
				return out.String(), true
			}
			lines++
			part = "\r\n"
		case !unicode.IsPrint(r):
			quoted := strconv.QuoteRune(r)
			part = quoted[1 : len(quoted)-1]
		}
		if out.Len()+len(part) > maxBytes {
			return out.String(), true
		}
		out.WriteString(part)
		offset += size
	}
	return out.String(), false
}
