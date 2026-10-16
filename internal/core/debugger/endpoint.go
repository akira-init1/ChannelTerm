package debugger

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"io"
	"math/big"
	"net"
	"net/netip"
	"strconv"
	"sync"
	"time"
)

const bridgePreface = "ChannelTerm-RSP/1 "
const bridgeHandshakeTimeout = 3 * time.Second

// BridgeConnection is a private connection descriptor for an external RSP client.
// Token is a bearer capability; callers must not publish this object in events
// or logs. Fingerprint pins the ephemeral TLS certificate before Token is sent.
type BridgeConnection struct {
	Address     string `json:"address"`
	Fingerprint string `json:"fingerprint"`
	Token       string `json:"token"`
}

// Endpoint accepts exactly one authorized TLS connection over TCP. Rejected
// peers do not consume that connection. Close interrupts pending authentication;
// the owner of an accepted connection must close it separately.
type Endpoint struct {
	listener net.Listener
	config   *tls.Config
	peer     netip.Addr
	info     BridgeConnection
	mu       sync.Mutex
	pending  net.Conn
	closed   bool
}

// ListenBridge binds a specific numeric IP (default 127.0.0.1:0). Wildcard
// binding is forbidden. A non-loopback bind requires an exact allowed peer IP;
// all clients, including loopback clients, must authenticate over pinned TLS.
func ListenBridge(address, allowedPeer string) (*Endpoint, error) {
	if address == "" {
		address = "127.0.0.1:0"
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, errors.New("listen_address must be a numeric IP:port")
	}
	ip, err := netip.ParseAddr(host)
	n, portErr := strconv.Atoi(port)
	if err != nil || ip.IsUnspecified() || ip.IsMulticast() || ip.Zone() != "" || portErr != nil || n < 0 || n > 65535 {
		return nil, errors.New("listen_address requires a specific numeric IP and port 0..65535; wildcard binding is forbidden")
	}
	var peer netip.Addr
	if allowedPeer != "" {
		peer, err = netip.ParseAddr(allowedPeer)
		if err != nil || peer.IsUnspecified() || peer.IsMulticast() || peer.Zone() != "" {
			return nil, errors.New("allowed_peer must be one specific numeric IP")
		}
		peer = peer.Unmap()
	} else if !ip.IsLoopback() {
		return nil, errors.New("non-loopback listening requires allowed_peer; explicitly select the Windows/WSL interface and client IP")
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, err
	}
	template := &x509.Certificate{SerialNumber: serial, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(24 * time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, err
	}
	secret := make([]byte, 32)
	if _, err = rand.Read(secret); err != nil {
		return nil, err
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return nil, err
	}
	fingerprint := sha256.Sum256(der)
	return &Endpoint{listener: listener, peer: peer,
		config: &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}},
		info:   BridgeConnection{Address: listener.Addr().String(), Fingerprint: hex.EncodeToString(fingerprint[:]), Token: hex.EncodeToString(secret)},
	}, nil
}

// Connection returns the secret descriptor to the authorized operation owner.
func (e *Endpoint) Connection() BridgeConnection { return e.info }

// Accept authenticates bounded, sequential handshakes until one succeeds or
// ctx/Close ends the wait. Authentication bytes never reach the Session. The
// listener is closed before the authorized connection is returned; no reconnect
// or second debugger can take over this endpoint.
func (e *Endpoint) Accept(ctx context.Context) (net.Conn, error) {
	stop := context.AfterFunc(ctx, e.Close)
	defer stop()
	for {
		conn, err := e.listener.Accept()
		if err != nil {
			return nil, err
		}
		remote, ok := conn.RemoteAddr().(*net.TCPAddr)
		if !ok || (e.peer.IsValid() && remote.AddrPort().Addr().Unmap() != e.peer) || (!e.peer.IsValid() && !remote.AddrPort().Addr().IsLoopback()) {
			conn.Close()
			continue
		}
		e.mu.Lock()
		if e.closed {
			e.mu.Unlock()
			conn.Close()
			return nil, net.ErrClosed
		}
		e.pending = conn
		e.mu.Unlock()
		conn.SetDeadline(time.Now().Add(bridgeHandshakeTimeout))
		secure := tls.Server(conn, e.config)
		err = secure.HandshakeContext(ctx)
		if err == nil {
			preface := make([]byte, len(bridgePreface)+64+1)
			_, err = io.ReadFull(secure, preface)
			if err == nil && subtle.ConstantTimeCompare(preface, []byte(bridgePreface+e.info.Token+"\n")) != 1 {
				err = errors.New("unauthorized bridge peer")
			}
		}
		if err == nil {
			_, err = writeAll(secure, []byte("OK\n"))
		}
		e.mu.Lock()
		e.pending = nil
		closed := e.closed
		e.mu.Unlock()
		if err != nil || closed {
			conn.Close()
			if closed {
				return nil, net.ErrClosed
			}
			continue
		}
		e.Close()
		secure.SetDeadline(time.Time{})
		return secure, nil
	}
}

// Close is idempotent and releases the listener and any unauthenticated socket.
func (e *Endpoint) Close() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.closed = true
	e.listener.Close()
	if e.pending != nil {
		e.pending.Close()
	}
}

// DialBridge authenticates the server certificate before transmitting the
// bearer token. Cancellation bounds connection/authentication; after return,
// the caller owns the connection and its lifetime. No RSP bytes are generated.
func DialBridge(ctx context.Context, info BridgeConnection) (net.Conn, error) {
	pin, err := hex.DecodeString(info.Fingerprint)
	if err != nil || len(pin) != sha256.Size {
		return nil, errors.New("fingerprint must be a SHA-256 certificate fingerprint")
	}
	secret, err := hex.DecodeString(info.Token)
	if err != nil || len(secret) != 32 {
		return nil, errors.New("bridge token must contain 64 hexadecimal characters")
	}
	ctx, cancel := context.WithTimeout(ctx, bridgeHandshakeTimeout)
	defer cancel()
	dialer := tls.Dialer{Config: &tls.Config{MinVersion: tls.VersionTLS13,
		// The operation pins an ephemeral certificate, rather than trusting a CA
		// or DNS name. VerifyConnection is mandatory even on resumed handshakes.
		InsecureSkipVerify: true,
		VerifyConnection: func(state tls.ConnectionState) error {
			if len(state.PeerCertificates) != 1 {
				return errors.New("unexpected bridge certificate chain")
			}
			digest := sha256.Sum256(state.PeerCertificates[0].Raw)
			if subtle.ConstantTimeCompare(pin, digest[:]) != 1 {
				return errors.New("bridge certificate fingerprint mismatch")
			}
			return nil
		},
	}}
	conn, err := dialer.DialContext(ctx, "tcp", info.Address)
	if err != nil {
		return nil, err
	}
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	deadline, _ := ctx.Deadline()
	conn.SetDeadline(deadline)
	if _, err = writeAll(conn, []byte(bridgePreface+info.Token+"\n")); err == nil {
		var response [3]byte
		_, err = io.ReadFull(conn, response[:])
		if err == nil && string(response[:]) != "OK\n" {
			err = errors.New("bridge authentication rejected")
		}
	}
	if err != nil {
		conn.Close()
		return nil, err
	}
	if !stop() || ctx.Err() != nil {
		conn.Close()
		return nil, ctx.Err()
	}
	conn.SetDeadline(time.Time{})
	return conn, nil
}
