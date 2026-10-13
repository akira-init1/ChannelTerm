//go:build !linux && !darwin

package terminalinput

import (
	"context"
	"time"
)

// sizeChangeWaiter polls on Windows, where SIGWINCH is unavailable. GetSize
// queries the visible console window without competing with the keyboard reader
// for WINDOW_BUFFER_SIZE_EVENT records. No extra worker goroutine is required.
func sizeChangeWaiter() (func(context.Context) bool, func()) {
	ticker := time.NewTicker(250 * time.Millisecond)
	return func(ctx context.Context) bool {
		select {
		case <-ctx.Done():
			return false
		case <-ticker.C:
			return true
		}
	}, ticker.Stop
}
