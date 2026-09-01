package command

import (
	"bytes"
	"fmt"
	"regexp"
	"time"
)

const maxPromptTimestampPendingBytes = 8 * 1024

// promptTimestampRenderer decorates only recognized shell prompts for one
// local CLI output stream. It buffers an incomplete logical line so arbitrary
// Session read boundaries cannot change detection. The bytes it forwards are
// otherwise unchanged and it never owns Session output.
//
// Callers serialize Write, Toggle, and Flush with their local output writer.
type promptTimestampRenderer struct {
	write           func([]byte) error
	flush           func() error
	now             func() time.Time
	enabled         bool
	pending         []byte
	rawUntilNewline bool
}

// newPromptTimestampRenderer creates an initially disabled local prompt
// timestamp renderer. write receives terminal bytes after any further local
// presentation processing, while flush releases that processor's pending data.
func newPromptTimestampRenderer(write func([]byte) error, flush func() error, now func() time.Time) *promptTimestampRenderer {
	if write == nil {
		write = func([]byte) error { return nil }
	}
	if flush == nil {
		flush = func() error { return nil }
	}
	if now == nil {
		now = time.Now
	}
	return &promptTimestampRenderer{write: write, flush: flush, now: now}
}

// Enabled reports whether timestamps are currently enabled for this renderer.
func (r *promptTimestampRenderer) Enabled() bool {
	return r.enabled
}

// Toggle flushes preceding remote presentation before changing the local
// setting. This prevents a partly received prompt from being stamped according
// to a setting that was selected after its bytes arrived.
func (r *promptTimestampRenderer) Toggle() (bool, error) {
	if err := r.flushPending(); err != nil {
		return r.enabled, err
	}
	r.enabled = !r.enabled
	return r.enabled, nil
}

// Write forwards remote bytes unchanged when disabled. When enabled, it adds a
// local timestamp only before a conservatively recognized prompt; ordinary
// output remains byte-for-byte unchanged.
func (r *promptTimestampRenderer) Write(data []byte) error {
	if !r.enabled {
		return r.write(data)
	}
	for len(data) > 0 {
		if r.rawUntilNewline {
			if lineEnd := bytes.IndexAny(data, "\r\n"); lineEnd >= 0 {
				if err := r.write(data[:lineEnd+1]); err != nil {
					return err
				}
				data = data[lineEnd+1:]
				r.rawUntilNewline = false
				continue
			}
			return r.write(data)
		}

		if newline := bytes.IndexByte(data, '\n'); newline >= 0 {
			r.pending = append(r.pending, data[:newline+1]...)
			if err := r.write(r.pending); err != nil {
				return err
			}
			r.pending = nil
			data = data[newline+1:]
			continue
		}

		r.pending = append(r.pending, data...)
		data = nil
		visible, complete, safe := promptVisibleText(r.pending)
		if !safe || len(r.pending) > maxPromptTimestampPendingBytes {
			if err := r.write(r.pending); err != nil {
				return err
			}
			r.pending = nil
			return nil
		}
		if !complete {
			return nil
		}
		if promptPattern.Match(visible) {
			stamp := []byte(fmt.Sprintf("[%s] ", r.now().Format("15:04:05")))
			stamped := make([]byte, 0, len(stamp)+len(r.pending))
			stamped = append(stamped, stamp...)
			stamped = append(stamped, r.pending...)
			if err := r.write(stamped); err != nil {
				return err
			}
			r.pending = nil
			r.rawUntilNewline = true
		}
	}
	return nil
}

// Flush writes an incomplete candidate as raw remote data before flushing the
// downstream local renderer during orderly disconnect.
func (r *promptTimestampRenderer) Flush() error {
	return r.flushPending()
}

func (r *promptTimestampRenderer) flushPending() error {
	if len(r.pending) > 0 {
		if err := r.write(r.pending); err != nil {
			return err
		}
		r.pending = nil
	}
	r.rawUntilNewline = false
	return r.flush()
}

// promptVisibleText removes complete SGR color sequences only for prompt
// recognition. It returns safe=false for editing or screen-control bytes so
// full-screen applications and terminal controls always pass through raw.
func promptVisibleText(data []byte) (visible []byte, complete, safe bool) {
	visible = make([]byte, 0, len(data))
	for index := 0; index < len(data); index++ {
		value := data[index]
		if value == 0x1b {
			if index+1 == len(data) {
				return visible, false, true
			}
			if data[index+1] != '[' {
				return visible, true, false
			}
			end := index + 2
			for end < len(data) && data[end] >= 0x30 && data[end] <= 0x3f {
				end++
			}
			if end == len(data) {
				return visible, false, true
			}
			if data[end] != 'm' {
				return visible, true, false
			}
			index = end
			continue
		}
		if value < 0x20 || value == 0x7f {
			return visible, true, false
		}
		visible = append(visible, value)
	}
	return visible, true, true
}

var promptPattern = regexp.MustCompile(`^(?:[A-Za-z0-9_.-]+@[A-Za-z0-9_.-]+:[^ \t\r\n]+[$#]|(?:ba)?sh-[0-9]+(?:\.[0-9]+)*#|/[ \t]+#|\[[A-Za-z0-9_.-]+@[A-Za-z0-9_.-]+[ \t]+[^ \t\r\n\]]+\]#)[ \t]*$`)
