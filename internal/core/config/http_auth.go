package config

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
// New files use owner-only permissions where the operating system honors them.
func LoadOrCreateHTTPAuthToken(path string) (string, error) {
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
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", fmt.Errorf("create HTTP authentication directory for %q: %w", path, err)
	}
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("generate HTTP authentication token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(random)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return "", fmt.Errorf("read concurrent HTTP authentication token %q: %w", path, readErr)
		}
		return validateHTTPAuthToken(path, data)
	}
	if err != nil {
		return "", fmt.Errorf("create HTTP authentication token %q: %w", path, err)
	}
	if _, err := file.WriteString(token + "\n"); err != nil {
		_ = file.Close()
		return "", fmt.Errorf("write HTTP authentication token %q: %w", path, err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return "", fmt.Errorf("sync HTTP authentication token %q: %w", path, err)
	}
	if err := file.Close(); err != nil {
		return "", fmt.Errorf("close HTTP authentication token %q: %w", path, err)
	}
	return token, nil
}

func validateHTTPAuthToken(path string, data []byte) (string, error) {
	token := strings.TrimSpace(string(data))
	if token == "" || strings.ContainsAny(token, "\r\n \t") {
		return "", fmt.Errorf("HTTP authentication token %q is empty or malformed", path)
	}
	return token, nil
}
