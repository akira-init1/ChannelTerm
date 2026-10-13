package ssh

import (
	"context"
	"errors"
	"fmt"
	"net"
)

// StageError identifies the failed SSH operation without exposing credentials.
// Stage is identity, dns, connect, host-key, authentication, handshake, shell,
// subsystem, transfer, or liveness. Unwrap preserves cancellation and underlying errors.
type StageError struct {
	Stage string
	Err   error
}

// Error formats the stage and cause, distinguishing cancellation and timeout.
func (e *StageError) Error() string {
	kind := "failed"
	var network net.Error
	if errors.Is(e.Err, context.Canceled) {
		kind = "cancelled"
	} else if errors.Is(e.Err, context.DeadlineExceeded) || errors.As(e.Err, &network) && network.Timeout() {
		kind = "timed out"
	}
	return fmt.Sprintf("SSH %s %s: %v", e.Stage, kind, e.Err)
}

// Unwrap exposes the original error for errors.Is and errors.As.
func (e *StageError) Unwrap() error { return e.Err }

func stageError(stage string, err error) error {
	if err == nil {
		return nil
	}
	var existing *StageError
	if errors.As(err, &existing) {
		return err
	}
	return &StageError{Stage: stage, Err: err}
}
