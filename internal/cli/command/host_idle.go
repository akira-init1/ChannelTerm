package command

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/core/app"
	"github.com/akira-init1/ChannelTerm/internal/core/config"
)

func resolveHostIdlePolicy(flags *flag.FlagSet, file config.Host, sessionTimeout, exitTimeout time.Duration, origin, transport string, temporary bool) (app.IdlePolicy, error) {
	set := make(map[string]bool)
	flags.Visit(func(f *flag.Flag) { set[f.Name] = true })
	if origin != "manual" && origin != "desktop" {
		return app.IdlePolicy{}, errors.New("host-origin must be manual or desktop")
	}
	if temporary {
		if set["idle-session-timeout"] || set["idle-exit-timeout"] || origin != "manual" {
			return app.IdlePolicy{}, errors.New("attachment-owned Hosts do not support idle policy overrides")
		}
		return app.IdlePolicy{}, nil
	}
	if !set["idle-session-timeout"] {
		if file.IdleSessionTimeout != "" {
			var err error
			sessionTimeout, err = time.ParseDuration(file.IdleSessionTimeout)
			if err != nil {
				return app.IdlePolicy{}, fmt.Errorf("host.idle_session_timeout: %w", err)
			}
		} else if transport == "http" {
			sessionTimeout = time.Second
		}
	}
	if !set["idle-exit-timeout"] && file.IdleExitTimeout != "" {
		var err error
		exitTimeout, err = time.ParseDuration(file.IdleExitTimeout)
		if err != nil {
			return app.IdlePolicy{}, fmt.Errorf("host.idle_exit_timeout: %w", err)
		}
	} else if !set["idle-exit-timeout"] && transport == "http" && sessionTimeout != 0 {
		exitTimeout = 30 * time.Second
		if origin == "desktop" {
			exitTimeout = app.DesktopIdleExitTimeout
		}
	}
	if transport != "http" && (sessionTimeout != 0 || exitTimeout != 0 || origin == "desktop") {
		return app.IdlePolicy{}, errors.New("idle policy requires a persistent HTTP Host")
	}
	policy := app.IdlePolicy{SessionTimeout: sessionTimeout, ExitTimeout: exitTimeout, Origin: origin,
		ReleaseOnDetach: transport == "http" && !set["idle-session-timeout"] && file.IdleSessionTimeout == ""}

	return policy, policy.Validate()
}

// runHostIdlePolicy owns scheduling and process-lifetime cancellation. Application
// owns all idle decisions and fences admission before asking this layer to stop.
func runHostIdlePolicy(parent context.Context, application *app.Application, policy app.IdlePolicy, diagnostics io.Writer) (context.Context, func()) {
	ctx, cancel := context.WithCancel(parent)
	if policy.SessionTimeout == 0 {
		return ctx, cancel
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				exit, err := application.SweepIdle(ctx)
				if err != nil && ctx.Err() == nil {
					fmt.Fprintln(diagnostics, "Host idle release reported a close error; verify device release before reopening.")
				}
				if exit {
					cancel()
					return
				}
			}
		}
	}()
	return ctx, func() { cancel(); <-done }
}
