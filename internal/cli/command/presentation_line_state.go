package command

// presentationLineState records whether the locally visible terminal is at a
// line boundary. It is intentionally presentation-only: raw Session data and
// cursors remain unchanged.
type presentationLineState struct {
	atLineStart bool
}

func newPresentationLineState() *presentationLineState {
	return &presentationLineState{atLineStart: true}
}

// observe records bytes that were rendered to the local terminal. Both CRLF
// and the platform-specific standalone CR form a local line boundary.
func (s *presentationLineState) observe(data []byte) {
	if len(data) == 0 {
		return
	}
	last := data[len(data)-1]
	s.atLineStart = last == '\r' || last == '\n'
}

// lineStartPrefix returns the one local line ending needed before a status
// block. It never adds a second blank line when output already ended a line.
func (s *presentationLineState) lineStartPrefix() []byte {
	if s.atLineStart {
		return nil
	}
	return []byte("\r\n")
}
