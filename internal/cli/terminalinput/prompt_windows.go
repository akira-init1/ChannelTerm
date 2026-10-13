//go:build windows

package terminalinput

import (
	"context"
	"os"
	"unicode/utf16"
	"unicode/utf8"

	"golang.org/x/sys/windows"
)

// Read input records directly so Ctrl+C is local cancellation and a bounded
// wait can observe ctx. No blocked ReadConsole or worker survives the prompt.
func promptByteReader(ctx context.Context, input *os.File) func() (byte, error) {
	handle := windows.Handle(input.Fd())
	source := win32ConsoleEventSource{handle: handle}
	var pending []byte
	var high uint16
	return func() (byte, error) {
		for {
			if err := ctx.Err(); err != nil {
				clear(pending)
				return 0, err
			}
			if len(pending) > 0 {
				value := pending[0]
				pending[0] = 0
				pending = pending[1:]
				return value, nil
			}
			status, err := windows.WaitForSingleObject(handle, 50)
			if err != nil {
				return 0, err
			}
			if status == uint32(windows.WAIT_TIMEOUT) {
				continue
			}
			records, err := source.Peek()
			if err != nil {
				return 0, err
			}
			if len(records) == 0 {
				continue
			}
			record := records[0]
			if err := source.Consume(1); err != nil {
				return 0, err
			}
			if record.eventType != windowsKeyEvent || record.key.keyDown == 0 {
				continue
			}
			if windowsControlCKey(record.key) {
				return 3, nil
			}
			value := record.key.unicodeChar
			if value == 0 {
				continue
			}
			if value >= 0xD800 && value <= 0xDBFF {
				high = value
				continue
			}
			r := rune(value)
			if high != 0 {
				r = utf16.DecodeRune(rune(high), r)
				high = 0
			}
			for i := 0; i < max(1, int(record.key.repeatCount)); i++ {
				pending = utf8.AppendRune(pending, r)
			}
		}
	}
}
