package terminalinput

import (
	"context"
	"errors"
	"io"
	"os"
	"unicode/utf8"

	"golang.org/x/term"
)

// ReadLine reads one local terminal response, with optional echo suppression.
// It requires an actual terminal, restores its original mode on every exit,
// and supports Ctrl+C, EOF, backspace, and context cancellation. It starts no
// background input reader and does not consume bytes beyond the response.
// The caller owns and should clear the returned bytes when they are secret.
func ReadLine(ctx context.Context, input *os.File, output io.Writer, prompt string, secret bool) (value []byte, err error) {
	if input == nil || !term.IsTerminal(int(input.Fd())) {
		return nil, errors.New("interactive input requires a terminal on stdin")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	_, restore, err := MakeRaw(input)
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, restore()) }()
	if _, err := io.WriteString(output, prompt); err != nil {
		return nil, err
	}
	defer func() {
		_, newlineErr := io.WriteString(output, "\r\n")
		err = errors.Join(err, newlineErr)
	}()
	return readPromptLine(ctx, promptByteReader(ctx, input), output, secret)
}

func readPromptLine(ctx context.Context, readByte func() (byte, error), output io.Writer, secret bool) ([]byte, error) {
	// A fixed backing array can be fully cleared even after backspace. Never
	// include this buffer in diagnostics or leave a secret in an input pump.
	var storage [4096]byte
	defer clear(storage[:])
	length := 0
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		value, err := readByte()
		if err != nil {
			return nil, err
		}
		switch value {
		case '\r', '\n':
			return append([]byte(nil), storage[:length]...), nil
		case 3:
			return nil, context.Canceled
		case 4, 26:
			return nil, io.EOF
		case 8, 127:
			if length > 0 {
				_, size := utf8.DecodeLastRune(storage[:length])
				clear(storage[length-size : length])
				length -= size
				if !secret {
					if _, err := io.WriteString(output, "\b \b"); err != nil {
						return nil, err
					}
				}
			}
		default:
			if value < 32 {
				continue
			}
			if length == len(storage) {
				return nil, errors.New("terminal response exceeds 4096 bytes")
			}
			storage[length] = value
			length++
			if !secret {
				if _, err := output.Write([]byte{value}); err != nil {
					return nil, err
				}
			}
		}
	}
}
