//go:build !linux

package app

import (
	"context"
	"errors"
)

type debugProcess struct{}

func (*debugProcess) close() {}
func validateDebugStart(*DebugStartRequest) error {
	return errors.New("user-space GDB debugging requires one native Linux environment")
}
func (*debugRun) start(context.Context) error { return errors.New("GDB backend requires Linux") }
func (*debugRun) stop(bool) error             { return errors.New("GDB backend requires Linux") }

func (*debugRun) heartbeat() error                 { return nil }
func (*debugRun) openServer(context.Context) error { return errors.New("GDB backend requires Linux") }
