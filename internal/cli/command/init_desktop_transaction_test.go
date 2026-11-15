package command

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/akira-init1/ChannelTerm/internal/core/config"
	initmcp "github.com/akira-init1/ChannelTerm/internal/init/mcp"
)

func TestDesktopRepairProbeUsesRealReadOnlyMCPAndClassifiesAuthentication(t *testing.T) {
	c, target, _ := repairTestCoordinator(t)
	host := newAttachTestHost(t)
	defer host.close()
	server := httptest.NewServer(requireClientToken(c.store, host.server.Config.Handler))
	defer server.Close()
	c.probe = probeDesktopRepair
	plan := repairTestPlan(t, c, target, server.URL)
	result, err := c.apply(context.Background(), target, server.URL, plan.Fingerprint, false)
	if err != nil || result.State != "applied" {
		t.Fatalf("real read-only probe: %+v %v", result, err)
	}
	if code := desktopProbeCode(probeDesktopRepair(context.Background(), server.URL, "wrong-test-credential")); code != "authentication_failed" {
		t.Fatalf("401: %s", code)
	}
	for _, status := range []int{403, 503} {
		failure := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(status) }))
		code := desktopProbeCode(probeDesktopRepair(context.Background(), failure.URL, "test-only"))
		failure.Close()
		want := map[int]string{403: "permission_denied", 503: "authentication_unavailable"}[status]
		if code != want {
			t.Fatalf("%d: %s", status, code)
		}
	}
}

func repairTestCoordinator(t *testing.T) (*desktopRepairCoordinator, initmcp.RepairTarget, string) {
	t.Helper()
	root := t.TempDir()
	c := &desktopRepairCoordinator{locations: initmcp.RepairLocations{Options: initmcp.Options{Home: root, ConfigDir: root, WorkDir: root}}, store: config.ClientStore{Path: filepath.Join(root, "clients.json")}, journal: filepath.Join(root, "repair.json"), probe: func(context.Context, string, string) error { return nil }}
	return c, initmcp.RepairTarget{Client: "claude", Scope: "user"}, "http://127.0.0.1:37099/mcp"
}

func repairTestPlan(t *testing.T, c *desktopRepairCoordinator, target initmcp.RepairTarget, endpoint string) *initmcp.RepairPlan {
	t.Helper()
	p, err := initmcp.PreviewRepair(c.locations, target, initmcp.Endpoint{Transport: initmcp.TransportStreamableHTTP, URL: endpoint, Headers: map[string]string{"Authorization": "Bearer placeholder"}})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestDesktopRepairCreatesReadOnlyCredentialAndPreservesExistingByDefault(t *testing.T) {
	c, target, endpoint := repairTestCoordinator(t)
	old, oldToken, err := c.store.Register("existing-agent", "agent")
	if err != nil {
		t.Fatal(err)
	}
	plan := repairTestPlan(t, c, target, endpoint)
	c.probe = func(_ context.Context, _ string, token string) error {
		identity, ok, err := c.store.Authenticate(token)
		if err != nil || !ok || identity.Kind != "agent" || identity.WriteAllowed {
			t.Fatal("replacement is not an independent read-only agent")
		}
		return nil
	}
	result, err := c.apply(context.Background(), target, endpoint, plan.Fingerprint, false)
	if err != nil || result.State != "applied" || result.ClientID == old.ID {
		t.Fatalf("apply: %+v, %v", result, err)
	}
	current := repairTestPlan(t, c, target, endpoint)
	if _, ok, err := c.store.Authenticate(current.ExistingCredential()); err != nil || !ok {
		t.Fatal("persisted credential cannot authenticate")
	}
	if _, ok, err := c.store.Authenticate(oldToken); err != nil || !ok {
		t.Fatal("old credential was revoked")
	}
	before, _ := os.ReadFile(current.Path)
	result, err = c.apply(context.Background(), target, endpoint, current.Fingerprint, false)
	if err != nil || result.State != "preserved" {
		t.Fatalf("repeat: %+v %v", result, err)
	}
	after, _ := os.ReadFile(current.Path)
	if string(after) != string(before) {
		t.Fatal("existing configuration changed")
	}
	clients, _ := c.store.List()
	if len(clients) != 2 {
		t.Fatal("preserve registered another credential")
	}
	result, err = c.apply(context.Background(), target, endpoint, current.Fingerprint, true)
	if err != nil || result.State != "applied" {
		t.Fatalf("explicit repair: %+v %v", result, err)
	}
	clients, _ = c.store.List()
	if len(clients) != 3 {
		t.Fatal("successful replacement revoked an old registration")
	}
}

func TestDesktopRepairFailuresRetainOriginalAndRemoveOnlyNewCredential(t *testing.T) {
	for _, phase := range []string{"probe", "write", "cancel", "concurrent-edit"} {
		t.Run(phase, func(t *testing.T) {
			c, target, endpoint := repairTestCoordinator(t)
			old, _, err := c.store.Register("old-agent", "agent")
			if err != nil {
				t.Fatal(err)
			}
			plan := repairTestPlan(t, c, target, endpoint)
			if err := plan.Apply(plan.Fingerprint); err != nil {
				t.Fatal(err)
			}
			plan = repairTestPlan(t, c, target, endpoint)
			original, _ := os.ReadFile(plan.Path)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch phase {
			case "concurrent-edit":
				original = []byte(`{"changed-by-user":true}`)
				c.probe = func(context.Context, string, string) error { return os.WriteFile(plan.Path, original, 0600) }
			case "probe":
				c.probe = func(context.Context, string, string) error { return errors.New("authentication rejected") }
			case "write":
				c.publish = func(*initmcp.RepairPlan, string) error { return os.ErrPermission }
			case "cancel":
				c.probe = func(context.Context, string, string) error { cancel(); return ctx.Err() }
			}
			result, err := c.apply(ctx, target, endpoint, plan.Fingerprint, true)
			if err == nil || result.State != "rolled_back" {
				t.Fatalf("failure: %+v %v", result, err)
			}
			actual, _ := os.ReadFile(plan.Path)
			if string(actual) != string(original) {
				t.Fatal("failed repair changed original")
			}
			clients, _ := c.store.List()
			if len(clients) != 1 || clients[0].ID != old.ID {
				t.Fatalf("credential rollback: %+v", clients)
			}
			if _, err := os.Stat(c.journal); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("journal not cleaned")
			}
		})
	}
}

func TestDesktopRepairInterruptionRecoversBeforeAndAfterPublication(t *testing.T) {
	for _, phase := range []string{"registered-before-id-journal", "before-publish", "after-publish", "external-edit", "external-grant"} {
		t.Run(phase, func(t *testing.T) {
			c, target, endpoint := repairTestCoordinator(t)
			plan := repairTestPlan(t, c, target, endpoint)
			journal := desktopRepairJournal{Version: 1, Path: plan.Path, Before: plan.Fingerprint, Name: "ChannelTerm crash-test"}
			if err := writeDesktopRepairJournal(c.journal, journal); err != nil {
				t.Fatal(err)
			}
			identity, token, err := c.store.Register(journal.Name, "agent")
			if err != nil {
				t.Fatal(err)
			}
			if phase != "registered-before-id-journal" {
				plan, err = initmcp.PreviewRepair(c.locations, target, initmcp.Endpoint{Transport: initmcp.TransportStreamableHTTP, URL: endpoint, Headers: map[string]string{"Authorization": "Bearer " + token}})
				if err != nil {
					t.Fatal(err)
				}
				journal.After, err = plan.ReplacementFingerprint()
				if err != nil {
					t.Fatal(err)
				}
				journal.ID = identity.ID
				if err := writeDesktopRepairJournal(c.journal, journal); err != nil {
					t.Fatal(err)
				}
			}
			if phase == "after-publish" {
				if err := plan.Apply(plan.Fingerprint); err != nil {
					t.Fatal(err)
				}
			}
			if phase == "external-edit" {
				if err := os.WriteFile(plan.Path, []byte(`{"user-change":true}`), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if phase == "external-grant" {
				if err := c.store.SetWriteAllowed(identity.ID, true); err != nil {
					t.Fatal(err)
				}
			}
			result, err := c.recover(context.Background())
			clients, _ := c.store.List()
			switch phase {
			case "after-publish":
				if err != nil || result.State != "recovered_applied" || len(clients) != 1 {
					t.Fatalf("lost applied credential: %+v %v", result, err)
				}
			case "external-edit", "external-grant":
				if err == nil || result.State != "recovery_required" || len(clients) != 1 {
					t.Fatalf("unsafe recovery: %+v %v", result, err)
				}
			default:
				if err != nil || result.State != "rolled_back" || len(clients) != 0 {
					t.Fatalf("orphan credential: %+v %v", result, err)
				}
			}
		})
	}
}

func TestDesktopRepairNoMutationOnStaleOrUnopenedRecovery(t *testing.T) {
	c, target, endpoint := repairTestCoordinator(t)
	result, err := c.recover(context.Background())
	if err != nil || result.State != "none" {
		t.Fatal(err)
	}
	files, _ := os.ReadDir(filepath.Dir(c.journal))
	if len(files) != 0 {
		t.Fatal("unused recovery mutated profile")
	}
	plan := repairTestPlan(t, c, target, endpoint)
	if err := os.WriteFile(plan.Path, []byte(`{"external":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := c.apply(context.Background(), target, endpoint, plan.Fingerprint, true); err == nil {
		t.Fatal("stale preview applied")
	}
	clients, _ := c.store.List()
	if len(clients) != 0 {
		t.Fatal("stale preview registered credential")
	}
}
