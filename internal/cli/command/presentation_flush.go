package command

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/cli/highlight"
)

const presentationFlushInterval = 50 * time.Millisecond

// startPresentationFlush bounds incomplete-line buffering independently of
// remote reads and local keystrokes. A fixed cadence cannot be postponed by a
// slow continuous stream. All renderer access uses the caller's output mutex,
// including local status output. It never writes input back to the Session.
//
// A write failure cancels the owning CLI operation. The returned stop function
// is idempotent, joins the worker, and reports that failure. Call it before final
// status output and defer it on every exit so no worker outlives the CLI stream.
func startPresentationFlush(ctx context.Context, outputMu *sync.Mutex, timestamps *promptTimestampRenderer, highlighter *highlight.Renderer, cancel context.CancelFunc) func() error {
	flushCtx, stop := context.WithCancel(ctx)
	done := make(chan struct{})
	var flushErr error
	go func() {
		defer close(done)
		ticker := time.NewTicker(presentationFlushInterval)
		defer ticker.Stop()
		for {
			select {
			case <-flushCtx.Done():
				return
			case <-ticker.C:
				outputMu.Lock()
				if flushCtx.Err() == nil {
					flushErr = timestamps.releasePending()
					if flushErr == nil && highlighter != nil {
						flushErr = highlighter.ReleasePending()
					}
				}
				outputMu.Unlock()
				if flushErr != nil {
					cancel()
					return
				}
			}
		}
	}()
	return func() error {
		stop()
		<-done
		if flushErr != nil {
			return fmt.Errorf("flush pending terminal output: %w", flushErr)
		}
		return nil
	}
}
