// Package guiapi adapts narrowly scoped desktop HTTP requests to Application.
// It owns no Sessions or SSH implementation. The embedding Host must mount it
// behind the same authentication middleware as its existing MCP endpoint.
package guiapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/core/app"
	sshtransport "github.com/akira-init1/ChannelTerm/internal/transport/ssh"
)

// SSHPath is separate from MCP; no MCP tool or parameter is added by this API.
const SSHPath = "/gui/v1/ssh"

type sshOpener interface {
	OpenSSH(context.Context, app.OpenSSHRequest) (app.OpenSSHResult, error)
}

type sshRequest struct {
	Host           string   `json:"host"`
	User           string   `json:"user"`
	Port           int      `json:"port"`
	Label          string   `json:"label"`
	IdentityFiles  []string `json:"identity_files"`
	KnownHostsFile string   `json:"known_hosts_file"`
	Password       *string  `json:"password"`
	KeyPassphrase  *string  `json:"key_passphrase"`
}

// NewSSHHandler exposes capability discovery and explicit SSH creation. Callers
// must authenticate before reaching this handler. Browser origins are rejected;
// secrets and underlying errors never enter responses, logs or Session metadata.
// Unknown, changed and revoked host keys fail closed against the Host trust file.
func NewSSHHandler(application *app.Application) http.Handler {
	return newSSHHandler(application)
}

func newSSHHandler(application sshOpener) http.Handler {
	active := make(chan struct{}, 2)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != SSHPath || r.URL.RawQuery != "" {
			reply(w, 404, map[string]string{"error": "not_found"})
			return
		}
		if r.Header.Get("Origin") != "" {
			reply(w, 403, map[string]string{"error": "origin_forbidden"})
			return
		}
		if r.Method == http.MethodGet {
			reply(w, 200, map[string]bool{"available": true})
			return
		}
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "GET, POST")
			reply(w, 405, map[string]string{"error": "method_not_allowed"})
			return
		}
		media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || media != "application/json" {
			reply(w, 415, map[string]string{"error": "invalid_content_type"})
			return
		}
		select {
		case active <- struct{}{}:
			defer func() { <-active }()
		default:
			reply(w, 429, map[string]string{"error": "busy"})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		// Bound a stalled body as well as setup. This affects only this request,
		// not subsequent MCP requests on the same HTTP/1 connection.
		controller := http.NewResponseController(w)
		_ = controller.SetReadDeadline(time.Now().Add(5 * time.Second))
		defer controller.SetReadDeadline(time.Time{})
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 32768))
		defer clear(body)
		var input sshRequest
		decoder := json.NewDecoder(bytes.NewReader(body))
		decoder.DisallowUnknownFields()
		if err != nil || decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF || !validRequest(input) {
			reply(w, 400, map[string]string{"error": "invalid_input"})
			return
		}
		config, cleanup, err := configuration(&input)
		if err != nil {
			reply(w, 400, map[string]string{"error": "configuration"})
			return
		}
		defer cleanup()
		opened, err := application.OpenSSH(ctx, app.OpenSSHRequest{Config: config, Label: input.Label})
		if err != nil {
			code := "connection_failed"
			var stage *sshtransport.StageError
			if errors.As(err, &stage) {
				switch stage.Stage {
				case "host-key":
					code = "host_key_rejected"
				case "identity", "authentication":
					code = "authentication_failed"
				}
			}
			if ctx.Err() != nil {
				code = "cancelled"
			}
			reply(w, 422, map[string]string{"error": code})
			return
		}
		// Request completion/GUI detach never closes this Manager-owned Session.
		reply(w, 200, map[string]any{"session_id": opened.Info.ID, "reused": opened.Reused})
	})
}

func reply(w http.ResponseWriter, status int, value any) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func cleanText(value string, max int) bool {
	return len(value) <= max && !strings.ContainsFunc(value, func(r rune) bool { return r < 32 || r == 127 })
}

func validRequest(r sshRequest) bool {
	if r.Host == "" || r.User == "" || !cleanText(r.Host, 253) || !cleanText(r.User, 128) || !cleanText(r.Label, 128) || r.Port < 1 || r.Port > 65535 || len(r.IdentityFiles) > 8 || !cleanText(r.KnownHostsFile, 4096) {
		return false
	}
	for _, path := range r.IdentityFiles {
		if path == "" || !cleanText(path, 4096) {
			return false
		}
	}
	return (r.Password == nil || len(*r.Password) <= 4096) && (r.KeyPassphrase == nil || len(*r.KeyPassphrase) <= 4096)
}

// configuration supplies existing Transport callbacks, never a parallel SSH
// stack. Strict known_hosts validation has no confirmation/skip callback. Paths
// are interpreted on the authenticated Host, and no configuration is persisted.
func configuration(r *sshRequest) (sshtransport.Config, func(), error) {
	c := sshtransport.Config{Host: r.Host, User: r.User, Port: r.Port, PrivateKeyPaths: r.IdentityFiles, KnownHostsPath: r.KnownHostsFile, Timeout: 10 * time.Second}
	if c.KnownHostsPath == "" || len(c.PrivateKeyPaths) == 0 && r.Password == nil {
		home, err := os.UserHomeDir()
		if err != nil {
			return c, nil, err
		}
		if c.KnownHostsPath == "" {
			c.KnownHostsPath = filepath.Join(home, ".ssh", "known_hosts")
		}
		if len(c.PrivateKeyPaths) == 0 && r.Password == nil {
			for _, name := range []string{"id_ed25519", "id_rsa"} {
				path := filepath.Join(home, ".ssh", name)
				if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
					c.PrivateKeyPaths = append(c.PrivateKeyPaths, path)
				}
			}
		}
	}
	var password, passphrase []byte
	if r.Password != nil {
		password = []byte(*r.Password)
		*r.Password = ""
		r.Password = nil
		used := false
		c.Password = func(ctx context.Context) ([]byte, error) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if used {
				return nil, errors.New("credential already attempted")
			}
			used = true
			return bytes.Clone(password), nil
		}
	}
	if r.KeyPassphrase != nil {
		passphrase = []byte(*r.KeyPassphrase)
		*r.KeyPassphrase = ""
		r.KeyPassphrase = nil
		used := map[string]bool{}
		c.KeyPassphrase = func(ctx context.Context, path string) ([]byte, error) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if used[path] {
				return nil, errors.New("credential already attempted")
			}
			used[path] = true
			return bytes.Clone(passphrase), nil
		}
	}
	return c, func() { clear(password); clear(passphrase) }, nil
}
