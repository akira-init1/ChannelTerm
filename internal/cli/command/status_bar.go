package command

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/akira-init1/ChannelTerm/internal/cli/terminalinput"
	"github.com/muesli/termenv"
	"golang.org/x/term"
)

// statusBar is a local, line-oriented output coordinator, not a VT emulator.
// Its lock serializes every display byte and status update. It buffers at most
// one incomplete UTF-8/CSI unit; unknown controls permanently restore full-screen
// output before forwarding the original bytes. It never receives device input.
type statusBar struct {
	column                  int
	failure                 error
	mu                      sync.Mutex
	output                  io.Writer
	size                    terminalinput.Size
	measure                 func() (terminalinput.Size, error)
	pending                 []byte
	active, started, closed bool
	text, disabled          string
}

// newConsoleStatusBar requires real interactive input/output and known geometry.
// Pipes, dumb terminals and explicit opt-out retain exactly the old output path.
func newConsoleStatusBar(input io.Reader, output io.Writer, enabled bool) *statusBar {
	in, inOK := input.(*os.File)
	out, outOK := output.(*os.File)
	if !enabled || !inOK || !outOK || !term.IsTerminal(int(in.Fd())) || !term.IsTerminal(int(out.Fd())) || os.Getenv("TERM") == "dumb" {
		return nil
	}
	size, err := terminalinput.ReadSize(out)
	if err != nil || size.Columns < 40 || size.Rows < 4 || size.Columns > 1000 || size.Rows > 1000 || !enableANSIOutput(out) {
		return nil
	}
	return &statusBar{output: output, size: size, measure: func() (terminalinput.Size, error) { return terminalinput.ReadSize(out) }, active: true, text: "Control unknown | Ctrl+] i: take control"}
}

// startLocked scrolls existing screen contents into scrollback rather than
// clearing them, creating two blank rows for a prompt and the local footer.
// This limited presentation requires an ordinary primary terminal screen.
func (b *statusBar) startLocked() error {
	if b.started || !b.active {
		return nil
	}
	b.started = true
	if err := writeAll(b.output, []byte(fmt.Sprintf("\x1b[%d;1H\r\n\r\n\x1b[1;%dr\x1b[%d;1H", b.size.Rows, b.size.Rows-1, b.size.Rows-1))); err != nil {
		return err
	}
	return b.drawLocked(b.text)
}

func statusCells(text string, width int) string {
	var s strings.Builder
	for _, r := range text {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			r = ' '
		}
		candidate := s.String() + string(r)
		if termenv.String(candidate).Width() > width {
			break
		}
		s.WriteRune(r)
		if s.Len() > 2048 {
			break
		}
	}
	return s.String()
}

func (b *statusBar) drawLocked(text string) error {
	// DECSC/DECRC preserve cursor and rendition, but not reliably wrap-pending
	// state. trackUnit exits before that state; the footer also leaves a cell free.
	return writeAll(b.output, []byte(fmt.Sprintf("\x1b7\x1b[%d;1H\x1b[0m\x1b[2K%s\x1b8", b.size.Rows, statusCells(text, int(b.size.Columns)-1))))
}

func (b *statusBar) disableLocked(reason string, resized bool) error {
	if !b.active {
		return nil
	}
	b.active = false
	b.disabled = reason
	if !b.started {
		return nil
	}
	var noticeErr error
	if !resized {
		noticeErr = b.drawLocked("")
	}
	// Resizing may already have reflowed the footer into scrollback: never erase
	// guessed coordinates, which could now contain device text.
	return errors.Join(noticeErr, writeAll(b.output, []byte("\x1b7\x1b[r\x1b8")))
}

func (b *statusBar) checkSizeLocked() error {
	if b.active && b.measure != nil {
		size, err := b.measure()
		if err != nil || size != b.size {
			return b.disableLocked("terminal resized or size unavailable", true)
		}
	}
	return nil
}

// statusUnit classifies a complete safe display unit. Zero means incomplete;
// negative means unsupported. CSI limits prevent unbounded pending storage.
func statusUnit(p []byte) int {
	if p[0] == 0x1b {
		if len(p) == 1 {
			return 0
		}
		if p[1] != '[' {
			return -1
		}
		for i := 2; i < len(p); i++ {
			c := p[i]
			if i >= 64 {
				return -1
			}
			if c >= 0x40 && c <= 0x7e {
				args := string(p[2:i])
				if (c == 'h' || c == 'l') && args == "?2004" {
					return i + 1
				}
				if c != 'm' && c != 'K' && c != 'C' && c != 'D' && c != 'G' {
					return -1
				}
				for _, a := range args {
					if (a < '0' || a > '9') && a != ';' && (c != 'm' || a != ':') {
						return -1
					}
				}
				return i + 1
			}
			if c < 0x20 || c > 0x3f {
				return -1
			}
		}
		return 0
	}
	if p[0] < 0x20 || p[0] == 0x7f {
		switch p[0] {
		case 0, 7, 8, 9, 10, 13, 127:
			return 1
		default:
			return -1
		}
	}
	if !utf8.FullRune(p) {
		return 0
	}
	r, n := utf8.DecodeRune(p)
	if r == utf8.RuneError && n == 1 || r >= 0x80 && r <= 0x9f {
		return -1
	}
	return n
}

// trackUnit conservatively avoids wrap-pending cursor state: real terminals may
// lose it across DECSC/DECRC. Give the full screen back before the last cell is
// filled. Non-ASCII cells count as two; combining/format sequences downgrade
// rather than pretending this small coordinator implements grapheme reflow.
func (b *statusBar) trackUnit(p []byte) bool {
	if p[0] == 0x1b {
		final := p[len(p)-1]
		if final == 'C' || final == 'D' || final == 'G' {
			arg := strings.Split(string(p[2:len(p)-1]), ";")[0]
			n, err := strconv.Atoi(arg)
			if err != nil && arg != "" {
				return false
			}
			if n == 0 {
				n = 1
			}
			// Clamp before arithmetic: untrusted CSI counts may approach MaxInt.
			n = min(n, int(b.size.Columns))
			switch final {
			case 'C':
				b.column = min(int(b.size.Columns)-1, b.column+n)
			case 'D':
				b.column = max(0, b.column-n)
			case 'G':
				b.column = min(int(b.size.Columns)-1, n-1)
			}
		}
		return true
	}
	switch p[0] {
	case '\r':
		b.column = 0
		return true
	case '\b':
		b.column = max(0, b.column-1)
		return true
	case '\t':
		b.column = min(int(b.size.Columns)-1, (b.column/8+1)*8)
		return true
	case 0, 7, 10, 127:
		return true
	}
	r, _ := utf8.DecodeRune(p)
	if unicode.Is(unicode.M, r) || unicode.Is(unicode.Cf, r) {
		return false
	}
	width := 1
	if r > 127 {
		width = 2
	}
	if b.column+width >= int(b.size.Columns) {
		return false
	}
	b.column += width
	return true
}

// Write forwards presentation bytes unchanged apart from local footer controls.
// Once disabled, no parser or redraw can alter subsequent full-screen output.
func (b *statusBar) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		n, err := b.output.Write(p)
		return n, err
	}
	if err := b.checkSizeLocked(); err != nil {
		return 0, err
	}
	if err := b.startLocked(); err != nil {
		return 0, err
	}
	data := append(b.pending, p...)
	b.pending = nil
	if b.closed || !b.active {
		if err := writeAll(b.output, data); err != nil {
			return 0, err
		}
		return len(p), nil
	}
	pos := 0
	for pos < len(data) {
		n := statusUnit(data[pos:])
		if n == 0 {
			break
		}
		reason := "unsupported terminal control"
		if n > 0 && !b.trackUnit(data[pos:pos+n]) {
			n = -1
			reason = "line reached right edge or complex Unicode"
		}
		if n < 0 {
			if err := writeAll(b.output, data[:pos]); err != nil {
				return 0, err
			}
			if err := b.disableLocked(reason, false); err != nil {
				return 0, err
			}
			if err := writeAll(b.output, data[pos:]); err != nil {
				return 0, err
			}
			return len(p), nil
		}
		pos += n
	}
	if err := writeAll(b.output, data[:pos]); err != nil {
		return 0, err
	}
	b.pending = append([]byte(nil), data[pos:]...)
	return len(p), nil
}

func (b *statusBar) update(text string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed || !b.active {
		return nil
	}
	if err := b.checkSizeLocked(); err != nil {
		return err
	}
	if !b.active {
		return nil
	}
	if err := b.startLocked(); err != nil {
		return err
	}
	if b.text == text {
		return nil
	}
	b.text = text
	return b.drawLocked(text)
}

func (b *statusBar) enabled() bool { b.mu.Lock(); defer b.mu.Unlock(); return b.active && !b.closed }

// Close restores the full scrolling region exactly once and flushes incomplete
// device bytes only after restoration. The underlying output stays caller-owned.
func (b *statusBar) Close() error {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil
	}
	b.closed = true
	err := errors.Join(b.failure, b.checkSizeLocked())
	if b.started && b.active {
		err = errors.Join(err, b.drawLocked(""), writeAll(b.output, []byte("\x1b7\x1b[r\x1b8")))
	}
	b.active = false
	tail := b.pending
	b.pending = nil
	err = errors.Join(err, writeAll(b.output, tail))
	if b.disabled != "" {
		err = errors.Join(err, writeAll(b.output, []byte("\r\n[ChannelTerm] Status bar disabled: "+b.disabled+". Reattach to enable it again.\r\n")))
	}
	return err
}

// runStatusBar polls only public observations, never an input/control/lease API.
// One bounded request at a time avoids queues; cancellation joins before cleanup.
func runStatusBar(ctx context.Context, b *statusBar, read func(context.Context) (string, error), cancelOutput context.CancelFunc) func() {
	if b == nil {
		return func() {}
	}
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for b.enabled() {
			request, stop := context.WithTimeout(ctx, 750*time.Millisecond)
			text, err := read(request)
			stop()
			if ctx.Err() != nil {
				return
			}
			if err != nil {
				text = "Control unknown (Host unavailable) | Ctrl+] i: take control"
			}
			if err := b.update(text); err != nil {
				b.mu.Lock()
				b.failure = err
				b.mu.Unlock()
				cancelOutput()
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	return func() { cancel(); <-done }
}
