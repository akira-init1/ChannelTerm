package ssh

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	gossh "golang.org/x/crypto/ssh"
)

func encryptTestIdentity(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	key, err := gossh.ParseRawPrivateKey(data)
	if err != nil {
		t.Fatal(err)
	}
	block, err := gossh.MarshalPrivateKeyWithPassphrase(key, "fixture", []byte("fixture-passphrase"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestEncryptedIdentityAuthentication(t *testing.T) {
	for _, mode := range []string{"success", "retry", "wrong", "cancel", "reject-host", "rejected-first", "unused-second", "deadline", "no-provider", "no-provider-rejected"} {
		t.Run(mode, func(t *testing.T) {
			server := startTestServer(t, "")
			accepted := server.config.PrivateKeyPath
			encrypted := accepted
			if mode == "rejected-first" || mode == "unused-second" || mode == "no-provider-rejected" {
				_, encrypted = testIdentity(t)
			}
			encryptTestIdentity(t, encrypted)
			if mode == "rejected-first" || mode == "no-provider-rejected" {
				server.config.PrivateKeyPath = encrypted
				server.config.PrivateKeyPaths = []string{accepted}
			}
			if mode == "unused-second" {
				server.config.PrivateKeyPaths = []string{encrypted}
			}
			if mode == "reject-host" {
				server.config.KnownHostsPath = filepath.Join(t.TempDir(), "hosts")
				os.WriteFile(server.config.KnownHostsPath, nil, 0600)
			}
			calls := 0
			var secrets [][]byte
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			server.config.KeyPassphrase = func(ctx context.Context, path string) ([]byte, error) {
				calls++
				if path != encrypted {
					t.Errorf("wrong identity prompt: %s", path)
				}
				if mode == "cancel" {
					cancel()
					return []byte("discard"), context.Canceled
				}
				if mode == "deadline" {
					<-ctx.Done()
					return nil, ctx.Err()
				}
				secret := []byte("fixture-passphrase")
				if mode == "wrong" || mode == "retry" && calls == 1 {
					secret = []byte("incorrect")
				}
				secrets = append(secrets, secret)
				return secret, nil
			}
			if mode == "no-provider" || mode == "no-provider-rejected" {
				server.config.KeyPassphrase = nil
			}
			transport, err := New(server.config)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "deadline" {
				var stop context.CancelFunc
				ctx, stop = context.WithTimeout(ctx, 100*time.Millisecond)
				defer stop()
			}
			stream, err := transport.Connect(ctx)
			failure := mode == "wrong" || mode == "cancel" || mode == "reject-host" || mode == "deadline" || mode == "no-provider"
			if failure {
				if err == nil || stream != nil {
					t.Fatal("unexpected authentication success")
				}
				if mode == "cancel" && !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
				if mode == "deadline" && !errors.Is(err, context.DeadlineExceeded) {
					t.Fatal(err)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				readExactly(t, stream, "ready> ")
				stream.Close()
			}
			want := 1
			if mode == "retry" {
				want = 2
			}
			if mode == "wrong" {
				want = 3
			}
			if mode == "reject-host" || mode == "rejected-first" || mode == "unused-second" || mode == "no-provider" || mode == "no-provider-rejected" {
				want = 0
			}
			if calls != want {
				t.Fatalf("prompt calls=%d want=%d", calls, want)
			}
			for _, secret := range secrets {
				if !bytes.Equal(secret, make([]byte, len(secret))) {
					t.Fatal("passphrase bytes retained")
				}
			}
		})
	}
}

func TestSSHStageDiagnostics(t *testing.T) {
	for _, mode := range []string{"host-key", "authentication", "shell"} {
		t.Run(mode, func(t *testing.T) {
			behavior := ""
			if mode == "shell" {
				behavior = "reject-pty-req"
			}
			server := startTestServer(t, behavior)
			if mode == "authentication" {
				_, server.config.PrivateKeyPath = testIdentity(t)
			}
			if mode == "host-key" {
				server.config.KnownHostsPath = testKnownHosts(t, server.config.Host, testSigner(t).PublicKey())
			}
			transport, err := New(server.config)
			if err != nil {
				t.Fatal(err)
			}
			_, err = transport.Connect(context.Background())
			var staged *StageError
			if !errors.As(err, &staged) || staged.Stage != mode {
				t.Fatalf("diagnostic=%v", err)
			}
		})
	}
}

func TestEncryptedRSAIdentityPreservesSHA2(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	for _, legacy := range []bool{false, true} {
		var block *pem.Block
		if legacy {
			block, err = x509.EncryptPEMBlock(rand.Reader, "RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(key), []byte("fixture-passphrase"), x509.PEMCipherAES256)
		} else {
			block, err = gossh.MarshalPrivateKeyWithPassphrase(key, "fixture", []byte("fixture-passphrase"))
		}
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(t.TempDir(), "rsa")
		if err := os.WriteFile(path, pem.EncodeToMemory(block), 0600); err != nil {
			t.Fatal(err)
		}
		config := Config{PrivateKeyPath: path, KeyPassphrase: func(context.Context, string) ([]byte, error) { return []byte("fixture-passphrase"), nil }}
		identities, err := loadIdentities(config)
		if err != nil {
			t.Fatal(err)
		}
		transport := &Transport{config: config, identities: identities}
		budget := newSetupBudget(context.Background(), time.Second)
		signers, err := transport.connectionSigners(budget.ctx, budget, transport.identities)
		if err != nil {
			budget.cancel(nil)
			t.Fatal(err)
		}
		data := []byte("fixture signature")
		signature, err := signers[0].(gossh.AlgorithmSigner).SignWithAlgorithm(rand.Reader, data, gossh.KeyAlgoRSASHA512)
		budget.pause()
		budget.cancel(nil)
		if err != nil {
			t.Fatal(err)
		}
		if signature.Format != gossh.KeyAlgoRSASHA512 {
			t.Fatal("RSA downgraded to SHA-1")
		}
		if err := signers[0].PublicKey().Verify(data, signature); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSSHConnectionDiagnosticPreservesCause(t *testing.T) {
	server := startTestServer(t, "")
	for _, tc := range []struct {
		stage string
		cause error
	}{
		{"dns", &net.DNSError{Err: "fixture lookup failed", Name: "fixture.invalid"}},
		{"connect", context.DeadlineExceeded},
	} {
		transport, err := New(server.config)
		if err != nil {
			t.Fatal(err)
		}
		transport.dial = func(context.Context, string, string) (net.Conn, error) { return nil, tc.cause }
		_, err = transport.Connect(context.Background())
		var staged *StageError
		if !errors.As(err, &staged) || staged.Stage != tc.stage || !errors.Is(err, tc.cause) {
			t.Fatalf("diagnostic lost cause: %v", err)
		}
	}
}
