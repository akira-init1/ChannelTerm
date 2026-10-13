package ssh

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	gossh "golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// TestConnectPrefersKnownHostKey covers a server that offers an additional key
// preferred by the backend, while the existing trusted key remains available.
func TestConnectPrefersKnownHostKey(t *testing.T) {
	ecdsaKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ecdsaSigner, err := gossh.NewSignerFromKey(ecdsaKey)
	if err != nil {
		t.Fatal(err)
	}
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	rsaSigner, err := gossh.NewSignerFromKey(rsaKey)
	if err != nil {
		t.Fatal(err)
	}
	// A known_hosts RSA key must enable RSA SHA-2 negotiation, not just the
	// legacy ssh-rsa signature algorithm which modern servers may disable.
	rsaSHA2, err := gossh.NewSignerWithAlgorithms(rsaSigner.(gossh.AlgorithmSigner), []string{gossh.KeyAlgoRSASHA256, gossh.KeyAlgoRSASHA512})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		signer gossh.Signer
		hashed bool
	}{
		{"ed25519", testSigner(t), false},
		{"hashed-ed25519", testSigner(t), true},
		{"rsa-sha2", rsaSHA2, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := startTestServer(t, "", tc.signer, ecdsaSigner)
			original, err := os.ReadFile(server.config.KnownHostsPath)
			if err != nil {
				t.Fatal(err)
			}
			if tc.hashed {
				host, rest, _ := strings.Cut(string(original), " ")
				original = []byte(knownhosts.HashHostname(host) + " " + rest)
				if err := os.WriteFile(server.config.KnownHostsPath, original, 0600); err != nil {
					t.Fatal(err)
				}
			}
			prompts := 0
			server.config.ConfirmHostKey = func(context.Context, HostKey) (bool, error) {
				prompts++
				return false, nil
			}
			transport, err := New(server.config)
			if err != nil {
				t.Fatal(err)
			}
			stream, err := transport.Connect(context.Background())
			if err != nil {
				t.Fatalf("trusted key still available: %v", err)
			}
			if err := stream.Close(); err != nil {
				t.Fatal(err)
			}
			files, err := transport.OpenFiles(context.Background(), "scp")
			if err != nil {
				t.Fatalf("file connection did not select trusted key: %v", err)
			}
			if err := files.Close(); err != nil {
				t.Fatal(err)
			}
			stored, err := os.ReadFile(server.config.KnownHostsPath)
			if err != nil || string(stored) != string(original) || prompts != 0 {
				t.Fatalf("known host prompted or trust changed: prompts=%d err=%v", prompts, err)
			}
		})
	}
}

// TestHostKeyPreferenceMatchesTrustRecords exercises the backend's matching
// rules without DNS/network access, including exclusions and port isolation.
func TestHostKeyPreferenceMatchesTrustRecords(t *testing.T) {
	key := testSigner(t).PublicKey()
	for _, tc := range []struct {
		name, pattern, address string
		matched                bool
	}{
		{"wildcard", "[*.example.test]:2222", "shell.example.test:2222", true},
		{"negated", "[*.example.test]:2222,![blocked.example.test]:2222", "blocked.example.test:2222", false},
		{"other-port", "[shell.example.test]:2222", "shell.example.test:22", false},
		{"other-host", "shell.example.test", "other.example.test:22", false},
		{"ipv6", "[::1]:2222", "[::1]:2222", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "known_hosts")
			if err := os.WriteFile(file, []byte(knownhosts.Line([]string{tc.pattern}, key)+"\n"), 0600); err != nil {
				t.Fatal(err)
			}
			verify, err := knownhosts.New(file)
			if err != nil {
				t.Fatal(err)
			}
			algorithms := preferredHostKeyAlgorithms(verify, tc.address, &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 2222})
			if !tc.matched {
				if algorithms != nil {
					t.Fatalf("unmatched host changed default algorithms: %v", algorithms)
				}
				return
			}
			for _, algorithm := range algorithms {
				if strings.Contains(algorithm, "-cert-") {
					continue
				}
				if algorithm != gossh.KeyAlgoED25519 {
					t.Fatalf("first raw host key algorithm = %s", algorithm)
				}
				return
			}
			t.Fatal("matching trusted key was not offered")
		})
	}
}

// TestHostKeyPreferenceStillRejectsChangedAndRevokedKeys verifies that ordering
// cannot approve replacement keys, alter trust records, or solicit credentials.
func TestHostKeyPreferenceStillRejectsChangedAndRevokedKeys(t *testing.T) {
	for _, mode := range []string{"changed", "unavailable", "revoked"} {
		t.Run(mode, func(t *testing.T) {
			host := testSigner(t)
			if mode == "unavailable" {
				key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
				if err != nil {
					t.Fatal(err)
				}
				host, err = gossh.NewSignerFromKey(key)
				if err != nil {
					t.Fatal(err)
				}
			}
			server := startTestServer(t, "password", host)
			original, err := os.ReadFile(server.config.KnownHostsPath)
			if err != nil {
				t.Fatal(err)
			}
			address := net.JoinHostPort(server.config.Host, strconv.Itoa(server.config.Port))
			data := []byte(knownhosts.Line([]string{address}, testSigner(t).PublicKey()) + "\n")
			if mode == "revoked" {
				data = append(append([]byte(nil), original...), append([]byte("@revoked "), original...)...)
			}
			if err := os.WriteFile(server.config.KnownHostsPath, data, 0600); err != nil {
				t.Fatal(err)
			}
			server.config.PrivateKeyPath = ""
			prompts, passwords := 0, 0
			server.config.ConfirmHostKey = func(context.Context, HostKey) (bool, error) { prompts++; return true, nil }
			server.config.Password = func(context.Context) ([]byte, error) { passwords++; return []byte("fixture-password"), nil }
			transport, err := New(server.config)
			if err != nil {
				t.Fatal(err)
			}
			stream, err := transport.Connect(context.Background())
			if stream != nil {
				_ = stream.Close()
				t.Fatal("untrusted server accepted")
			}
			var stage *StageError
			if !errors.As(err, &stage) || stage.Stage != "host-key" || prompts != 0 || passwords != 0 {
				t.Fatalf("trust rejection changed: err=%v prompts=%d passwords=%d", err, prompts, passwords)
			}
			if mode == "revoked" {
				var revoked *knownhosts.RevokedError
				if !errors.As(err, &revoked) {
					t.Fatalf("lost revocation error: %v", err)
				}
			}
			stored, readErr := os.ReadFile(server.config.KnownHostsPath)
			if readErr != nil || string(stored) != string(data) {
				t.Fatalf("rejected host changed trust file: %v", readErr)
			}
		})
	}
}

// TestHostKeyPreferencePreservesCertificates uses different CA/host key types:
// the authority record's type must not restrict the certified host algorithm.
func TestHostKeyPreferencePreservesCertificates(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	authority, err := gossh.NewSignerFromKey(key)
	if err != nil {
		t.Fatal(err)
	}
	host := testSigner(t)
	certificate := &gossh.Certificate{
		Key: host.PublicKey(), CertType: gossh.HostCert,
		ValidPrincipals: []string{"127.0.0.1"}, ValidBefore: gossh.CertTimeInfinity,
	}
	if err := certificate.SignCert(rand.Reader, authority); err != nil {
		t.Fatal(err)
	}
	signer, err := gossh.NewCertSigner(certificate, host)
	if err != nil {
		t.Fatal(err)
	}
	server := startTestServer(t, "", signer, authority)
	address := net.JoinHostPort(server.config.Host, strconv.Itoa(server.config.Port))
	if err := os.WriteFile(server.config.KnownHostsPath, []byte("@cert-authority "+knownhosts.Line([]string{address}, authority.PublicKey())+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	transport, err := New(server.config)
	if err != nil {
		t.Fatal(err)
	}
	stream, err := transport.Connect(context.Background())
	if err != nil {
		t.Fatalf("trusted certificate was not selected: %v", err)
	}
	if err := stream.Close(); err != nil {
		t.Fatal(err)
	}
}
