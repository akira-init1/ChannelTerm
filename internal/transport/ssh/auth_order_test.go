package ssh

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestIdentityFailureDoesNotFallBackToPassword models a UI returning a cancel
// error without cancelling the caller's context, as the real prompt does.
func TestIdentityFailureDoesNotFallBackToPassword(t *testing.T) {
	for _, mode := range []string{"cancel", "exhausted"} {
		t.Run(mode, func(t *testing.T) {
			server := startTestServer(t, "password")
			encryptTestIdentity(t, server.config.PrivateKeyPath)
			server.config.KeyPassphrase = func(context.Context, string) ([]byte, error) {
				if mode == "cancel" {
					return nil, context.Canceled
				}
				return []byte("fixture-wrong"), nil
			}
			passwordCalls := 0
			server.config.Password = func(context.Context) ([]byte, error) { passwordCalls++; return []byte("fixture-password"), nil }
			transport, err := New(server.config)
			if err != nil {
				t.Fatal(err)
			}
			stream, err := transport.Connect(context.Background())
			if stream != nil {
				_ = stream.Close()
			}
			if err == nil || passwordCalls != 0 {
				t.Fatalf("identity failure fell back to password: calls=%d err=%v", passwordCalls, err)
			}
			if mode == "cancel" && !errors.Is(err, context.Canceled) {
				t.Fatalf("lost cancellation: %v", err)
			}
		})
	}
}

func TestAcceptedIdentityDoesNotUnlockLaterLegacyKey(t *testing.T) {
	server := startTestServer(t, "")
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	block, err := x509.EncryptPEMBlock(rand.Reader, "RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(key), []byte("fixture-passphrase"), x509.PEMCipherAES256)
	if err != nil {
		t.Fatal(err)
	}
	legacy := filepath.Join(t.TempDir(), "legacy-rsa")
	if err := os.WriteFile(legacy, pem.EncodeToMemory(block), 0600); err != nil {
		t.Fatal(err)
	}
	server.config.PrivateKeyPaths = []string{legacy}
	calls := 0
	server.config.KeyPassphrase = func(context.Context, string) ([]byte, error) { calls++; return nil, context.Canceled }
	transport, err := New(server.config)
	if err != nil {
		t.Fatal(err)
	}
	stream, err := transport.Connect(context.Background())
	if stream != nil {
		defer stream.Close()
	}
	if err != nil || calls != 0 {
		t.Fatalf("unused legacy key was requested before the accepted first key: calls=%d err=%v", calls, err)
	}
}
