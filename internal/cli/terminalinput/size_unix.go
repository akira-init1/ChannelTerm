//go:build linux || darwin

package terminalinput

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

// sizeChangeWaiter subscribes before the initial recheck to avoid missing a
// window change between measuring the terminal and entering the wait loop.
func sizeChangeWaiter() (func(context.Context) bool, func()) {
	changes := make(chan os.Signal, 1)
	signal.Notify(changes, syscall.SIGWINCH)
	return func(ctx context.Context) bool {
		select {
		case <-ctx.Done():
			return false
		case <-changes:
			return true
		}
	}, func() { signal.Stop(changes) }
}
