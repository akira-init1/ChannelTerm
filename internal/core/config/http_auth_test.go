package config

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestHTTPHostIdentityConcurrentCreationAndTokenIndependence(t *testing.T) {
	directory := t.TempDir()
	t.Setenv(HostConfigDirectoryEnv, directory)
	t.Setenv("CHANNELTERM_HTTP_AUTH_TOKEN", "not-a-host-identity")
	const callers = 12
	type result struct {
		store ClientStore
		err   error
	}
	results := make(chan result, callers)
	for range callers {
		go func() { s, err := DefaultClientStore(); results <- result{s, err} }()
	}
	var path string
	for range callers {
		got := <-results
		if got.err != nil {
			t.Fatal(got.err)
		}
		if path == "" {
			path = got.store.Path
		}
		if got.store.Path != path {
			t.Fatal("concurrent Host identity diverged")
		}
	}
	if _, err := os.Stat(filepath.Join(directory, "http-auth-token")); !os.IsNotExist(err) {
		t.Fatal("created retired shared token")
	}
	t.Setenv("CHANNELTERM_HTTP_AUTH_TOKEN", "another-client-token")
	store, err := DefaultClientStore()
	if err != nil || store.Path != path {
		t.Fatal("client token changed Host identity")
	}
	token, err := store.LoadOrCreateLocalClientToken()
	if err != nil {
		t.Fatal(err)
	}
	again, err := store.LoadOrCreateLocalClientToken()
	if err != nil || again != token {
		t.Fatal("management credential not stable")
	}
	if runtime.GOOS != "windows" {
		for _, file := range []string{path, store.LocalClientTokenPath(), filepath.Join(directory, "http-host.json")} {
			info, err := os.Stat(file)
			if err != nil || info.Mode().Perm() != 0o600 {
				t.Fatal("private authentication file permissions")
			}
		}
	}
	t.Setenv(HostConfigDirectoryEnv, t.TempDir())
	other, err := DefaultClientStore()
	if err != nil || other.Path == path {
		t.Fatal("independent Host directories not isolated")
	}
	if _, ok, err := other.Authenticate(token); err != nil || ok {
		t.Fatal("another Host accepted credential")
	}
}

func legacyStoreFixture(t *testing.T, directory, token string) ClientStore {
	t.Helper()
	digest := sha256.Sum256([]byte(token))
	return ClientStore{Path: filepath.Join(directory, "http-clients-"+hex.EncodeToString(digest[:])+".json")}
}

func TestHTTPHostMigrationPreservesRegistrationsAndSurvivesLegacyDeletion(t *testing.T) {
	// Literal directory names must never be interpreted as glob expressions.
	directory := filepath.Join(t.TempDir(), "host[identity]")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv(HostConfigDirectoryEnv, directory)
	legacy := legacyStoreFixture(t, directory, "retired-token")
	tokenFile := filepath.Join(directory, "http-auth-token")
	if err := os.WriteFile(tokenFile, []byte("retired-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	agent, token, err := legacy.Register("board AI", "agent")
	if err != nil {
		t.Fatal(err)
	}
	if err := legacy.SetWriteAllowed(agent.ID, true); err != nil {
		t.Fatal(err)
	}
	revoked, revokedToken, err := legacy.Register("revoked", "agent")
	if err != nil {
		t.Fatal(err)
	}
	if err := legacy.Remove(revoked.ID); err != nil {
		t.Fatal(err)
	}
	management, err := legacy.LoadOrCreateLocalClientToken()
	if err != nil {
		t.Fatal(err)
	}
	store, err := DefaultClientStore()
	if err != nil {
		t.Fatal(err)
	}
	identity, ok, err := store.Authenticate(token)
	if err != nil || !ok || identity.ID != agent.ID || identity.Name != agent.Name || !identity.WriteAllowed {
		t.Fatal("migration changed identity or grant")
	}
	for _, rejected := range []string{"retired-token", revokedToken} {
		if _, ok, err := store.Authenticate(rejected); err != nil || ok {
			t.Fatal("retired/revoked token accepted")
		}
	}
	local, err := store.LoadOrCreateLocalClientToken()
	if err != nil || local != management {
		t.Fatal("management token changed")
	}
	if _, err := MigrateHTTPClients(tokenFile); err == nil {
		t.Fatal("migration overwrote existing Host")
	}
	for _, path := range []string{tokenFile, legacy.Path, legacy.LocalClientTokenPath()} {
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
	}
	reopened, err := DefaultClientStore()
	if err != nil || reopened.Path != store.Path {
		t.Fatal("legacy deletion changed Host")
	}
	if _, ok, err := reopened.Authenticate(token); err != nil || !ok {
		t.Fatal("lost migrated credential")
	}
	if err := reopened.SetWriteAllowed(agent.ID, false); err != nil {
		t.Fatal(err)
	}
	identity, ok, err = reopened.Authenticate(token)
	if err != nil || !ok || identity.WriteAllowed {
		t.Fatal("revocation failed after migration")
	}
}

func TestHTTPHostMigrationFailsClosedAndCanRetry(t *testing.T) {
	for _, scenario := range []string{"corrupt", "oversized", "revoked-local", "ambiguous", "missing-token"} {
		t.Run(scenario, func(t *testing.T) {
			directory := t.TempDir()
			t.Setenv(HostConfigDirectoryEnv, directory)
			legacy := legacyStoreFixture(t, directory, "legacy")
			tokenFile := filepath.Join(directory, "http-auth-token")
			if err := os.WriteFile(tokenFile, []byte("legacy"), 0o600); err != nil {
				t.Fatal(err)
			}
			client, token, err := legacy.Register("preserved", "agent")
			if err != nil {
				t.Fatal(err)
			}
			original, err := os.ReadFile(legacy.Path)
			if err != nil {
				t.Fatal(err)
			}
			switch scenario {
			case "corrupt":
				err = os.WriteFile(legacy.Path, []byte("broken"), 0o600)
			case "oversized":
				err = os.WriteFile(legacy.Path, bytes.Repeat([]byte("x"), 128*1024+1), 0o600)
			case "revoked-local":
				err = os.WriteFile(legacy.LocalClientTokenPath(), []byte("revoked"), 0o600)
			case "ambiguous":
				_, _, err = legacyStoreFixture(t, directory, "another-domain").Register("other", "agent")
			case "missing-token":
				err = os.Remove(tokenFile)
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err := DefaultClientStore(); err == nil {
				t.Fatal("unsafe migration succeeded")
			}
			if _, err := os.Stat(filepath.Join(directory, "http-host.json")); !os.IsNotExist(err) {
				t.Fatal("published partial migration")
			}
			if err := os.WriteFile(legacy.Path, original, 0o600); err != nil {
				t.Fatal(err)
			}
			if scenario == "revoked-local" {
				if err := os.Remove(legacy.LocalClientTokenPath()); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(tokenFile, []byte("legacy"), 0o600); err != nil {
				t.Fatal(err)
			}
			migrated, err := MigrateHTTPClients(tokenFile)
			if err != nil {
				t.Fatal(err)
			}
			got, ok, err := migrated.Authenticate(token)
			if err != nil || !ok || got.ID != client.ID {
				t.Fatal("retry lost original registration")
			}
		})
	}
}

func TestHTTPHostIdentityRejectsCorruptionWithoutLegacyFallback(t *testing.T) {
	for _, raw := range []string{`{}`, `{"version":1,"host_id":"../escape"}`, `{"version":2,"host_id":"H-0123456789abcdef0123456789abcdef"}`, strings.Repeat("x", 4097)} {
		directory := t.TempDir()
		t.Setenv(HostConfigDirectoryEnv, directory)
		if err := os.WriteFile(filepath.Join(directory, "http-host.json"), []byte(raw), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := DefaultClientStore(); err == nil {
			t.Fatal("corrupt identity replaced")
		}
	}
	directory := t.TempDir()
	t.Setenv(HostConfigDirectoryEnv, directory)
	store, err := DefaultClientStore()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(store.Path); err != nil {
		t.Fatal(err)
	}
	if _, err := DefaultClientStore(); err == nil {
		t.Fatal("missing registry silently recreated")
	}
	t.Setenv(HostConfigDirectoryEnv, "relative")
	if _, err := DefaultClientStore(); err == nil {
		t.Fatal("relative Host directory accepted")
	}
}
