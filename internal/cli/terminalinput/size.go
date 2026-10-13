package terminalinput

import (
	"context"
	"errors"
	"os"

	"golang.org/x/term"
)

// Size is a validated terminal width and height in character cells.
// Its zero value represents an unavailable initial measurement.
type Size struct {
	Columns uint16
	Rows    uint16
}

// ReadSize reads a terminal's visible dimensions without changing console mode.
// It rejects unavailable, zero, or oversized measurements before conversion to
// uint16. The caller retains ownership of file.
func ReadSize(file *os.File) (Size, error) {
	cols, rows, err := term.GetSize(int(file.Fd()))
	if err != nil {
		return Size{}, err
	}
	return checkedSize(cols, rows)
}

func checkedSize(cols, rows int) (Size, error) {
	if cols < 1 || cols > 65535 || rows < 1 || rows > 65535 {
		return Size{}, errors.New("terminal dimensions must be between 1 and 65535")
	}
	return Size{Columns: uint16(cols), Rows: uint16(rows)}, nil
}

// WatchSize synchronously forwards valid changes relative to the local initial
// measurement. It rechecks after subscribing to catch changes during connection
// setup. An unchanged local size preserves explicit initial remote dimensions.
// Linux/macOS use SIGWINCH; Windows polls without consuming console input events.
// Invalid or unavailable measurements are ignored until the next notification.
// ctx stops watching, but cannot interrupt an in-flight resize callback: callers
// must release its underlying I/O before joining WatchSize during shutdown.
func WatchSize(ctx context.Context, file *os.File, initial Size, resize func(cols, rows uint16) error) error {
	wait, stop := sizeChangeWaiter()
	defer stop()
	return watchSize(ctx, initial, func() (Size, error) { return ReadSize(file) }, wait, resize)
}

// watchSize sends only the latest measurement, serially. OS notifications can
// coalesce without losing the eventual dimensions or building an unbounded queue.
func watchSize(ctx context.Context, last Size, read func() (Size, error), wait func(context.Context) bool, resize func(uint16, uint16) error) error {
	for {
		if ctx.Err() != nil {
			return nil
		}
		size, err := read()
		if err == nil && size.Columns != 0 && size.Rows != 0 && size != last {
			if ctx.Err() != nil {
				return nil
			}
			if err := resize(size.Columns, size.Rows); err != nil {
				return err
			}
			last = size
		}
		if !wait(ctx) {
			return nil
		}
	}
}
