package app

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/akira-init1/ChannelTerm/internal/core/session"
	sshtransport "github.com/akira-init1/ChannelTerm/internal/transport/ssh"
)

func sshTestConfig(t *testing.T) sshtransport.Config {
	t.Helper()
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	identity := filepath.Join(directory, "identity")
	trusted := filepath.Join(directory, "known_hosts")
	if err := os.WriteFile(identity, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: encoded}), 0600); err != nil {
		t.Fatal(err)
	}
	// The fake Session never connects. An empty trust database rejects all
	// hosts if accidentally used; Application tests need no SSH-library types.
	if err := os.WriteFile(trusted, nil, 0600); err != nil {
		t.Fatal(err)
	}
	return sshtransport.Config{User: "user", Host: "host.example", PrivateKeyPath: identity, KnownHostsPath: trusted}
}

// TestOpenSSHRegistersReusesAndSeparatesUsersAndPorts verifies endpoint identity, metadata, and Manager ownership.
func TestOpenSSHRegistersReusesAndSeparatesUsersAndPorts(t *testing.T) {
	manager := session.NewManager()
	defer manager.Close()
	application, _ := New(Dependencies{Manager: manager})
	request := OpenSSHRequest{Config: sshTestConfig(t), Label: "test shell"}
	var created []*fakeConnectedSession
	factory := func(id string, transport *sshtransport.Transport) (ConnectedSession, error) {
		terminal := newFakeConnectedSession(id)
		created = append(created, terminal)
		return terminal, nil
	}
	first, err := application.openSSH(context.Background(), request, factory)
	if err != nil {
		t.Fatal(err)
	}
	if first.Reused || first.Info.Metadata.Transport != "ssh" || first.Info.Metadata.Endpoint != "user@host.example:22" || first.Info.Metadata.Reference != "SSH-1" || len(first.Info.ID) != 32 {
		t.Fatalf("unexpected first Session: %+v", first)
	}
	request.Label = "ignored on reuse"
	second, err := application.openSSH(context.Background(), request, factory)
	if err != nil || !second.Reused || second.Info.ID != first.Info.ID || second.Info.Metadata.Label != "test shell" || len(created) != 1 {
		t.Fatalf("reuse=%+v, %v, creates=%d", second, err, len(created))
	}
	request.Config.User = "other-user"
	third, err := application.openSSH(context.Background(), request, factory)
	if err != nil || third.Reused || third.Info.Metadata.Reference != "SSH-2" {
		t.Fatalf("user=%+v, %v", third, err)
	}
	request.Config.Port = 2222
	fourth, err := application.openSSH(context.Background(), request, factory)
	if err != nil || fourth.Reused || fourth.Info.Metadata.Reference != "SSH-3" {
		t.Fatalf("port=%+v, %v", fourth, err)
	}
	if _, err := application.CloseSession("SSH-1"); err != nil || !created[0].closed {
		t.Fatalf("close=%v", err)
	}
	if err := manager.Close(); err != nil {
		t.Fatal(err)
	}
	for _, terminal := range created {
		if !terminal.closed {
			t.Error("Manager did not close SSH candidate")
		}
	}
}

type failingSSHSession struct {
	*fakeConnectedSession
	connect func(context.Context) error
}

// Connect injects connection errors or cancellation before registration.
func (s *failingSSHSession) Connect(ctx context.Context) error { return s.connect(ctx) }

// TestOpenSSHFailureAndCancellationReleaseCandidate keeps failed opens outside the Manager.
func TestOpenSSHFailureAndCancellationReleaseCandidate(t *testing.T) {
	for _, cancelAfterConnect := range []bool{false, true} {
		t.Run(map[bool]string{false: "connection-error", true: "cancel-before-register"}[cancelAfterConnect], func(t *testing.T) {
			manager := session.NewManager()
			defer manager.Close()
			application, _ := New(Dependencies{Manager: manager})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			failure := errors.New("test connection failure")
			var candidate *fakeConnectedSession
			_, err := application.openSSH(ctx, OpenSSHRequest{Config: sshTestConfig(t)}, func(id string, _ *sshtransport.Transport) (ConnectedSession, error) {
				candidate = newFakeConnectedSession(id)
				return &failingSSHSession{candidate, func(context.Context) error {
					if cancelAfterConnect {
						cancel()
						return nil
					}
					return failure
				}}, nil
			})
			want := failure
			if cancelAfterConnect {
				want = context.Canceled
			}
			if !errors.Is(err, want) || !candidate.closed || len(application.ListSessions()) != 0 {
				t.Fatalf("error=%v closed=%t sessions=%v", err, candidate.closed, application.ListSessions())
			}
		})
	}
}

// TestOpenSSHConcurrentReuse verifies one Session is created for simultaneous identical requests.
func TestOpenSSHConcurrentReuse(t *testing.T) {
	manager := session.NewManager()
	defer manager.Close()
	application, _ := New(Dependencies{Manager: manager})
	request := OpenSSHRequest{Config: sshTestConfig(t)}
	var mu sync.Mutex
	creates := 0
	factory := func(id string, _ *sshtransport.Transport) (ConnectedSession, error) {
		mu.Lock()
		creates++
		mu.Unlock()
		return newFakeConnectedSession(id), nil
	}
	var workers sync.WaitGroup
	for range 8 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			opened, err := application.openSSH(context.Background(), request, factory)
			if err != nil || opened.Info.Metadata.Reference != "SSH-1" {
				t.Errorf("open=%+v %v", opened, err)
			}
		}()
	}
	workers.Wait()
	if creates != 1 {
		t.Fatalf("created %d SSH Sessions for one endpoint", creates)
	}
}

// TestOpenSSHRejectsInvalidConfigurationAndPrecancel ensures errors precede connection attempts.
func TestOpenSSHRejectsInvalidConfigurationAndPrecancel(t *testing.T) {
	var missing *Application
	if _, err := missing.OpenSSH(context.Background(), OpenSSHRequest{}); !errors.Is(err, ErrNilApplicationManager) {
		t.Fatal(err)
	}
	manager := session.NewManager()
	defer manager.Close()
	application, _ := New(Dependencies{Manager: manager})
	if _, err := application.OpenSSH(context.Background(), OpenSSHRequest{}); err == nil {
		t.Fatal("invalid SSH config accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := application.OpenSSH(ctx, OpenSSHRequest{Config: sshTestConfig(t)}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
