//go:build windows

package terminalinput

import (
	"fmt"
	"io"
	"unicode/utf16"
	"unicode/utf8"
	"unsafe"

	"golang.org/x/sys/windows"
)

const windowsInputRecordBatch = 32

var (
	windowsKernel32         = windows.NewLazySystemDLL("kernel32.dll")
	windowsPeekConsoleInput = windowsKernel32.NewProc("PeekConsoleInputW")
	windowsReadConsoleInput = windowsKernel32.NewProc("ReadConsoleInputW")
)

// windowsConsoleReader preserves the console host's VT translation for normal
// input while intercepting a queued Esc key record before ReadConsole. Some
// Windows Console paths do not reliably surface a bare Esc through the stream
// API; the explicit record mapping guarantees byte 0x1b to the controller.
type windowsConsoleReader struct {
	events               windowsConsoleEventSource
	pendingBytes         []byte
	pendingHighSurrogate uint16
}

type windowsConsoleEventSource interface {
	Wait() error
	Peek() ([]windowsInputRecord, error)
	Consume(int) error
	ReadUTF16(int) ([]uint16, error)
}

type win32ConsoleEventSource struct {
	handle windows.Handle
}

func newWindowsConsoleReader(handle windows.Handle) io.Reader {
	return &windowsConsoleReader{events: win32ConsoleEventSource{handle: handle}}
}

func (r *windowsConsoleReader) Read(data []byte) (int, error) {
	if len(data) == 0 {
		return 0, nil
	}
	for {
		if len(r.pendingBytes) > 0 {
			count := copy(data, r.pendingBytes)
			r.pendingBytes = r.pendingBytes[count:]
			return count, nil
		}

		if r.pendingHighSurrogate == 0 {
			if err := r.events.Wait(); err != nil {
				return 0, err
			}
		}
		records, err := r.events.Peek()
		if err != nil {
			return 0, err
		}
		plan := windowsEscapePlan(records)
		if plan.escapeRepeats > 0 {
			if err := r.events.Consume(plan.discardRecords); err != nil {
				return 0, err
			}
			r.pendingBytes = make([]byte, plan.escapeRepeats)
			for index := range r.pendingBytes {
				r.pendingBytes[index] = 0x1B
			}
			continue
		}
		readLimit := windowsInputRecordBatch
		if plan.readSingleUTF16 {
			readLimit = 1
		}
		if plan.discardRecords > 0 {
			if err := r.events.Consume(plan.discardRecords); err != nil {
				return 0, err
			}
			continue
		}
		characters, err := r.events.ReadUTF16(readLimit)
		if err != nil {
			return 0, err
		}
		r.appendUTF16(characters)
	}
}

// appendUTF16 converts complete console UTF-16 input to buffered UTF-8 while
// retaining a trailing high surrogate for the next ReadConsole result.
func (r *windowsConsoleReader) appendUTF16(characters []uint16) {
	if r.pendingHighSurrogate != 0 {
		characters = append([]uint16{r.pendingHighSurrogate}, characters...)
		r.pendingHighSurrogate = 0
	}
	if len(characters) > 0 && characters[len(characters)-1] >= 0xD800 && characters[len(characters)-1] <= 0xDBFF {
		r.pendingHighSurrogate = characters[len(characters)-1]
		characters = characters[:len(characters)-1]
	}
	for _, value := range utf16.Decode(characters) {
		r.pendingBytes = utf8.AppendRune(r.pendingBytes, value)
	}
}

func (s win32ConsoleEventSource) Wait() error {
	if _, err := windows.WaitForSingleObject(s.handle, windows.INFINITE); err != nil {
		return fmt.Errorf("wait for console input: %w", err)
	}
	return nil
}

func (s win32ConsoleEventSource) Peek() ([]windowsInputRecord, error) {
	records := make([]windowsInputRecord, windowsInputRecordBatch)
	var count uint32
	result, _, callErr := windowsPeekConsoleInput.Call(
		uintptr(s.handle),
		uintptr(unsafe.Pointer(&records[0])),
		uintptr(len(records)),
		uintptr(unsafe.Pointer(&count)),
	)
	if result == 0 {
		return nil, windowsConsoleCallError("peek console input", callErr)
	}
	return records[:count], nil
}

func (s win32ConsoleEventSource) Consume(count int) error {
	if count == 0 {
		return nil
	}
	records := make([]windowsInputRecord, count)
	var consumed uint32
	result, _, callErr := windowsReadConsoleInput.Call(
		uintptr(s.handle),
		uintptr(unsafe.Pointer(&records[0])),
		uintptr(len(records)),
		uintptr(unsafe.Pointer(&consumed)),
	)
	if result == 0 {
		return windowsConsoleCallError("read console input", callErr)
	}
	if consumed != uint32(count) {
		return fmt.Errorf("read console input: consumed %d of %d queued records", consumed, count)
	}
	return nil
}

func (s win32ConsoleEventSource) ReadUTF16(limit int) ([]uint16, error) {
	characters := make([]uint16, limit)
	var count uint32
	if err := windows.ReadConsole(s.handle, &characters[0], uint32(len(characters)), &count, nil); err != nil {
		return nil, fmt.Errorf("read console input: %w", err)
	}
	return characters[:count], nil
}

func windowsConsoleCallError(operation string, err error) error {
	if err == nil || err == windows.ERROR_SUCCESS {
		return fmt.Errorf("%s failed", operation)
	}
	return fmt.Errorf("%s: %w", operation, err)
}
