// Package ssh establishes private-key-authenticated SSH PTY shell Channels.
// Session, rather than Transport, owns output history and shared access.
package ssh

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/core/channel"
	"github.com/akira-init1/ChannelTerm/internal/core/transport"
	gossh "golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// Config describes one SSH shell connection using library-independent values.
// PrivateKeyPath identifies an unencrypted private key; KnownHostsPath identifies
// the trusted OpenSSH host-key database. Both paths are required. Zero Port,
// Timeout, Term, Columns, and Rows select 22, 10 seconds, xterm-256color, 80, and
// 24 respectively. Configuration is not persisted by this package.
type Config struct {
	User           string
	Host           string
	Port           int
	PrivateKeyPath string
	KnownHostsPath string
	Timeout        time.Duration
	Term           string
	Columns        int
	Rows           int
}

// Transport retains validated connection settings, never a live stream.
// Each successful Connect transfers an independent SSH connection to a Channel.
type Transport struct {
	config Config
	dial   func(context.Context, string, string) (net.Conn, error)

	// Backend-specific authentication stays private to this implementation.
	signer        gossh.Signer
	verifyHostKey gossh.HostKeyCallback
}

var _ transport.Transport = (*Transport)(nil)

// New validates config and loads authentication and host trust without opening
// a network connection. It retains a private snapshot of the parsed credentials;
// callers exchange only paths and settings, never backend-specific objects.
// Changes to either file require constructing a new Transport.
func New(config Config) (*Transport, error) {
	if config.User == "" || strings.ContainsAny(config.User, "@ \t\r\n\x00") {
		return nil, errors.New("SSH username must be nonempty and contain no whitespace or @")
	}
	if config.Host == "" || strings.ContainsAny(config.Host, "@/[] \t\r\n\x00") {
		return nil, errors.New("SSH host must be a hostname or an unbracketed IP address")
	}
	if strings.Contains(config.Host, ":") && net.ParseIP(config.Host) == nil {
		return nil, errors.New("SSH host must not include a port; set Port separately")
	}
	if config.Port == 0 {
		config.Port = 22
	}
	if config.Port < 1 || config.Port > 65535 {
		return nil, errors.New("SSH port must be between 1 and 65535")
	}
	if config.PrivateKeyPath == "" || config.KnownHostsPath == "" {
		return nil, errors.New("SSH requires private-key and known_hosts paths")
	}
	if config.Timeout == 0 {
		config.Timeout = 10 * time.Second
	}
	if config.Timeout < 0 {
		return nil, errors.New("SSH connection timeout must be positive")
	}
	if config.Term == "" {
		config.Term = "xterm-256color"
	}
	if strings.ContainsAny(config.Term, "\x00\r\n") {
		return nil, errors.New("SSH terminal type must not contain control characters")
	}
	if config.Columns == 0 {
		config.Columns = 80
	}
	if config.Rows == 0 {
		config.Rows = 24
	}
	if config.Columns < 1 || config.Columns > 65535 || config.Rows < 1 || config.Rows > 65535 {
		return nil, errors.New("SSH PTY dimensions must be between 1 and 65535")
	}
	key, err := os.ReadFile(config.PrivateKeyPath)
	if err != nil {
		return nil, fmt.Errorf("read SSH identity: %w", err)
	}
	signer, err := gossh.ParsePrivateKey(key)
	clear(key)
	if err != nil {
		return nil, fmt.Errorf("parse SSH identity (an unencrypted private key is required): %w", err)
	}
	verifyHostKey, err := knownhosts.New(config.KnownHostsPath)
	if err != nil {
		return nil, fmt.Errorf("load SSH known_hosts: %w", err)
	}
	return &Transport{
		config: config, dial: (&net.Dialer{}).DialContext,
		signer: signer, verifyHostKey: verifyHostKey,
	}, nil
}

// Endpoint returns the validated user@host:port Session-reuse key. IPv6 hosts
// are bracketed, and credentials are never included.
func (t *Transport) Endpoint() string {
	return t.config.User + "@" + net.JoinHostPort(t.config.Host, strconv.Itoa(t.config.Port))
}

// Connect authenticates, requests a PTY, and starts an interactive shell.
// ctx and Config.Timeout bound the entire setup, including handshake and SSH
// requests. Cancellation releases every partial resource. After success, ctx
// no longer controls the Channel: only Channel.Close owns its lifetime.
func (t *Transport) Connect(ctx context.Context) (_ channel.Channel, err error) {
	setup, cancel := context.WithTimeout(ctx, t.config.Timeout)
	defer cancel()
	if err := setup.Err(); err != nil {
		return nil, err
	}
	address := net.JoinHostPort(t.config.Host, strconv.Itoa(t.config.Port))
	conn, err := t.dial(setup, "tcp", address)
	if err != nil {
		return nil, fmt.Errorf("dial SSH %s: %w", address, err)
	}
	// Closing the socket interrupts handshake, channel opening, PTY, and shell
	// requests, none of which accept a context in x/crypto/ssh. Join the callback
	// before handing ownership off so late cancellation cannot close a Channel.
	cancelDone := make(chan struct{})
	stop := context.AfterFunc(setup, func() {
		_ = conn.Close()
		close(cancelDone)
	})
	stopped := false
	defer func() {
		if !stopped && !stop() {
			<-cancelDone
		}
		if err != nil {
			_ = conn.Close()
			if setup.Err() != nil {
				err = errors.Join(setup.Err(), err)
			}
		}
	}()
	sshConn, channels, requests, err := gossh.NewClientConn(conn, address, &gossh.ClientConfig{
		User: t.config.User, Auth: []gossh.AuthMethod{gossh.PublicKeys(t.signer)},
		HostKeyCallback: t.verifyHostKey,
	})
	if err != nil {
		return nil, fmt.Errorf("SSH handshake: %w", err)
	}
	client := gossh.NewClient(sshConn, channels, requests)
	shell, err := client.NewSession()
	if err != nil {
		return nil, fmt.Errorf("open SSH shell channel: %w", err)
	}
	stdin, err := shell.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("open SSH shell input: %w", err)
	}
	reader, writer := io.Pipe()
	shell.Stdout, shell.Stderr = writer, writer
	defer func() {
		if err != nil {
			_ = reader.Close()
			_ = writer.Close()
		}
	}()
	if err := shell.RequestPty(t.config.Term, t.config.Rows, t.config.Columns, gossh.TerminalModes{
		gossh.ECHO: 1, gossh.TTY_OP_ISPEED: 38400, gossh.TTY_OP_OSPEED: 38400,
	}); err != nil {
		return nil, fmt.Errorf("request SSH PTY: %w", err)
	}
	if err := shell.Shell(); err != nil {
		return nil, fmt.Errorf("start SSH shell: %w", err)
	}
	stream := &shellStream{reader: reader, writer: writer, stdin: stdin, client: client, done: make(chan struct{})}
	go func() {
		defer close(stream.done)
		_ = writer.CloseWithError(shell.Wait())
	}()
	if !stop() {
		<-cancelDone
	}
	stopped = true
	if err := setup.Err(); err != nil {
		_ = stream.Close()
		return nil, err
	}
	return channel.NewStream(stream)
}

// shellStream owns the SSH client and the two output-copy workers managed by
// ssh.Session. The pipe merges stdout/stderr without retaining terminal history.
// channel.Stream supplies lifecycle state and exactly-once Close around it.
type shellStream struct {
	reader *io.PipeReader
	writer *io.PipeWriter
	stdin  io.WriteCloser
	client *gossh.Client
	done   chan struct{}
}

// Read consumes merged remote output without retaining a history copy.
func (s *shellStream) Read(p []byte) (int, error) { return s.reader.Read(p) }

// Write sends raw shell input; Session supplies write serialization above it.
func (s *shellStream) Write(p []byte) (int, error) { return s.stdin.Write(p) }

// Close breaks both local pipe backpressure and remote SSH flow control before
// joining the copy workers. It cannot wait for a cooperative remote shell.
func (s *shellStream) Close() error {
	_ = s.reader.Close()
	_ = s.writer.Close()
	err := s.client.Close()
	<-s.done
	if errors.Is(err, net.ErrClosed) || errors.Is(err, io.EOF) {
		return nil
	}
	return err
}
