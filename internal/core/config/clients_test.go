package config

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestClientCredentialsPersistWithoutSecretsAndRevoke(t *testing.T) {
	store := ClientStore{Path: filepath.Join(t.TempDir(), "clients.json")}
	if clients, err := store.List(); err != nil || len(clients) != 0 {
		t.Fatalf("empty list: %v %v", clients, err)
	}
	if _, err := os.Stat(store.Path); !os.IsNotExist(err) {
		t.Fatal("list created a file")
	}
	first, token, err := store.Register("AI board debugger", "agent")
	if err != nil {
		t.Fatal(err)
	}
	second, other, err := store.Register("Local operator", "user")
	if err != nil {
		t.Fatal(err)
	}
	if token == other || first.ID == second.ID {
		t.Fatal("registrations share identity or credential")
	}
	data, err := os.ReadFile(store.Path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), token) || strings.Contains(string(data), other) {
		t.Fatal("plaintext credential persisted")
	}
	reopened := ClientStore{Path: store.Path}
	if got, ok, err := reopened.Authenticate(token); err != nil || !ok || got != first {
		t.Fatalf("authenticate: %v %v %v", got, ok, err)
	}
	if _, ok, err := reopened.Authenticate("not-a-credential"); err != nil || ok {
		t.Fatal("wrong credential accepted")
	}
	if err := reopened.Remove(first.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := store.Authenticate(token); err != nil || ok {
		t.Fatal("revoked credential accepted")
	}
	if got, ok, err := store.Authenticate(other); err != nil || !ok || got != second {
		t.Fatal("unrelated credential changed")
	}
	if err := store.Remove(first.ID); err == nil {
		t.Fatal("unknown removal succeeded")
	}
}

func TestClientRegistrationRejectsMalformedAndBoundsState(t *testing.T) {
	store := ClientStore{Path: filepath.Join(t.TempDir(), "clients.json")}
	for _, test := range []struct{ name, kind string }{{"", "agent"}, {"a\nb", "agent"}, {strings.Repeat("a", 129), "agent"}, {"name", "admin"}} {
		if _, _, err := store.Register(test.name, test.kind); err == nil {
			t.Fatalf("accepted %+v", test)
		}
	}
	for _, raw := range []string{`{}`, `{"version":2}`, `{"version":1,"clients":[]} {}`, `{"version":1,"unknown":true}`, strings.Repeat("x", 128*1024+1)} {
		if err := os.WriteFile(store.Path, []byte(raw), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, _, err := store.Authenticate("secret"); err == nil {
			t.Fatal("corrupt store accepted")
		}
		if _, _, err := store.Register("test", "agent"); err == nil {
			t.Fatal("corrupt store overwritten")
		}
	}
}

func TestConcurrentClientRegistrationDoesNotLoseUpdates(t *testing.T) {
	store := ClientStore{Path: filepath.Join(t.TempDir(), "clients.json")}
	var group sync.WaitGroup
	for range 12 {
		group.Go(func() {
			if _, _, err := store.Register("client", "agent"); err != nil {
				t.Error(err)
			}
		})
	}
	group.Wait()
	clients, err := store.List()
	if err != nil || len(clients) != 12 {
		t.Fatalf("list count %d: %v", len(clients), err)
	}
}
