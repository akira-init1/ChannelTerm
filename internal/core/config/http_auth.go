package config

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/gofrs/flock"
)

// HostConfigDirectoryEnv selects an independent local HTTP authentication domain.
// It is an absolute directory, not a credential or a remote endpoint selector.
const HostConfigDirectoryEnv = "CHANNELTERM_HOST_CONFIG_DIR"

// hostIdentity is published last during initialization/migration. Its public ID
// selects credential storage; knowledge of the ID never authorizes a request.
type hostIdentity struct {
	Version int    `json:"version"`
	ID      string `json:"host_id"`
}

// DefaultHTTPAuthTokenPath identifies the retired default token for installer
// cleanup only. Runtime authentication never loads or creates this credential.
func DefaultHTTPAuthTokenPath() (string, error) {
	directory, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user configuration directory: %w", err)
	}
	return filepath.Join(directory, applicationDirectory, "http-auth-token"), nil
}

// DefaultHTTPHostPath returns the public Host identity manifest path. Explicit
// directories isolate independent Hosts without coupling identity to any token.
func DefaultHTTPHostPath() (string, error) {
	if directory := os.Getenv(HostConfigDirectoryEnv); directory != "" {
		if !filepath.IsAbs(directory) {
			return "", fmt.Errorf("%s must be an absolute directory", HostConfigDirectoryEnv)
		}
		return filepath.Join(directory, "http-host.json"), nil
	}
	legacy, err := DefaultHTTPAuthTokenPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(legacy), "http-host.json"), nil
}

// DefaultClientStore resolves the stable Host identity, migrating a default
// legacy registration once if necessary. Client environment tokens are ignored.
func DefaultClientStore() (ClientStore, error) {
	path, err := DefaultHTTPHostPath()
	if err != nil {
		return ClientStore{}, err
	}
	return loadHTTPHost(path, "")
}

// MigrateHTTPClients explicitly selects a legacy token file before a Host identity
// exists. It preserves registrations and grants without importing the old token
// as a client. Stop old Hosts/administrators first. Source files remain backups.
func MigrateHTTPClients(legacyTokenFile string) (ClientStore, error) {
	if legacyTokenFile == "" {
		return ClientStore{}, errors.New("migration requires a legacy token file")
	}
	path, err := DefaultHTTPHostPath()
	if err != nil {
		return ClientStore{}, err
	}
	return loadHTTPHost(path, legacyTokenFile)
}

func readBoundedAuthFile(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("authentication file exceeds size limit")
	}
	return data, nil
}

func parseHostIdentity(data []byte) (hostIdentity, error) {
	var identity hostIdentity
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&identity) != nil || decoder.Decode(new(any)) != io.EOF || identity.Version != 1 {
		return hostIdentity{}, errors.New("invalid HTTP Host identity")
	}
	id, err := hex.DecodeString(strings.TrimPrefix(identity.ID, "H-"))
	if err != nil || len(id) != 16 || identity.ID != "H-"+hex.EncodeToString(id) {
		return hostIdentity{}, errors.New("invalid HTTP Host identity")
	}
	return identity, nil
}

// loadHTTPHost serializes publishers. New files are prepared before publishing
// the manifest, so a failed migration cannot expose partial authority. Existing
// manifests never consult legacy files; corruption/missing stores fail closed.
func loadHTTPHost(path, explicitLegacy string) (ClientStore, error) {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return ClientStore{}, err
	}
	lock := flock.New(path + ".lock")
	if err := lock.Lock(); err != nil {
		return ClientStore{}, err
	}
	defer lock.Unlock()
	data, err := readBoundedAuthFile(path, 4096)
	if err == nil {
		if explicitLegacy != "" {
			return ClientStore{}, errors.New("Host already initialized; migration will not overwrite or merge registrations")
		}
		identity, err := parseHostIdentity(data)
		if err != nil {
			return ClientStore{}, err
		}
		store := ClientStore{Path: filepath.Join(directory, "http-clients-"+identity.ID+".json")}
		if _, err := os.Stat(store.Path); err != nil {
			return ClientStore{}, fmt.Errorf("Host client store unavailable: %w", err)
		}
		if _, err := store.read(); err != nil {
			return ClientStore{}, err
		}
		return store, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return ClientStore{}, err
	}
	legacyPath := explicitLegacy
	if legacyPath == "" {
		legacyPath = filepath.Join(directory, "http-auth-token")
	}
	registrations, localToken, err := legacyHTTPClients(directory, legacyPath, explicitLegacy != "")
	if err != nil {
		return ClientStore{}, err
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return ClientStore{}, err
	}
	identity := hostIdentity{Version: 1, ID: "H-" + hex.EncodeToString(random[:])}
	store := ClientStore{Path: filepath.Join(directory, "http-clients-"+identity.ID+".json")}
	data, err = json.MarshalIndent(registrations, "", "  ")
	if err != nil {
		return ClientStore{}, err
	}
	if err := writeFileAtomically(store.Path, append(data, '\n'), 0o600); err != nil {
		return ClientStore{}, err
	}
	if localToken != nil {
		if err := writeFileAtomically(store.LocalClientTokenPath(), localToken, 0o600); err != nil {
			return ClientStore{}, err
		}
	}
	data, err = json.Marshal(identity)
	if err != nil {
		return ClientStore{}, err
	}
	if err := writeFileAtomically(path, append(data, '\n'), 0o600); err != nil {
		return ClientStore{}, err
	}
	return store, nil
}

// legacyHTTPClients is the only upgrade reader of the retired shared token. It
// never creates it, trusts client overrides, or merges unrelated legacy domains.
func legacyHTTPClients(directory, tokenPath string, explicit bool) (clientFile, []byte, error) {
	empty := clientFile{Version: 1}
	data, err := readBoundedAuthFile(tokenPath, 4096)
	if err != nil && (!errors.Is(err, os.ErrNotExist) || explicit) {
		return empty, nil, err
	}
	var selected string
	if err == nil {
		token := strings.TrimSpace(string(data))
		if token == "" || strings.ContainsAny(token, "\r\n \t") {
			return empty, nil, errors.New("legacy migration token is malformed")
		}
		digest := sha256.Sum256([]byte(token))
		selected = filepath.Join(directory, "http-clients-"+hex.EncodeToString(digest[:])+".json")
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return empty, nil, err
	}
	found := false
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), "http-clients-") || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		candidate := filepath.Join(directory, entry.Name())
		name := strings.TrimSuffix(strings.TrimPrefix(entry.Name(), "http-clients-"), ".json")
		decoded, err := hex.DecodeString(name)
		if err != nil || len(decoded) != 32 {
			continue
		}
		if candidate == selected {
			found = true
		} else if !explicit {
			return empty, nil, errors.New("unselected legacy registrations exist; stop old Hosts and use clients migrate --legacy-token-file FILE in the intended Host configuration directory")
		}
	}
	if !found {
		if explicit {
			return empty, nil, errors.New("no client registrations match the selected legacy token")
		}
		return empty, nil, nil
	}
	legacy := ClientStore{Path: selected}
	lock := flock.New(selected + ".lock")
	if err := lock.Lock(); err != nil {
		return empty, nil, err
	}
	defer lock.Unlock()
	registrations, err := legacy.read()
	if err != nil {
		return empty, nil, err
	}
	local, err := readBoundedAuthFile(legacy.LocalClientTokenPath(), 4096)
	if errors.Is(err, os.ErrNotExist) {
		return registrations, nil, nil
	}
	if err != nil {
		return empty, nil, err
	}
	client, ok, err := legacy.Authenticate(strings.TrimSpace(string(local)))
	if err != nil || !ok || client.Kind != "user" {
		return empty, nil, errors.New("legacy local management credential is invalid or revoked; resolve it before migration")
	}
	return registrations, local, nil
}
