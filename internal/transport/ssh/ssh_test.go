package ssh

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/core/channel"
	gossh "golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

type testServer struct {
	config   Config
	requests chan *gossh.Request
	seen     chan string
}

// startTestServer speaks actual SSH over loopback using fresh ephemeral keys.
// A stage can reject or stall setup, or leave shell input unread for flow-control
// tests. Cleanup closes every accepted socket before joining server goroutines.
func startTestServer(t *testing.T, behavior string) *testServer {
	t.Helper()
	hostKey := testSigner(t)
	clientKey, identity := testIdentity(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	host, port, _ := net.SplitHostPort(listener.Addr().String())
	portNumber, _ := strconv.Atoi(port)
	s := &testServer{
		config:   Config{User: "test-user", Host: host, Port: portNumber, PrivateKeyPath: identity, KnownHostsPath: testKnownHosts(t, listener.Addr().String(), hostKey.PublicKey()), Timeout: 3 * time.Second},
		requests: make(chan *gossh.Request, 16), seen: make(chan string, 16),
	}
	serverConfig := &gossh.ServerConfig{PublicKeyCallback: func(metadata gossh.ConnMetadata, key gossh.PublicKey) (*gossh.Permissions, error) {
		if metadata.User() != "test-user" || !bytes.Equal(key.Marshal(), clientKey.PublicKey().Marshal()) {
			return nil, errors.New("test key rejected")
		}
		return nil, nil
	}}
	serverConfig.AddHostKey(hostKey)
	var workers sync.WaitGroup
	var mu sync.Mutex
	var connections []net.Conn
	accepted := make(chan struct{})
	go func() {
		defer close(accepted)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			connections = append(connections, conn)
			mu.Unlock()
			workers.Add(1)
			go func() {
				defer workers.Done()
				defer conn.Close()
				if behavior == "stall-handshake" {
					s.seen <- "handshake"
					_, _ = io.Copy(io.Discard, conn)
					return
				}
				server, channels, requests, err := gossh.NewServerConn(conn, serverConfig)
				if err != nil {
					return
				}
				defer server.Close()
				go gossh.DiscardRequests(requests)
				for request := range channels {
					s.seen <- "open"
					if behavior == "stall-open" {
						_ = server.Wait()
						return
					}
					if request.ChannelType() != "session" {
						_ = request.Reject(gossh.UnknownChannelType, "only session allowed")
						continue
					}
					ch, requests, err := request.Accept()
					if err != nil {
						return
					}
					for req := range requests {
						s.requests <- req
						s.seen <- req.Type
						if behavior == "stall-"+req.Type {
							_ = server.Wait()
							return
						}
						ok := (req.Type == "pty-req" || req.Type == "shell") && behavior != "reject-"+req.Type
						_ = req.Reply(ok, nil)
						if req.Type != "shell" || !ok {
							continue
						}
						_, _ = ch.Write([]byte("ready> "))
						if behavior == "unread-input" {
							continue
						}
						if behavior == "stderr" {
							_, _ = ch.Stderr().Write(bytes.Repeat([]byte("E"), 3*1024*1024))
							_, _ = ch.Write([]byte("end\n"))
						}
						reader := bufio.NewReader(ch)
						for {
							line, err := reader.ReadString('\n')
							if err != nil {
								break
							}
							if line == "exit\n" {
								_, _ = ch.SendRequest("exit-status", false, gossh.Marshal(struct{ Status uint32 }{0}))
								_ = ch.Close()
								break
							}
							_, _ = ch.Write([]byte("echo: " + line))
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
		for _, conn := range connections {
			_ = conn.Close()
		}
		mu.Unlock()
		done := make(chan struct{})
		go func() { workers.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("SSH test server did not stop")
		}
	})
	return s
}

func testSigner(t *testing.T) gossh.Signer {
	t.Helper()
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := gossh.NewSignerFromKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return signer
}

// testIdentity writes an ephemeral key so tests use the same plain config as callers.
func testIdentity(t *testing.T) (gossh.Signer, string) {
	t.Helper()
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "identity")
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: encoded}), 0600); err != nil {
		t.Fatal(err)
	}
	signer, err := gossh.NewSignerFromKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return signer, path
}

func testKnownHosts(t *testing.T, address string, key gossh.PublicKey) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "known_hosts")
	if err := os.WriteFile(path, []byte(knownhosts.Line([]string{address}, key)+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func connectTestChannel(t *testing.T, configuration Config) channel.Channel {
	t.Helper()
	transport, err := New(configuration)
	if err != nil {
		t.Fatal(err)
	}
	stream, err := transport.Connect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stream.Close() })
	return stream
}

func readExactly(t *testing.T, stream io.Reader, expected string) {
	t.Helper()
	result := make(chan error, 1)
	go func() {
		data := make([]byte, len(expected))
		_, err := io.ReadFull(stream, data)
		if err == nil && string(data) != expected {
			err = errors.New("unexpected stream bytes")
		}
		result <- err
	}()
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out reading SSH output")
	}
}

// TestConnectPTYAndShell verifies PTY dimensions and raw shell bytes through a real SSH handshake.
func TestConnectPTYAndShell(t *testing.T) {
	server := startTestServer(t, "")
	server.config.Term, server.config.Columns, server.config.Rows = "vt100", 101, 37
	stream := connectTestChannel(t, server.config)
	pty, shell := <-server.requests, <-server.requests
	var dimensions struct {
		Term                         string
		Columns, Rows, Width, Height uint32
		Modes                        string
	}
	if err := gossh.Unmarshal(pty.Payload, &dimensions); err != nil {
		t.Fatal(err)
	}
	if pty.Type != "pty-req" || dimensions.Term != "vt100" || dimensions.Columns != 101 || dimensions.Rows != 37 || shell.Type != "shell" {
		t.Fatalf("PTY=%+v, requests=%s/%s", dimensions, pty.Type, shell.Type)
	}
	readExactly(t, stream, "ready> ")
	if n, err := stream.Write([]byte("uname -a\n")); n != 9 || err != nil {
		t.Fatalf("Write = %d, %v", n, err)
	}
	readExactly(t, stream, "echo: uname -a\n")
}

// TestNewValidation rejects invalid connection settings before any network I/O.
func TestNewValidation(t *testing.T) {
	_, identity := testIdentity(t)
	valid := Config{User: "user", Host: "host.example", PrivateKeyPath: identity, KnownHostsPath: testKnownHosts(t, "host.example:22", testSigner(t).PublicKey())}
	for _, tc := range []struct {
		name   string
		change func(*Config)
	}{
		{"user", func(c *Config) { c.User = "" }},
		{"host", func(c *Config) { c.Host = "" }},
		{"host-port", func(c *Config) { c.Host = "host:22" }},
		{"port", func(c *Config) { c.Port = 65536 }},
		{"key", func(c *Config) { c.PrivateKeyPath = "" }},
		{"host-key", func(c *Config) { c.KnownHostsPath = "" }},
		{"timeout", func(c *Config) { c.Timeout = -1 }},
		{"columns", func(c *Config) { c.Columns = -1 }},
		{"rows", func(c *Config) { c.Rows = 65536 }},
		{"term", func(c *Config) { c.Term = "xterm\n" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			configuration := valid
			tc.change(&configuration)
			if _, err := New(configuration); err == nil {
				t.Fatal("invalid configuration accepted")
			}
		})
	}
	valid.Host = "::1"
	transport, err := New(valid)
	if err != nil || transport.Endpoint() != "user@[::1]:22" || transport.config.Columns != 80 || transport.config.Rows != 24 || transport.config.Timeout != 10*time.Second {
		t.Fatalf("defaults/IPv6 = %+v, %v", transport, err)
	}
}

// TestConnectRejectsAuthenticationHostKeyAndRequests covers failures before ownership can transfer to a Channel.
func TestConnectRejectsAuthenticationHostKeyAndRequests(t *testing.T) {
	for _, behavior := range []string{"authentication", "host-key", "reject-pty-req", "reject-shell"} {
		t.Run(behavior, func(t *testing.T) {
			server := startTestServer(t, behavior)
			if behavior == "authentication" {
				_, server.config.PrivateKeyPath = testIdentity(t)
			}
			if behavior == "host-key" {
				server.config.KnownHostsPath = testKnownHosts(t, net.JoinHostPort(server.config.Host, strconv.Itoa(server.config.Port)), testSigner(t).PublicKey())
			}
			transport, _ := New(server.config)
			if stream, err := transport.Connect(context.Background()); err == nil || stream != nil {
				if stream != nil {
					_ = stream.Close()
				}
				t.Fatalf("Connect = %v, %v", stream, err)
			}
		})
	}
}

// TestConnectCancellationAndDeadlineAtEveryStage ensures each blocking setup boundary releases the socket.
func TestConnectCancellationAndDeadlineAtEveryStage(t *testing.T) {
	for _, stage := range []string{"handshake", "open", "pty-req", "shell"} {
		for _, deadline := range []bool{false, true} {
			t.Run(stage+strconv.FormatBool(deadline), func(t *testing.T) {
				server := startTestServer(t, "stall-"+stage)
				if deadline {
					server.config.Timeout = 200 * time.Millisecond
				}
				transport, _ := New(server.config)
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				result := make(chan error, 1)
				go func() { _, err := transport.Connect(ctx); result <- err }()
				if !deadline {
					for reached := range server.seen {
						if reached == stage {
							break
						}
					}
					cancel()
				}
				want := context.Canceled
				if deadline {
					want = context.DeadlineExceeded
				}
				select {
				case err := <-result:
					if !errors.Is(err, want) {
						t.Fatalf("error = %v, want %v", err, want)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("Connect did not stop")
				}
			})
		}
	}
}

// TestConnectPrecancelAndDialFailure preserves context and dial error identities.
func TestConnectPrecancelAndDialFailure(t *testing.T) {
	server := startTestServer(t, "")
	transport, _ := New(server.config)
	dialError := errors.New("test dial error")
	calls := 0
	transport.dial = func(context.Context, string, string) (net.Conn, error) { calls++; return nil, dialError }
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := transport.Connect(ctx); !errors.Is(err, context.Canceled) || calls != 0 {
		t.Fatalf("precancel = %v, calls=%d", err, calls)
	}
	if _, err := transport.Connect(context.Background()); !errors.Is(err, dialError) {
		t.Fatalf("dial error = %v", err)
	}
}

// TestConnectedChannelOutlivesSetupAndIsIndependent protects the successful resource-ownership handoff.
func TestConnectedChannelOutlivesSetupAndIsIndependent(t *testing.T) {
	server := startTestServer(t, "")
	transport, _ := New(server.config)
	ctx, cancel := context.WithCancel(context.Background())
	first, err := transport.Connect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = first.Close() })
	cancel()
	second, err := transport.Connect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = second.Close() })
	readExactly(t, first, "ready> ")
	readExactly(t, second, "ready> ")
	if _, err := first.Write([]byte("alive\n")); err != nil {
		t.Fatal(err)
	}
	readExactly(t, first, "echo: alive\n")
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := second.Write([]byte("independent\n")); err != nil {
		t.Fatal(err)
	}
	readExactly(t, second, "echo: independent\n")
}

// TestCloseUnblocksReadAndFlowControlledWrite checks concurrent idempotent cleanup against blocked SSH I/O.
func TestCloseUnblocksReadAndFlowControlledWrite(t *testing.T) {
	server := startTestServer(t, "unread-input")
	stream := connectTestChannel(t, server.config)
	readExactly(t, stream, "ready> ")
	readDone, writeDone := make(chan error, 1), make(chan error, 1)
	go func() { _, err := stream.Read(make([]byte, 1)); readDone <- err }()
	go func() { _, err := stream.Write(bytes.Repeat([]byte("x"), 4*1024*1024)); writeDone <- err }()
	select {
	case err := <-writeDone:
		t.Fatalf("large write should block: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	closed := make(chan error, 4)
	for range 4 {
		go func() { closed <- stream.Close() }()
	}
	for range 4 {
		select {
		case err := <-closed:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("Close blocked")
		}
	}
	for _, done := range []chan error{readDone, writeDone} {
		select {
		case err := <-done:
			if err == nil {
				t.Fatal("blocked I/O succeeded after Close")
			}
		case <-time.After(time.Second):
			t.Fatal("I/O did not unblock")
		}
	}
	if stream.State() != channel.StateClosed {
		t.Fatalf("state=%v", stream.State())
	}
	if _, err := stream.Write([]byte("closed")); !errors.Is(err, channel.ErrNotOpen) {
		t.Fatal(err)
	}
	if _, err := stream.Read(make([]byte, 1)); !errors.Is(err, channel.ErrNotOpen) {
		t.Fatal(err)
	}
}

// TestStderrDrainedAndRemoteEOF protects extended-data draining and remote shell completion.
func TestStderrDrainedAndRemoteEOF(t *testing.T) {
	server := startTestServer(t, "stderr")
	stream := connectTestChannel(t, server.config)
	readExactly(t, stream, "ready> ")
	// SSH extended data has a separate stream; its bytes must all be drained,
	// but stdout and stderr do not have a cross-stream ordering guarantee.
	data := make([]byte, 3*1024*1024+4)
	if _, err := io.ReadFull(stream, data); err != nil {
		t.Fatal(err)
	}
	if bytes.Count(data, []byte("E")) != 3*1024*1024 || !bytes.Contains(data, []byte("end\n")) {
		t.Fatal("stdout or stderr was lost")
	}
	if _, err := stream.Write([]byte("exit\n")); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := stream.Read(make([]byte, 1)); done <- err }()
	select {
	case err := <-done:
		if !errors.Is(err, io.EOF) {
			t.Fatalf("remote exit=%v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("remote exit did not terminate Read")
	}
}

// TestPublicAPIDoesNotExposeBackendTypes protects the replaceable SSH-library
// boundary, including nested exported fields and constructor/method signatures.
func TestPublicAPIDoesNotExposeBackendTypes(t *testing.T) {
	seen := make(map[reflect.Type]bool)
	var inspect func(reflect.Type)
	inspect = func(typ reflect.Type) {
		if seen[typ] {
			return
		}
		seen[typ] = true
		if strings.HasPrefix(typ.PkgPath(), "golang.org/x/crypto/ssh") {
			t.Errorf("SSH backend type escaped through public API: %v", typ)
		}
		for i := 0; i < typ.NumMethod(); i++ {
			inspect(typ.Method(i).Type)
		}
		switch typ.Kind() {
		case reflect.Pointer, reflect.Slice, reflect.Array, reflect.Chan:
			inspect(typ.Elem())
		case reflect.Map:
			inspect(typ.Key())
			inspect(typ.Elem())
		case reflect.Struct:
			for i := 0; i < typ.NumField(); i++ {
				field := typ.Field(i)
				if field.IsExported() {
					inspect(field.Type)
				}
			}
		case reflect.Func:
			for i := 0; i < typ.NumIn(); i++ {
				inspect(typ.In(i))
			}
			for i := 0; i < typ.NumOut(); i++ {
				inspect(typ.Out(i))
			}
		}
	}
	inspect(reflect.TypeOf(Config{}))
	inspect(reflect.TypeOf(New))
}

// TestNewRejectsCredentialFileErrors checks that backend-private parsing keeps
// missing-file error identities and rejects malformed credentials before dialing.
func TestNewRejectsCredentialFileErrors(t *testing.T) {
	_, identity := testIdentity(t)
	valid := Config{User: "user", Host: "host.example", PrivateKeyPath: identity, KnownHostsPath: testKnownHosts(t, "host.example:22", testSigner(t).PublicKey())}
	bad := filepath.Join(t.TempDir(), "invalid")
	if err := os.WriteFile(bad, []byte("not a credential\n"), 0600); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(t.TempDir(), "missing")
	for _, tc := range []struct {
		name    string
		change  func(*Config)
		message string
		missing bool
	}{
		{"missing-key", func(c *Config) { c.PrivateKeyPath = missing }, "read SSH identity", true},
		{"invalid-key", func(c *Config) { c.PrivateKeyPath = bad }, "parse SSH identity", false},
		{"missing-known-hosts", func(c *Config) { c.KnownHostsPath = missing }, "load SSH known_hosts", true},
		{"invalid-known-hosts", func(c *Config) { c.KnownHostsPath = bad }, "load SSH known_hosts", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			configuration := valid
			tc.change(&configuration)
			transport, err := New(configuration)
			if transport != nil || err == nil || !strings.Contains(err.Error(), tc.message) {
				t.Fatalf("New = %v, %v", transport, err)
			}
			if tc.missing && !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("missing-file error identity lost: %v", err)
			}
		})
	}
}

// TestTransportOwnsCredentialSnapshot verifies that callers supply paths once,
// while an established Transport owns the parsed credentials independently.
func TestTransportOwnsCredentialSnapshot(t *testing.T) {
	server := startTestServer(t, "")
	transport, err := New(server.config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(server.config.PrivateKeyPath); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(server.config.KnownHostsPath, []byte("invalid replacement\n"), 0600); err != nil {
		t.Fatal(err)
	}
	stream, err := transport.Connect(context.Background())
	if err != nil {
		t.Fatalf("caller file changes affected the owned credential snapshot: %v", err)
	}
	defer stream.Close()
	readExactly(t, stream, "ready> ")
	if _, err := New(server.config); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a new Transport must reload credentials: %v", err)
	}
}
