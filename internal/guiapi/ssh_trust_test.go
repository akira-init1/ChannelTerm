package guiapi

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/core/app"
	"github.com/akira-init1/ChannelTerm/internal/core/session"
	sshtransport "github.com/akira-init1/ChannelTerm/internal/transport/ssh"
	gossh "golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

func TestSSHTrustDeadlineWithdrawsCapability(t *testing.T) {
	handler := newSSHHandler(openerFunc(func(ctx context.Context, request app.OpenSSHRequest) (app.OpenSSHResult, error) {
		_, err := request.Config.ConfirmHostKey(ctx, sshtransport.HostKey{Address: "example.invalid:22", Algorithm: "ssh-ed25519", Fingerprint: "SHA256:" + strings.Repeat("a", 43)})
		return app.OpenSSHResult{}, err
	}))
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	r := httptest.NewRequest(http.MethodPost, SSHPath, strings.NewReader(`{"host":"example.invalid","port":22,"user":"fixture","password":"fake","known_hosts_file":"fixture-trust","confirm_host_key":true}`)).WithContext(ctx)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	decoder := json.NewDecoder(w.Body)
	var prompt, result map[string]any
	if err := decoder.Decode(&prompt); err != nil {
		t.Fatal(err)
	}
	if err := decoder.Decode(&result); err != nil {
		t.Fatal(err)
	}
	if result["event"] != "error" || result["error"] != "cancelled" {
		t.Fatalf("deadline: %+v", result)
	}
	body, _ := json.Marshal(map[string]any{"challenge_id": prompt["challenge_id"], "accept": true})
	r = httptest.NewRequest(http.MethodPost, SSHConfirmPath, bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 409 {
		t.Fatal("expired capability accepted")
	}
}

func TestSSHConfirmationBoundary(t *testing.T) {
	handler := NewSSHHandler(nil)
	valid := `{"challenge_id":"` + strings.Repeat("a", 64) + `","accept":true}`
	for _, tc := range []struct {
		method, body, origin, media string
		status                      int
	}{
		{"GET", "", "", "application/json", 405},
		{"POST", valid, "https://example.invalid", "application/json", 403},
		{"POST", valid, "", "text/plain", 415},
		{"POST", valid, "", "application/json", 409},
		{"POST", strings.Replace(valid, `"accept":true`, `"accept":null`, 1), "", "application/json", 400},
		{"POST", valid + `{}`, "", "application/json", 400},
		{"POST", strings.Repeat("x", 1025), "", "application/json", 400},
		{"POST", `{"challenge_id":"x","accept":true,"key":"replacement"}`, "", "application/json", 400},
	} {
		r := httptest.NewRequest(tc.method, SSHConfirmPath, strings.NewReader(tc.body))
		r.Header.Set("Content-Type", tc.media)
		r.Header.Set("Origin", tc.origin)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Fatalf("%s %s: %d", tc.method, tc.body, w.Code)
		}
	}
}

// TestSSHTrustBindsConfirmationToLiveHandshake exercises actual SSH verification,
// persistence, refusal, cancellation and concurrent trust-file replacement.
func TestSSHTrustBindsConfirmationToLiveHandshake(t *testing.T) {
	for _, mode := range []string{"accept", "accept-delayed", "reject", "cancel", "changed", "revoked", "changed-during-confirm"} {
		t.Run(mode, func(t *testing.T) {
			host, port, trust, _ := sshFixture(t, false)
			original, err := os.ReadFile(trust)
			if err != nil {
				t.Fatal(err)
			}
			_, private, _ := ed25519.GenerateKey(rand.Reader)
			other, _ := gossh.NewSignerFromKey(private)
			changed := []byte(knownhosts.Line([]string{net.JoinHostPort(host, strconv.Itoa(port))}, other.PublicKey()) + "\n")
			initial := []byte("# keep this comment\n")
			if mode == "changed" {
				initial = changed
			}
			if mode == "revoked" {
				initial = append([]byte("@revoked "), original...)
			}
			if err := os.WriteFile(trust, initial, 0600); err != nil {
				t.Fatal(err)
			}
			manager := session.NewManager()
			defer manager.Close()
			application, err := app.New(app.Dependencies{Manager: manager})
			if err != nil {
				t.Fatal(err)
			}
			server := httptest.NewServer(NewHandler(application))
			defer server.Close()
			client := &http.Client{Timeout: 15 * time.Second, Transport: &http.Transport{}}
			defer client.CloseIdleConnections()
			body, _ := json.Marshal(map[string]any{"host": host, "port": port, "user": "fixture-user", "password": "fixture-password", "known_hosts_file": trust, "confirm_host_key": true})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			r, _ := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+SSHPath, bytes.NewReader(body))
			r.Header.Set("Content-Type", "application/json")
			response, err := client.Do(r)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			decoder := json.NewDecoder(response.Body)
			var event map[string]any
			if err := decoder.Decode(&event); err != nil {
				t.Fatal(err)
			}
			if mode == "changed" || mode == "revoked" {
				if event["event"] != "error" || event["error"] != "host_key_rejected" {
					t.Fatalf("unsafe trust: %+v", event)
				}
				return
			}
			id, ok := event["challenge_id"].(string)
			if !ok || len(id) != 64 || event["event"] != "trust" || event["algorithm"] != "ssh-ed25519" || event["address"] != knownhosts.Normalize(net.JoinHostPort(host, strconv.Itoa(port))) || !strings.HasPrefix(event["fingerprint"].(string), "SHA256:") {
				t.Fatalf("identity: %+v", event)
			}
			decide := func(id string, accept bool) int {
				t.Helper()
				data, _ := json.Marshal(map[string]any{"challenge_id": id, "accept": accept})
				res, err := client.Post(server.URL+SSHConfirmPath, "application/json", bytes.NewReader(data))
				if err != nil {
					t.Fatal(err)
				}
				_, _ = io.Copy(io.Discard, res.Body)
				_ = res.Body.Close()
				return res.StatusCode
			}
			if mode == "accept-delayed" {
				time.Sleep(6 * time.Second)
			}
			if decide(strings.Repeat("0", 64), true) != 409 {
				t.Fatal("foreign capability accepted")
			}
			if mode == "cancel" {
				cancel()
				_ = response.Body.Close()
				deadline := time.Now().Add(time.Second)
				for decide(id, false) != 409 {
					if time.Now().After(deadline) {
						t.Fatal("cancelled capability retained")
					}
				}
			} else {
				if mode == "changed-during-confirm" {
					if err := os.WriteFile(trust, changed, 0600); err != nil {
						t.Fatal(err)
					}
				}
				if decide(id, mode != "reject") != 200 {
					t.Fatal("decision rejected")
				}
				if decide(id, true) != 409 {
					t.Fatal("decision replay accepted")
				}
				event = make(map[string]any)
				if err := decoder.Decode(&event); err != nil {
					t.Fatal(err)
				}
				if strings.HasPrefix(mode, "accept") {
					if event["event"] != "result" || len(manager.List()) != 1 {
						t.Fatalf("not opened: %+v", event)
					}
				} else if event["event"] != "error" {
					t.Fatalf("unsafe success: %+v", event)
				}
			}
			stored, err := os.ReadFile(trust)
			if err != nil {
				t.Fatal(err)
			}
			want := initial
			if strings.HasPrefix(mode, "accept") {
				want = append(initial, original...)
			}
			if mode == "changed-during-confirm" {
				want = changed
			}
			if !bytes.Equal(stored, want) {
				t.Fatalf("unexpected trust modification: %q", stored)
			}
			if !strings.HasPrefix(mode, "accept") && len(manager.List()) != 0 {
				t.Fatal("failed trust created Session")
			}
		})
	}
}
