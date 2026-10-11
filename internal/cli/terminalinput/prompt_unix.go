//go:build !windows

package terminalinput

import (
	"context"
	"errors"
	"io"
	"os"

	"golang.org/x/sys/unix"
)

// Polling a raw terminal lets cancellation restore termios without a lingering
// reader that could steal the next prompt's input. ReadLine owns stdin alone.
func promptByteReader(ctx context.Context, input *os.File) func() (byte, error) {
	fd := int(input.Fd())
	return func() (byte, error) {
		for {
			if err := ctx.Err(); err != nil {
				return 0, err
			}
			fds := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
			n, err := unix.Poll(fds, 50)
			if errors.Is(err, unix.EINTR) {
				continue
			}
			if err != nil {
				return 0, err
			}
			if n == 0 {
				continue
			}
			if fds[0].Revents&unix.POLLNVAL != 0 {
				return 0, os.ErrClosed
			}
			var b [1]byte
			n, err = unix.Read(fd, b[:])
			if errors.Is(err, unix.EINTR) || errors.Is(err, unix.EAGAIN) {
				continue
			}
			if err != nil {
				return 0, err
			}
			if n == 0 {
				return 0, io.EOF
			}
			return b[0], nil
		}
	}
}
