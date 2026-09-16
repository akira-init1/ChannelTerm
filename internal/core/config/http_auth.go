package config

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gofrs/flock"
)

const httpAuthTokenFile = "http-auth-token"

// DefaultHTTPAuthTokenPath returns the per-user credential path used by the
// local Streamable HTTP Host and ChannelTerm's built-in HTTP clients.
func DefaultHTTPAuthTokenPath() (string, error) {
	directory, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user configuration directory: %w", err)
	}
	return filepath.Join(directory, applicationDirectory, httpAuthTokenFile), nil
}

// LoadOrCreateHTTPAuthToken returns a stable random bearer token from path.
// Creation is serialized across processes and installs only a complete 32-byte
// Base64URL credential. New files use owner-only permissions where the
// operating system honors them.
func LoadOrCreateHTTPAuthToken(path string) (string, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", fmt.Errorf("create HTTP authentication directory for %q: %w", path, err)
	}
	lock := flock.New(path + ".lock")
	if err := lock.Lock(); err != nil {
		return "", fmt.Errorf("lock HTTP authentication token %q: %w", path, err)
	}
	defer func() { _ = lock.Unlock() }()

	data, err := os.ReadFile(path)
	if err == nil {
		if err := os.Chmod(path, 0o600); err != nil {
			return "", fmt.Errorf("secure HTTP authentication token %q: %w", path, err)
		}
		return validateHTTPAuthToken(path, data)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("read HTTP authentication token %q: %w", path, err)
	}
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("generate HTTP authentication token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(random)
	if err := writeFileAtomically(path, []byte(token+"\n"), 0o600); err != nil {
		return "", fmt.Errorf("write HTTP authentication token %q: %w", path, err)
	}
	return token, nil
}

func validateHTTPAuthToken(path string, data []byte) (string, error) {
	raw := string(data)
	token := strings.TrimSuffix(raw, "\r\n")
	if token == raw {
		token = strings.TrimSuffix(raw, "\n")
	}
	if token == "" || strings.ContainsAny(token, "\r\n \t") {
		return "", fmt.Errorf("HTTP authentication token %q is empty or malformed", path)
	}
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(decoded) != 32 {
		return "", fmt.Errorf("HTTP authentication token %q is not a 32-byte Base64URL credential", path)
	}
	return token, nil
}
