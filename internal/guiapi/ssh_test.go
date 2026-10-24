package guiapi

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/core/app"
	"github.com/akira-init1/ChannelTerm/internal/core/session"
	gossh "golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

type openerFunc func(context.Context, app.OpenSSHRequest) (app.OpenSSHResult, error)

func (f openerFunc) OpenSSH(ctx context.Context, r app.OpenSSHRequest) (app.OpenSSHResult, error) {
	return f(ctx, r)
}

func request(handler http.Handler, method, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, SSHPath, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

// TestSSHRequestBoundary rejects browser origins and malformed/excessive input
// before entering the application; diagnostic bodies never echo credentials.
func TestSSHRequestBoundary(t *testing.T) {
	calls := 0
	h := newSSHHandler(openerFunc(func(context.Context, app.OpenSSHRequest) (app.OpenSSHResult, error) {
		calls++
		return app.OpenSSHResult{}, errors.New("SECRET-DO-NOT-EXPOSE")
	}))
	valid := `{"host":"example.invalid","user":"user","port":22,"password":"SECRET-DO-NOT-EXPOSE"}`
	for _, body := range []string{`null`, `{}`, valid + `{}`, strings.Replace(valid, `"port":22`, `"port":0`, 1), strings.Replace(valid, `"port":22`, `"port":22,"skip_host_key":true`, 1), strings.Repeat("x", 32769)} {
		w := request(h, http.MethodPost, body)
		if w.Code != 400 {
			t.Fatalf("status=%d", w.Code)
		}
	}
	if calls != 0 {
		t.Fatal("invalid input reached application")
	}
	r := httptest.NewRequest(http.MethodPost, SSHPath, strings.NewReader(valid))
	r.Header.Set("Origin", "https://untrusted.invalid")
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 || calls != 0 {
		t.Fatal("browser request accepted")
	}
	if w = request(h, http.MethodDelete, ""); w.Code != 405 {
		t.Fatal("unexpected method accepted")
	}
	if w = request(h, http.MethodGet, ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"available":true`) {
		t.Fatal("capability missing")
	}
	w = request(h, http.MethodPost, valid)
	if w.Code != 422 || strings.Contains(w.Body.String(), "SECRET") || calls != 1 {
		t.Fatalf("unsafe failure %s", w.Body)
	}
}

// TestSSHCredentialsAreEphemeral ensures retained callbacks cannot recover a
// submitted password/passphrase after the request and never skip host checks.
func TestSSHCredentialsAreEphemeral(t *testing.T) {
	var retained app.OpenSSHRequest
	h := newSSHHandler(openerFunc(func(ctx context.Context, r app.OpenSSHRequest) (app.OpenSSHResult, error) {
		retained = r
		return app.OpenSSHResult{Info: session.SessionInfo{ID: "opaque"}}, nil
	}))
	w := request(h, http.MethodPost, `{"host":"example.invalid","user":"user","port":22,"known_hosts_file":"fixture-trust","identity_files":["fixture-key"],"password":"secret-password","key_passphrase":"secret-phrase"}`)
	if w.Code != 200 || retained.Config.ConfirmHostKey != nil {
		t.Fatal("unsafe configuration")
	}
	p, err := retained.Config.Password(context.Background())
	if err != nil || !bytes.Equal(p, make([]byte, len(p))) {
		t.Fatal("password retained")
	}
	p, err = retained.Config.KeyPassphrase(context.Background(), "fixture-key")
	if err != nil || !bytes.Equal(p, make([]byte, len(p))) {
		t.Fatal("passphrase retained")
	}
	if strings.Contains(w.Body.String(), "secret") {
		t.Fatal("secret in response")
	}
}

// TestSSHSetupCancellationAndConcurrency bounds setup and prevents unbounded
// authenticated request fan-out without introducing a second Session manager.
func TestSSHSetupCancellationAndConcurrency(t *testing.T) {
	entered := make(chan struct{}, 2)
	h := newSSHHandler(openerFunc(func(ctx context.Context, _ app.OpenSSHRequest) (app.OpenSSHResult, error) {
		entered <- struct{}{}
		<-ctx.Done()
		return app.OpenSSHResult{}, ctx.Err()
	}))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan int, 2)
	body := `{"host":"example.invalid","user":"user","port":22,"password":"fake"}`
	for range 2 {
		go func() {
			r := httptest.NewRequest(http.MethodPost, SSHPath, strings.NewReader(body)).WithContext(ctx)
			r.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			done <- w.Code
		}()
	}
	for range 2 {
		select {
		case <-entered:
		case <-time.After(time.Second):
			t.Fatal("setup not reached")
		}
	}
	if w := request(h, http.MethodPost, body); w.Code != 429 {
		t.Fatal("setup concurrency not bounded")
	}
	cancel()
	for range 2 {
		select {
		case code := <-done:
			if code != 422 {
				t.Fatal(code)
			}
		case <-time.After(time.Second):
			t.Fatal("cancellation blocked")
		}
	}
}

// TestSSHRealAuthenticationTrustAndSharedLifecycle uses actual SSH on loopback
// with temporary keys; it never connects to hardware or uses personal trust.
func TestSSHRealAuthenticationTrustAndSharedLifecycle(t *testing.T) {
	for _, mode := range []string{"password", "key", "encrypted-key", "wrong-password", "unknown-key", "changed-key", "revoked-key"} {
		t.Run(mode, func(t *testing.T) {
			host, port, trust, key := sshFixture(t, mode == "encrypted-key")
			if mode == "unknown-key" {
				if err := os.WriteFile(trust, nil, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "changed-key" {
				_, private, _ := ed25519.GenerateKey(rand.Reader)
				signer, _ := gossh.NewSignerFromKey(private)
				if err := os.WriteFile(trust, []byte(knownhosts.Line([]string{net.JoinHostPort(host, strconv.Itoa(port))}, signer.PublicKey())+"\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "revoked-key" {
				data, _ := os.ReadFile(trust)
				if err := os.WriteFile(trust, append([]byte("@revoked "), data...), 0600); err != nil {
					t.Fatal(err)
				}
			}
			manager := session.NewManager()
			defer manager.Close()
			application, err := app.New(app.Dependencies{Manager: manager})
			if err != nil {
				t.Fatal(err)
			}
			input := map[string]any{"host": host, "port": port, "user": "fixture-user", "known_hosts_file": trust, "label": "GUI test"}
			if mode == "key" || mode == "encrypted-key" {
				input["identity_files"] = []string{key}
				if mode == "encrypted-key" {
					input["key_passphrase"] = "fixture-passphrase"
				}
			} else {
				input["password"] = "fixture-password"
			}
			if mode == "wrong-password" {
				input["password"] = "wrong-secret"
			}
			body, _ := json.Marshal(input)
			h := NewSSHHandler(application)
			w := request(h, http.MethodPost, string(body))
			if mode == "wrong-password" || strings.HasSuffix(mode, "-key") && mode != "encrypted-key" {
				if w.Code != 422 || len(manager.List()) != 0 {
					t.Fatalf("unsafe failed setup: %d %s", w.Code, w.Body)
				}
				if strings.Contains(w.Body.String(), "secret") || strings.Contains(w.Body.String(), trust) {
					t.Fatal("sensitive failure detail")
				}
				return
			}
			if w.Code != 200 {
				t.Fatalf("open %d %s", w.Code, w.Body)
			}
			var opened struct {
				ID     string `json:"session_id"`
				Reused bool   `json:"reused"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &opened); err != nil {
				t.Fatal(err)
			}
			if opened.ID == "" || len(application.ListSessions()) != 1 || application.ListSessions()[0].Metadata.Transport != "ssh" {
				t.Fatal("not shared through Manager")
			}
			if err := application.AttachSession(opened.ID, "user"); err != nil {
				t.Fatal(err)
			}
			if err := application.DetachSession(opened.ID, "user"); err != nil {
				t.Fatal(err)
			}
			live, ok := manager.Get(opened.ID)
			if !ok {
				t.Fatal("GUI detach removed Session")
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if _, err := application.WriteSession(ctx, opened.ID, session.WriteRequest{Data: []byte("echo\n"), Actor: "user"}); err != nil {
				t.Fatal(err)
			}
			// A nil cursor is an immediate history snapshot, not a wait for the
			// asynchronous SSH reader. Wait for the echo across arbitrary chunks.
			cursor := session.OutputCursor(0)
			var received []byte
			for !bytes.Contains(received, []byte("echo\n")) {
				chunk, err := application.ReadSession(ctx, opened.ID, &cursor, 1024)
				if err != nil {
					t.Fatalf("shared echo read: %v (received %q)", err, received)
				}
				cursor = chunk.Next
				received = append(received, chunk.Data...)
				if chunk.Dropped || len(received) > 1024 {
					t.Fatal("unexpected or dropped SSH fixture output")
				}
			}
			w = request(h, http.MethodPost, string(body))
			if w.Code != 200 {
				t.Fatal(w.Body)
			}
			_ = json.Unmarshal(w.Body.Bytes(), &opened)
			if !opened.Reused || len(manager.List()) != 1 {
				t.Fatal("duplicate Session")
			}
			metadata, _ := json.Marshal(application.ListSessions())
			if bytes.Contains(metadata, []byte("fixture-password")) || bytes.Contains(metadata, []byte("fixture-passphrase")) || bytes.Contains(metadata, []byte(key)) {
				t.Fatal("credential in Session metadata")
			}
			if err := manager.Close(); err != nil {
				t.Fatal(err)
			}
			if live.State() == session.StateOpen {
				t.Fatal("owner close left Session open")
			}
		})
	}
}

func sshFixture(t *testing.T, encrypted bool) (string, int, string, string) {
	t.Helper()
	directory := t.TempDir()
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := gossh.NewSignerFromKey(private)
	if err != nil {
		t.Fatal(err)
	}
	block, err := gossh.MarshalPrivateKey(private, "fixture")
	if encrypted {
		block, err = gossh.MarshalPrivateKeyWithPassphrase(private, "fixture", []byte("fixture-passphrase"))
	}
	if err != nil {
		t.Fatal(err)
	}
	key := filepath.Join(directory, "identity")
	if err := os.WriteFile(key, pem.EncodeToMemory(block), 0600); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	host, port, _ := net.SplitHostPort(listener.Addr().String())
	number, _ := strconv.Atoi(port)
	trust := filepath.Join(directory, "known_hosts")
	if err := os.WriteFile(trust, []byte(knownhosts.Line([]string{listener.Addr().String()}, signer.PublicKey())+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	config := &gossh.ServerConfig{PasswordCallback: func(meta gossh.ConnMetadata, p []byte) (*gossh.Permissions, error) {
		if meta.User() != "fixture-user" || string(p) != "fixture-password" {
			return nil, errors.New("rejected")
		}
		return nil, nil
	}, PublicKeyCallback: func(meta gossh.ConnMetadata, key gossh.PublicKey) (*gossh.Permissions, error) {
		if meta.User() != "fixture-user" || !bytes.Equal(key.Marshal(), signer.PublicKey().Marshal()) {
			return nil, errors.New("rejected")
		}
		return nil, nil
	}}
	config.AddHostKey(signer)
	var mu sync.Mutex
	var sockets []net.Conn
	var workers sync.WaitGroup
	accepted := make(chan struct{})
	go func() {
		defer close(accepted)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			sockets = append(sockets, conn)
			mu.Unlock()
			workers.Add(1)
			go func() {
				defer workers.Done()
				defer conn.Close()
				server, channels, requests, err := gossh.NewServerConn(conn, config)
				if err != nil {
					return
				}
				defer server.Close()
				go gossh.DiscardRequests(requests)
				for channel := range channels {
					if channel.ChannelType() != "session" {
						_ = channel.Reject(gossh.UnknownChannelType, "session only")
						continue
					}
					stream, requests, err := channel.Accept()
					if err != nil {
						return
					}
					for req := range requests {
						ok := req.Type == "pty-req" || req.Type == "shell"
						_ = req.Reply(ok, nil)
						if req.Type == "shell" {
							go gossh.DiscardRequests(requests)
							_, _ = stream.Write([]byte("ready\n"))
							_, _ = io.Copy(stream, stream)
							_ = stream.Close()
							break
						}
					}
				}
			}()
		}
	}()
	t.Cleanup(func() {
		_ = listener.Close()
		<-accepted
		mu.Lock()
		for _, socket := range sockets {
			_ = socket.Close()
		}
		mu.Unlock()
		workers.Wait()
	})
	return host, number, trust, key
}
