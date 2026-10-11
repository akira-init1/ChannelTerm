package ssh

import (
	"bytes"
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh/knownhosts"
)

// TestPasswordAuthenticationAndFallback verifies key precedence, retry bounds,
// cancellation, and clearing of callback-owned password buffers.
func TestPasswordAuthenticationAndFallback(t *testing.T) {
	for _, mode := range []string{"password-only", "rejected-key", "accepted-key", "retry", "exhausted", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			server := startTestServer(t, "password")
			switch mode {
			case "accepted-key":
			case "rejected-key":
				_, server.config.PrivateKeyPath = testIdentity(t)
			default:
				server.config.PrivateKeyPath = ""
			}
			calls := 0
			var supplied [][]byte
			server.config.Password = func(ctx context.Context) ([]byte, error) {
				calls++
				if mode == "cancel" {
					return nil, context.Canceled
				}
				password := []byte("fixture-password")
				if mode == "exhausted" || mode == "retry" && calls == 1 {
					password = []byte("fixture-wrong-password")
				}
				supplied = append(supplied, password)
				return password, nil
			}
			transport, err := New(server.config)
			if err != nil {
				t.Fatal(err)
			}
			stream, err := transport.Connect(context.Background())
			if mode == "exhausted" || mode == "cancel" {
				if err == nil || stream != nil {
					t.Fatalf("unexpected success: %v", stream)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				defer stream.Close()
				readExactly(t, stream, "ready> ")
			}
			want := 1
			if mode == "accepted-key" {
				want = 0
			}
			if mode == "retry" {
				want = 2
			}
			if mode == "exhausted" {
				want = 3
			}
			if calls != want {
				t.Fatalf("password calls=%d want=%d", calls, want)
			}
			for _, password := range supplied {
				if !bytes.Equal(password, make([]byte, len(password))) {
					t.Fatal("password bytes were retained")
				}
			}
		})
	}
}

// TestFirstHostTrustAndReconnect covers persistent trust before authentication
// and rekeying after setup ownership has ended.
func TestFirstHostTrustAndReconnect(t *testing.T) {
	server := startTestServer(t, "password")
	server.config.PrivateKeyPath = ""
	server.config.KnownHostsPath = filepath.Join(t.TempDir(), ".ssh", "known_hosts")
	confirmed, prompted := 0, 0
	server.config.ConfirmHostKey = func(ctx context.Context, key HostKey) (bool, error) {
		confirmed++
		if key.Address != knownhosts.Normalize(net.JoinHostPort(server.config.Host, strconv.Itoa(server.config.Port))) || key.Algorithm != "ssh-ed25519" || !strings.HasPrefix(key.Fingerprint, "SHA256:") {
			t.Errorf("invalid host identity: %+v", key)
		}
		if prompted != 0 {
			t.Error("password requested before verification")
		}
		return true, nil
	}
	server.config.Password = func(context.Context) ([]byte, error) { prompted++; return []byte("fixture-password"), nil }
	for i := 0; i < 2; i++ {
		transport, err := New(server.config)
		if err != nil {
			t.Fatal(err)
		}
		stream, err := transport.Connect(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		readExactly(t, stream, "ready> ")
		// The small server threshold forces rekey after setup has returned;
		// a newly approved key must stay trusted without another prompt.
		payload := strings.Repeat("x", 8192) + "\n"
		if _, err := stream.Write([]byte(payload)); err != nil {
			t.Fatal(err)
		}
		readExactly(t, stream, "echo: "+payload)
		_ = stream.Close()
	}
	if confirmed != 1 || prompted != 2 {
		t.Fatalf("confirm=%d passwords=%d", confirmed, prompted)
	}
	data, err := os.ReadFile(server.config.KnownHostsPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(data), "ssh-ed25519") != 1 || bytes.Contains(data, []byte("fixture-password")) {
		t.Fatalf("invalid trust contents: %q", data)
	}
}

// TestHostTrustRejectionDoesNotRequestPassword ensures unsafe or failed trust
// decisions neither modify existing records nor solicit credentials.
func TestHostTrustRejectionDoesNotRequestPassword(t *testing.T) {
	for _, mode := range []string{"no", "cancel", "changed", "revoked", "write-failure", "concurrent-change"} {
		t.Run(mode, func(t *testing.T) {
			server := startTestServer(t, "password")
			original, _ := os.ReadFile(server.config.KnownHostsPath)
			address := net.JoinHostPort(server.config.Host, strconv.Itoa(server.config.Port))
			var data []byte
			if mode == "changed" {
				data = []byte(knownhosts.Line([]string{address}, testSigner(t).PublicKey()) + "\n")
			}
			if mode == "revoked" {
				data = append([]byte("@revoked "), original...)
			}
			if err := os.WriteFile(server.config.KnownHostsPath, data, 0600); err != nil {
				t.Fatal(err)
			}
			confirmCalls, passwordCalls := 0, 0
			server.config.PrivateKeyPath = ""
			server.config.Password = func(context.Context) ([]byte, error) { passwordCalls++; return []byte("fixture-password"), nil }
			server.config.ConfirmHostKey = func(ctx context.Context, key HostKey) (bool, error) {
				confirmCalls++
				switch mode {
				case "cancel":
					return false, context.Canceled
				case "write-failure":
					// Directory locks are valid on macOS, so a directory at the
					// lock path is not a portable failure fixture. Replace the
					// trust destination after New has loaded its snapshot: no
					// supported OS can append a host-key record to a directory.
					if err := os.Remove(server.config.KnownHostsPath); err != nil {
						return false, err
					}
					if err := os.Mkdir(server.config.KnownHostsPath, 0700); err != nil {
						return false, err
					}
					return true, nil
				case "concurrent-change":
					data = []byte(knownhosts.Line([]string{address}, testSigner(t).PublicKey()) + "\n")
					if err := os.WriteFile(server.config.KnownHostsPath, data, 0600); err != nil {
						t.Error(err)
					}
					return true, nil
				}
				return false, nil
			}
			transport, err := New(server.config)
			if err != nil {
				t.Fatal(err)
			}
			stream, err := transport.Connect(context.Background())
			if err == nil || stream != nil {
				t.Fatal("untrusted connection accepted")
			}
			if passwordCalls != 0 {
				t.Fatal("password requested before trust")
			}
			if (mode == "changed" || mode == "revoked") && confirmCalls != 0 {
				t.Fatal("prompted to replace an existing key")
			}
			if mode == "write-failure" {
				info, err := os.Stat(server.config.KnownHostsPath)
				if err != nil || !info.IsDir() || confirmCalls != 1 {
					t.Fatalf("unwritable trust destination changed: %v, confirmations=%d", err, confirmCalls)
				}
				return
			}
			after, readErr := os.ReadFile(server.config.KnownHostsPath)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if !bytes.Equal(after, data) {
				t.Fatal("trust file modified on rejection")
			}
		})
	}
}

// TestSaveHostKeyPreservesRecordsAndSerializesWriters checks concurrent appends,
// newline preservation, and OpenSSH address normalization including IPv6.
func TestSaveHostKeyPreservesRecordsAndSerializesWriters(t *testing.T) {
	key := testSigner(t).PublicKey()
	for _, host := range []string{"host.example:22", "host.example:2222", "[::1]:22", "[::1]:2222"} {
		t.Run(host, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "known_hosts")
			prefix := "# keep this comment\n" + knownhosts.Line([]string{knownhosts.HashHostname("other.example")}, testSigner(t).PublicKey())
			if err := os.WriteFile(path, []byte(prefix), 0600); err != nil {
				t.Fatal(err)
			}
			remote := &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 22}
			var wg sync.WaitGroup
			for i := 0; i < 4; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					if err := saveHostKey(context.Background(), path, host, remote, key); err != nil {
						t.Error(err)
					}
				}()
			}
			wg.Wait()
			data, _ := os.ReadFile(path)
			if string(data) != prefix+"\n"+knownhosts.Line([]string{host}, key)+"\n" {
				t.Fatal("records corrupted or duplicated")
			}
			verify, err := knownhosts.New(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := verify(host, remote, key); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// TestInteractionCancellationAndTimeout checks that callback waits are still
// interrupted by the caller while the network budget is paused.
func TestInteractionCancellationAndTimeout(t *testing.T) {
	for _, phase := range []string{"host", "password"} {
		t.Run(phase, func(t *testing.T) {
			server := startTestServer(t, "password")
			server.config.PrivateKeyPath = ""
			entered := make(chan struct{})
			server.config.Password = func(ctx context.Context) ([]byte, error) { close(entered); <-ctx.Done(); return nil, ctx.Err() }
			if phase == "host" {
				server.config.KnownHostsPath = filepath.Join(t.TempDir(), "missing")
				server.config.ConfirmHostKey = func(ctx context.Context, key HostKey) (bool, error) {
					close(entered)
					<-ctx.Done()
					return false, ctx.Err()
				}
			}
			transport, err := New(server.config)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			result := make(chan error, 1)
			go func() { _, err := transport.Connect(ctx); result <- err }()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("no prompt")
			}
			cancel()
			select {
			case err := <-result:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("cancel=%v", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("cancel blocked")
			}
		})
	}
}

// TestSetupBudgetExcludesInteractionButRetainsParentDeadline protects the two
// independent limits: network work budget and caller lifetime.
func TestSetupBudgetExcludesInteractionButRetainsParentDeadline(t *testing.T) {
	b := newSetupBudget(context.Background(), 100*time.Millisecond)
	defer b.cancel(nil)
	b.pause()
	time.Sleep(150 * time.Millisecond)
	if b.ctx.Err() != nil {
		t.Fatal("paused budget expired")
	}
	b.resume()
	select {
	case <-b.ctx.Done():
		if !errors.Is(context.Cause(b.ctx), context.DeadlineExceeded) {
			t.Fatal(context.Cause(b.ctx))
		}
	case <-time.After(time.Second):
		t.Fatal("budget did not resume")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	b = newSetupBudget(ctx, time.Second)
	defer b.cancel(nil)
	b.pause()
	<-b.ctx.Done()
	if !errors.Is(context.Cause(b.ctx), context.DeadlineExceeded) {
		t.Fatal(context.Cause(b.ctx))
	}
}

// TestPasswordPromptMayOutlastNetworkTimeout exercises the budget through an
// actual handshake rather than only the timer helper.
func TestPasswordPromptMayOutlastNetworkTimeout(t *testing.T) {
	server := startTestServer(t, "password")
	server.config.PrivateKeyPath = ""
	server.config.Timeout = time.Second
	server.config.Password = func(ctx context.Context) ([]byte, error) {
		select {
		case <-time.After(1200 * time.Millisecond):
			return []byte("fixture-password"), nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	transport, err := New(server.config)
	if err != nil {
		t.Fatal(err)
	}
	stream, err := transport.Connect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	readExactly(t, stream, "ready> ")
}

// TestDialTimeoutPreservesDeadlineError distinguishes exhausted setup time
// from explicit caller cancellation even before a socket exists.
func TestDialTimeoutPreservesDeadlineError(t *testing.T) {
	server := startTestServer(t, "")
	server.config.Timeout = 20 * time.Millisecond
	transport, err := New(server.config)
	if err != nil {
		t.Fatal(err)
	}
	transport.dial = func(ctx context.Context, network, address string) (net.Conn, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if _, err := transport.Connect(context.Background()); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("dial timeout=%v", err)
	}
}
