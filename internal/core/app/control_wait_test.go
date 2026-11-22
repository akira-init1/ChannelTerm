package app

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"testing/synctest"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/core/config"
	"github.com/akira-init1/ChannelTerm/internal/core/session"
)

// Exercise both sides of the handback race using the runtime's virtual clock.
func TestInterruptedTaskWaitDeadlineAndEarlyReturn(t *testing.T) {
	for _, mode := range []string{"early", "timeout", "disconnect", "exit"} {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				a, _, _ := instanceFixture(t)
				user := controlTestClient(t, a, "CL-operator", "user")
				agent := controlTestClient(t, a, "CL-agent", "agent")
				idle, _ := a.SessionControl(user, "board")
				active, e := a.BeginControl(agent, "board", controlProof(idle))
				if e != nil {
					t.Fatal(e)
				}
				start := time.Now()
				_, e = a.TakeControl(user, "board", controlProof(active))
				if e != nil {
					t.Fatal(e)
				}
				synctest.Wait()
				human, _ := a.SessionControl(user, "board")
				if mode == "early" {
					if _, e := a.AllowAI(user, "board", controlProof(human)); e != nil {
						t.Fatal(e)
					}
				}
				ctx, cancel := context.WithCancel(agent)
				defer cancel()
				type result struct {
					notice WaitNotice
					err    error
				}
				done := make(chan result, 1)
				go func() { n, err := a.WaitForHuman(ctx, "board", controlProof(active)); done <- result{n, err} }()
				synctest.Wait()
				switch mode {
				case "early":
					r := <-done
					if r.err != nil || r.notice.Status != "allowed" || r.notice.RetryAllowed {
						t.Fatal(r)
					}
					if _, err := a.WaitForHuman(ctx, "board", controlProof(active)); !errors.Is(err, ErrControlStale) {
						t.Fatal(err)
					}
				case "timeout":
					time.Sleep(180 * time.Second)
					r := <-done
					if r.err != nil || r.notice.Deadline.Sub(start) != 180*time.Second || r.notice.Status != "timed_out" || !r.notice.WriteForbidden {
						t.Fatal(r)
					}
				case "disconnect":
					cancel()
					if r := <-done; !errors.Is(r.err, context.Canceled) {
						t.Fatal(r)
					}
				case "exit":
					proof := agent.Value(instanceProofKey{}).(InstanceProof)
					if err := a.EndClientInstance(agent, proof); err != nil {
						t.Fatal(err)
					}
					time.Sleep(time.Second)
					if r := <-done; !errors.Is(r.err, ErrClientInstance) {
						t.Fatal(r)
					}
				}
				status, _ := a.SessionControl(user, "board")
				if status.Waiting || (mode != "early" && !status.AIPaused) {
					t.Fatal(status)
				}
			})
		})
	}
}

func TestSavedPolicyPrecedesSessionPublicationAndSurvivesRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	manager := session.NewManager()
	defer manager.Close()
	a, err := New(Dependencies{Manager: manager, ConfigPath: path})
	if err != nil {
		t.Fatal(err)
	}
	user := controlTestClient(t, a, "CL-operator", "user")
	if err := a.SetSavedConnectionPolicy(user, "ssh", "dev@build:22", true); err != nil {
		t.Fatal(err)
	}
	file, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	restarted, err := New(Dependencies{Manager: manager, ConfigPath: path, ManualOnly: file.ManualOnly})
	if err != nil {
		t.Fatal(err)
	}
	agent := controlTestClient(t, restarted, "CL-agent", "agent")
	for _, id := range []string{"first", "second"} {
		terminal := newFakeConnectedSession(id)
		if err := terminal.Connect(context.Background()); err != nil {
			t.Fatal(err)
		}
		_, created, err := manager.CreateIndependent(context.Background(), session.SessionMetadata{Transport: "ssh", Endpoint: "dev@build:22", Label: id}, func() (session.Session, error) { return terminal, nil })
		if !created || err != nil {
			t.Fatal(err)
		}
		if _, _, err := restarted.GuardSessionCall(agent, id); !errors.Is(err, ErrManualOnly) {
			t.Fatal(id, err)
		}
	}
}
