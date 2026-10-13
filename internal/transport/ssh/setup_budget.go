package ssh

import (
	"context"
	"sync"
	"time"
)

// setupBudget counts only network setup time. Pausing never suspends the
// caller's cancellation or deadline. The mutex and deadline check prevent a
// timer callback already queued before Stop from expiring a resumed budget.
type setupBudget struct {
	ctx       context.Context
	cancel    context.CancelCauseFunc
	mu        sync.Mutex
	timer     *time.Timer
	remaining time.Duration
	started   time.Time
	running   bool
}

func newSetupBudget(ctx context.Context, timeout time.Duration) *setupBudget {
	ctx, cancel := context.WithCancelCause(ctx)
	b := &setupBudget{ctx: ctx, cancel: cancel, remaining: timeout}
	b.resume()
	return b
}

func (b *setupBudget) resume() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.running || b.ctx.Err() != nil {
		return
	}
	b.started, b.running = time.Now(), true
	b.timer = time.AfterFunc(b.remaining, func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if b.running && time.Since(b.started) >= b.remaining {
			b.cancel(context.DeadlineExceeded)
		}
	})
}

func (b *setupBudget) pause() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.running {
		return
	}
	b.timer.Stop()
	b.remaining -= time.Since(b.started)
	b.running = false
	if b.remaining <= 0 {
		b.cancel(context.DeadlineExceeded)
	}
}
