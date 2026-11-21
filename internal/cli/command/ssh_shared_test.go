package command

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/core/config"
	"github.com/akira-init1/ChannelTerm/internal/guiapi"
	sshtransport "github.com/akira-init1/ChannelTerm/internal/transport/ssh"
)

// TestSSHOpensSecondSessionOnForegroundHost exercises both CLI entry paths
// against independent loopback SSH servers and the real authenticated Host.
func TestSSHOpensSecondSessionOnForegroundHost(t *testing.T) {
	t.Setenv(config.HostConfigDirectoryEnv, t.TempDir())
	t.Setenv(httpAuthTokenEnvVar, "")
	first, _ := sshCommandServer(t)
	second, _ := sshCommandServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	start := func(args []string) (string, *io.PipeWriter, <-chan error) {
		in, keys := io.Pipe()
		t.Cleanup(func() { keys.Close(); in.Close() })
		var out lockedBuffer
		done := make(chan error, 1)
		go func() { done <- Run(ctx, append([]string{"ssh"}, args...), in, &out, io.Discard) }()
		return waitSSHOutput(t, &out, "test-shell$ "), keys, done
	}
	output, owner, ownerDone := start(first)
	_, after, ok := strings.Cut(output, "Shared Host: ")
	if !ok {
		t.Fatal("missing Host address")
	}
	endpoint := strings.Fields(after)[0]
	for i, flag := range second {
		if flag == "--listen" {
			second = append(second[:i], second[i+2:]...)
			break
		}
	}
	second = append(second, "--endpoint", endpoint)
	t.Setenv(httpAuthTokenEnvVar, "unregistered-fixture-token")
	if err := runSSH(ctx, second, strings.NewReader(""), io.Discard, nil); err == nil || !strings.Contains(err.Error(), "Unauthorized") {
		t.Fatalf("unregistered client: %v", err)
	}
	t.Setenv(httpAuthTokenEnvVar, "")
	_, attached, attachedDone := start(second)
	if _, err := attached.Write([]byte{0x1d, 'q'}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-attachedDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	listed, err := listMCPSessions(ctx, endpoint)
	if err != nil || len(listed) != 2 {
		t.Fatalf("after detach: sessions=%+v err=%v", listed, err)
	}
	for _, s := range listed {
		if s.State != "open" {
			t.Fatalf("Session after detach: %+v", s)
		}
	}
	if _, err := owner.Write([]byte{0x1d, 'q'}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-ownerDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}

// TestSharedSSHConfirmsHandshake exercises the streamed challenge and ensures
// a rejected fingerprint never attaches, even if the server sends a result.
func TestSharedSSHConfirmsHandshake(t *testing.T) {
	t.Setenv(httpAuthTokenEnvVar, "fixture-management")
	for _, approve := range []bool{false, true} {
		decision := make(chan bool, 1)
		challenge := strings.Repeat("a", 64)
		host := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer fixture-management" {
				t.Error("unexpected method or credential")
			}
			switch r.URL.Path {
			case guiapi.SSHPath:
				var req guiapi.SSHRequest
				if json.NewDecoder(r.Body).Decode(&req) != nil || !req.ConfirmHostKey || req.Password == nil || *req.Password != "fixture-password" {
					t.Error("missing shared credentials/confirmation")
				}
				enc := json.NewEncoder(w)
				enc.Encode(map[string]string{"event": "trust", "challenge_id": challenge, "address": "test:22", "algorithm": "ssh-ed25519", "fingerprint": "SHA256:fixture"})
				w.(http.Flusher).Flush()
				select {
				case <-decision:
				case <-r.Context().Done():
					return
				}
				enc.Encode(map[string]string{"event": "result", "session_id": "fixture-session"})
			case guiapi.SSHConfirmPath:
				var answer struct {
					ID     string `json:"challenge_id"`
					Accept bool
				}
				if json.NewDecoder(r.Body).Decode(&answer) != nil || answer.ID != challenge || answer.Accept != approve {
					t.Error("incorrect trust decision")
				}
				decision <- answer.Accept
			default:
				http.NotFound(w, r)
			}
		}))
		u, err := url.Parse(host.URL)
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		id, err := createSharedSSH(ctx, u, sshtransport.Config{Password: func(context.Context) ([]byte, error) { return []byte("fixture-password"), nil }, ConfirmHostKey: func(_ context.Context, key sshtransport.HostKey) (bool, error) {
			if key.Fingerprint != "SHA256:fixture" {
				t.Fatal("wrong key")
			}
			return approve, nil
		}}, "", false, false)
		cancel()
		host.Close()
		if approve && (err != nil || id != "fixture-session") || !approve && (err == nil || id != "") {
			t.Fatalf("approve=%t id=%q err=%v", approve, id, err)
		}
	}
}
