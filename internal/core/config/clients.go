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
	"unicode"
	"unicode/utf8"

	"github.com/gofrs/flock"
)

// ClientIdentity is a public credential registration, not an OS user identity
// or a Session/Attachment identifier. Names are display data, never authority.
type ClientIdentity struct {
	ID   string `json:"client_id"`
	Name string `json:"name"`
	Kind string `json:"kind"`
	// WriteAllowed is granted only by local administration. It is not inferred
	// from a caller's actor label or possession of the legacy shared token.
	WriteAllowed bool `json:"write_allowed"`
}

type clientCredential struct {
	ClientIdentity
	Digest string `json:"sha256"`
}

type clientFile struct {
	Version int                `json:"version"`
	Clients []clientCredential `json:"clients"`
}

// ClientStore persists independent HTTP credentials for one Host authentication
// domain. Only token digests are retained. The embedding local account is trusted
// to edit this file; this is not isolation from processes running as that account.
type ClientStore struct{ Path string }

// Register creates a named independent credential. The returned secret is shown
// only once; callers must deliver it privately. Registrations are bounded to 128.
func (s ClientStore) Register(name, kind string) (ClientIdentity, string, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 128 || !utf8.ValidString(name) || strings.ContainsFunc(name, unicode.IsControl) {
		return ClientIdentity{}, "", errors.New("client name must be 1..128 UTF-8 bytes without controls")
	}
	if kind != "user" && kind != "agent" {
		return ClientIdentity{}, "", errors.New("client kind must be user or agent")
	}
	var random [32]byte
	if _, err := rand.Read(random[:]); err != nil {
		return ClientIdentity{}, "", err
	}
	token := hex.EncodeToString(random[:])
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return ClientIdentity{}, "", err
	}
	identity := ClientIdentity{ID: "CL-" + hex.EncodeToString(id[:]), Name: name, Kind: kind, WriteAllowed: kind == "user"}
	digest := sha256.Sum256([]byte(token))
	err := s.update(func(file *clientFile) error {
		if len(file.Clients) >= 128 {
			return errors.New("client registration limit reached; remove unused clients")
		}
		file.Clients = append(file.Clients, clientCredential{ClientIdentity: identity, Digest: hex.EncodeToString(digest[:])})
		return nil
	})
	if err != nil {
		return ClientIdentity{}, "", err
	}
	return identity, token, nil
}

// List returns only public registrations. Reading an unused store creates no
// files. A corrupt or oversized store fails closed without exposing its content.
func (s ClientStore) List() ([]ClientIdentity, error) {
	file, err := s.read()
	if err != nil {
		return nil, err
	}
	result := make([]ClientIdentity, 0, len(file.Clients))
	for _, client := range file.Clients {
		result = append(result, client.ClientIdentity)
	}
	return result, nil
}

// Authenticate reads the current registrations on every request so removal takes
// effect without restarting a Host. Existing in-flight requests are not cancelled.
func (s ClientStore) Authenticate(token string) (ClientIdentity, bool, error) {
	file, err := s.read()
	if err != nil {
		return ClientIdentity{}, false, err
	}
	digest := sha256.Sum256([]byte(token))
	want := hex.EncodeToString(digest[:])
	for _, client := range file.Clients {
		if client.Digest == want {
			return client.ClientIdentity, true, nil
		}
	}
	return ClientIdentity{}, false, nil
}

// Remove revokes a registration for subsequent requests. Unknown IDs fail rather
// than implying that an unrelated credential was revoked.
func (s ClientStore) Remove(id string) error {
	return s.update(func(file *clientFile) error {
		for i, client := range file.Clients {
			if client.ID == id {
				file.Clients = append(file.Clients[:i], file.Clients[i+1:]...)
				return nil
			}
		}
		return errors.New("client registration not found")
	})
}

// SetWriteAllowed changes an agent's permission for subsequent requests. It does
// not cancel work already accepted; operation cancellation is a separate use case.
func (s ClientStore) SetWriteAllowed(id string, allowed bool) error {
	return s.update(func(file *clientFile) error {
		for i := range file.Clients {
			if file.Clients[i].ID == id {
				if file.Clients[i].Kind != "agent" {
					return errors.New("write grants apply only to agent credentials")
				}
				file.Clients[i].WriteAllowed = allowed
				return nil
			}
		}
		return errors.New("client registration not found")
	})
}

// LocalClientTokenPath identifies the private credential used by trusted local
// clients of this Host domain. It is distinct from the legacy shared token.
func (s ClientStore) LocalClientTokenPath() string { return s.Path + ".local-token" }

// LoadOrCreateLocalClientToken bootstraps the local account's management client.
// Cross-process locking serializes creation. An existing revoked credential is
// never silently recreated; the user must deliberately remove its private file.
// Filesystem access is the trust boundary, not the name of the calling process.
func (s ClientStore) LoadOrCreateLocalClientToken() (string, error) {
	path := s.LocalClientTokenPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	lock := flock.New(path + ".lock")
	if err := lock.Lock(); err != nil {
		return "", err
	}
	defer lock.Unlock()
	data, err := os.ReadFile(path)
	if err == nil {
		token := strings.TrimSpace(string(data))
		client, ok, err := s.Authenticate(token)
		if err != nil {
			return "", err
		}
		if !ok || client.Kind != "user" {
			return "", errors.New("local management credential is invalid or revoked; explicitly remove its private file to register again")
		}
		return token, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	client, token, err := s.Register("Local CLI/Electron", "user")
	if err != nil {
		return "", err
	}
	if err := writeFileAtomically(path, []byte(token+"\n"), 0o600); err != nil {
		return "", errors.Join(err, s.Remove(client.ID))
	}
	return token, nil
}

func (s ClientStore) read() (clientFile, error) {
	file := clientFile{Version: 1}
	f, err := os.Open(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return file, nil
	}
	if err != nil {
		return file, fmt.Errorf("open client registrations: %w", err)
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 128*1024+1))
	if err != nil {
		return file, fmt.Errorf("read client registrations: %w", err)
	}
	file = clientFile{}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if len(data) > 128*1024 || decoder.Decode(&file) != nil || file.Version != 1 || len(file.Clients) > 128 || decoder.Decode(new(any)) != io.EOF {
		return clientFile{}, errors.New("invalid client registration file")
	}
	seenIDs, seenDigests := map[string]bool{}, map[string]bool{}
	for i := range file.Clients {
		client := &file.Clients[i]
		if client.Kind == "user" {
			client.WriteAllowed = true
		}
		digest, err := hex.DecodeString(client.Digest)
		id, idErr := hex.DecodeString(strings.TrimPrefix(client.ID, "CL-"))
		if err != nil || len(digest) != 32 || idErr != nil || len(id) != 16 || !strings.HasPrefix(client.ID, "CL-") || seenIDs[client.ID] || seenDigests[client.Digest] ||
			(client.Kind != "user" && client.Kind != "agent") || client.Name == "" || len(client.Name) > 128 || !utf8.ValidString(client.Name) || strings.ContainsFunc(client.Name, unicode.IsControl) {
			return clientFile{}, errors.New("invalid client registration entry")
		}
		seenIDs[client.ID], seenDigests[client.Digest] = true, true
	}
	return file, nil
}

func (s ClientStore) update(change func(*clientFile) error) error {
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
		return err
	}
	lock := flock.New(s.Path + ".lock")
	if err := lock.Lock(); err != nil {
		return fmt.Errorf("lock client registrations: %w", err)
	}
	defer lock.Unlock()
	file, err := s.read()
	if err != nil {
		return err
	}
	if err := change(&file); err != nil {
		return err
	}
	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomically(s.Path, append(data, '\n'), 0o600)
}
