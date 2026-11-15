package command

import (
	"context"
	"flag"
	"io"
	"testing"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/core/app"
	"github.com/akira-init1/ChannelTerm/internal/core/config"
	"github.com/akira-init1/ChannelTerm/internal/core/session"
)

func TestHostIdleDefaultsOverridesAndTemporaryOwnership(t *testing.T) {
	for _, tc := range []struct {
		name, origin, transport string
		file                    config.Host
		args                    []string
		temporary               bool
		session, exit           time.Duration
		invalid                 bool
	}{
		{name: "manual", origin: "manual", transport: "http"},
		{name: "desktop", origin: "desktop", transport: "http", session: 30 * time.Second},
		{name: "configured keep", origin: "desktop", transport: "http", file: config.Host{IdleSessionTimeout: "0s"}},
		{name: "explicit exit", origin: "manual", transport: "http", file: config.Host{IdleSessionTimeout: "1m", IdleExitTimeout: "2m"}, session: time.Minute, exit: 2 * time.Minute},
		{name: "flag override", origin: "desktop", transport: "http", file: config.Host{IdleSessionTimeout: "2m"}, args: []string{"--idle-session-timeout=5s"}, session: 5 * time.Second},
		{name: "too short", origin: "manual", transport: "http", args: []string{"--idle-session-timeout=1ms"}, invalid: true},
		{name: "exit without release", origin: "manual", transport: "http", args: []string{"--idle-exit-timeout=1m"}, invalid: true},
		{name: "bad config", origin: "manual", transport: "http", file: config.Host{IdleSessionTimeout: "invalid"}, invalid: true},
		{name: "stdio", origin: "desktop", transport: "stdio", invalid: true},
		{name: "temporary preserved", origin: "manual", transport: "http", temporary: true, file: config.Host{IdleSessionTimeout: "30s", IdleExitTimeout: "30s"}},
		{name: "temporary override rejected", origin: "manual", transport: "http", temporary: true, args: []string{"--idle-session-timeout=5s"}, invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := flag.NewFlagSet("idle", flag.ContinueOnError)
			s := f.Duration("idle-session-timeout", 0, "")
			e := f.Duration("idle-exit-timeout", 0, "")
			if err := f.Parse(tc.args); err != nil {
				t.Fatal(err)
			}
			p, err := resolveHostIdlePolicy(f, tc.file, *s, *e, tc.origin, tc.transport, tc.temporary)
			if tc.invalid {
				if err == nil {
					t.Fatal("invalid policy accepted")
				}
				return
			}
			if err != nil || p.SessionTimeout != tc.session || p.ExitTimeout != tc.exit {
				t.Fatalf("%+v %v", p, err)
			}
		})
	}
}

func TestHostIdleLoopCancelsOnlyAfterApplicationExitDecision(t *testing.T) {
	m := session.NewManager()
	defer m.Close()
	p := app.IdlePolicy{SessionTimeout: time.Second, ExitTimeout: time.Second}
	a, err := app.New(app.Dependencies{Manager: m, Idle: p})
	if err != nil {
		t.Fatal(err)
	}
	ctx, stop := runHostIdlePolicy(context.Background(), a, p, io.Discard)
	defer stop()
	select {
	case <-ctx.Done():
		t.Fatal("Host exited before startup grace")
	default:
	}
	select {
	case <-ctx.Done():
	case <-time.After(4 * time.Second):
		t.Fatal("idle Host did not stop")
	}
	if _, _, err := a.RegisterClientInstance(context.Background(), app.InstanceRegistration{Name: "late", Entry: "test"}); err != app.ErrHostDraining {
		t.Fatal(err)
	}
}
