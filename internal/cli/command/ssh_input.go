package command

import (
	"context"
	"errors"
	"io"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/core/session"
)

// forwardSSHOwnerInput gives the owning SSH terminal one ordered write worker.
// Local escape parsing stays on the input dispatcher while a remote write is
// blocked. stopOwner must close the owned Session, which releases that write;
// cancellation joins the worker before local terminal state is restored.
// At most 256 input batches (normally 4 KiB each) are queued. Overflow ends the
// connection explicitly instead of silently dropping bytes or growing memory.
func forwardSSHOwnerInput(ctx context.Context, input io.Reader, terminal attachSession, writeLocal, writeStatus func([]byte) error, toggle func() error, cancel context.CancelFunc, interrupts <-chan os.Signal, stopOwner func()) {
	ctx, stop := context.WithCancel(ctx)
	var once sync.Once
	shutdown := func() { once.Do(func() { stop(); cancel(); stopOwner() }) }
	queue := make(chan []byte, 256)
	done := make(chan struct{})
	var pending atomic.Int64
	go func() {
		defer close(done)
		for {
			select {
			case <-ctx.Done():
				return
			case data := <-queue:
				if ctx.Err() != nil {
					return
				}
				err := writeSession(sshOwnerWriter{ctx: ctx, terminal: terminal}, session.ActorUser, data)
				pending.Add(-1)
				if err != nil {
					if ctx.Err() != nil {
						return
					}
					// A lease/busy rejection is not a connection failure. Preserve
					// the ordinary attachment behavior: report it without replay.
					if writeLocal == nil || writeLocal(writeFailureText(err)) != nil {
						shutdown()
						return
					}
				}
			}
		}
	}()
	defer func() { shutdown(); <-done }()
	pump := newAttachInputPumpContext(ctx, input)
	// A file shortcut must not overtake already queued shell input. Waiting
	// happens in its existing cancellable worker, so local controls stay live.
	runner := func(ctx context.Context, terminal attachSession, output io.Writer, cancelRequested func() bool, direction, first, second string) error {
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for pending.Load() != 0 {
			if cancelRequested() {
				return context.Canceled
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-ticker.C:
			}
		}
		return runAttachShortcutFileTransfer(ctx, terminal, output, cancelRequested, direction, first, second)
	}
	d := newAttachInputDispatcherWithPumpAndInterrupts(ctx, &pump, terminal, writeLocal, toggle, shutdown, runner, interrupts)
	d.writeStatus = writeStatus
	d.writeRemote = func(data []byte) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		pending.Add(1)
		select {
		case queue <- append([]byte(nil), data...):
			return nil
		default:
			pending.Add(-1)
			shutdown()
			return errors.New("SSH input queue full; connection closed, queued input was not replayed")
		}
	}
	d.run()
}

// sshOwnerWriter cancels the local MCP request independently of closing the
// owned remote Channel. Closing only SSH must not leave an HTTP write waiting
// for a response while shutdown is joining the input worker.
type sshOwnerWriter struct {
	ctx      context.Context
	terminal attachSession
}

// Write uses the attachment's contextual request API when available.
func (w sshOwnerWriter) Write(request session.WriteRequest) (int, error) {
	if err := w.ctx.Err(); err != nil {
		return 0, err
	}
	if contextual, ok := w.terminal.(interface {
		WriteContext(context.Context, session.WriteRequest) (int, error)
	}); ok {
		return contextual.WriteContext(w.ctx, request)
	}
	return w.terminal.Write(request)
}
