package debugger

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

const (
	bridgeAlivePreface   = "ChannelTerm-RSP/2 "
	bridgeControlPreface = "ChannelTerm-HBT/1 "
	bridgeAliveInterval  = 10 * time.Second
	bridgeAliveTimeout   = 30 * time.Second
)

// TCP probes supplement the application control heartbeat, including detection
// of an idle half-open data socket. Control deadlines do not depend on the OS's
// TCP retransmission timeout and never depend on target RSP traffic.
func configureBridgeTCP(conn net.Conn) error {
	tcp, ok := conn.(*net.TCPConn)
	if !ok {
		return errors.New("bridge requires a TCP connection")
	}
	return tcp.SetKeepAliveConfig(net.KeepAliveConfig{Enable: true, Idle: 10 * time.Second, Interval: 5 * time.Second, Count: 3})
}

// ClientAlive reports recent challenge/response evidence from the helper's
// separately authenticated control socket. Merely retaining a data socket, or
// receiving RSP bytes, is not liveness evidence. Legacy clients return false.
func (e *Endpoint) ClientAlive() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return !e.closed && !e.dataEnded && e.control != nil && !e.lastAlive.IsZero() && time.Since(e.lastAlive) < e.aliveTimeout
}

// ClientError returns a control failure without disclosing authentication data.
func (e *Endpoint) ClientError() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.keepaliveErr
}

type endpointDataConn struct {
	net.Conn
	endpoint *Endpoint
}

func (c *endpointDataConn) Close() error {
	c.endpoint.stopClient(nil)
	return c.Conn.Close()
}

// stopClient ends liveness as soon as either socket ends. It deliberately keeps
// the listener/reservation: disconnect does not end the bridge or prove a shell.
func (e *Endpoint) stopClient(cause error) {
	e.mu.Lock()
	if e.dataEnded {
		e.mu.Unlock()
		return
	}
	e.dataEnded = true
	close(e.clientDone)
	e.keepaliveErr = cause
	data, control := e.data, e.control
	e.mu.Unlock()
	if data != nil {
		data.Close()
	}
	if control != nil {
		control.Close()
	}
}

// acceptControl admits at most one control socket after the data client has
// authenticated. It reuses the existing listener, pin, peer policy and bridge
// token. No Host or management capability is ever sent to the helper.
func (e *Endpoint) acceptControl() {
	for {
		conn, err := e.listener.Accept()
		if err != nil {
			return
		}
		e.mu.Lock()
		eligible := e.keepalive && !e.dataEnded && !e.closed && e.control == nil
		e.mu.Unlock()
		if !eligible {
			conn.Close()
			continue
		}
		secure, _, err := e.authenticate(context.Background(), conn, bridgeControlPreface)
		if err != nil {
			conn.Close()
			continue
		}
		e.mu.Lock()
		ended := e.closed || e.dataEnded
		if !ended {
			e.control = secure
		}
		e.mu.Unlock()
		if ended {
			secure.Close()
			continue
		}
		go e.heartbeat(secure)
	}
}

func (e *Endpoint) heartbeat(conn net.Conn) {
	defer e.stopClient(errors.New("bridge client heartbeat lost; target state remains unverified"))
	for {
		var challenge, response [16]byte
		if _, err := rand.Read(challenge[:]); err != nil {
			return
		}
		conn.SetDeadline(time.Now().Add(e.aliveTimeout))
		if _, err := writeAll(conn, challenge[:]); err != nil {
			return
		}
		if _, err := io.ReadFull(conn, response[:]); err != nil || !bytes.Equal(challenge[:], response[:]) {
			return
		}
		e.mu.Lock()
		if e.dataEnded || e.closed {
			e.mu.Unlock()
			return
		}
		e.lastAlive = time.Now()
		e.mu.Unlock()
		timer := time.NewTimer(bridgeAliveInterval)
		select {
		case <-e.clientDone:
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

// DialBridgeKeepAlive authenticates one data socket and one control socket on
// the same endpoint. It returns an unchanged RSP byte stream. Closing either
// socket terminates the pair; ctx bounds setup, and the caller owns the returned
// connection. A current Host is required; legacy data-only clients can still use
// DialBridge with explicit management renewal.
func DialBridgeKeepAlive(ctx context.Context, info BridgeConnection) (net.Conn, error) {
	data, err := dialBridge(ctx, info, bridgeAlivePreface)
	if err != nil {
		return nil, err
	}
	control, err := dialBridge(ctx, info, bridgeControlPreface)
	if err != nil {
		data.Close()
		return nil, fmt.Errorf("bridge control authentication failed (use matching Host/helper versions): %w", err)
	}
	pair := &bridgeClientPair{Conn: data, control: control}
	go func() {
		defer func() {
			control.Close()
			// The two TCP streams can deliver EOF in either order. Allow the
			// caller to drain already received RSP bytes before the data read
			// fails; immediate Close here can discard the final GDB response.
			// A dead data path is still bounded, independently of TCP timers.
			data.SetDeadline(time.Now().Add(time.Second))
		}()
		for {
			control.SetDeadline(time.Now().Add(bridgeAliveInterval + bridgeAliveTimeout))
			var challenge [16]byte
			if _, err := io.ReadFull(control, challenge[:]); err != nil {
				return
			}
			if _, err := writeAll(control, challenge[:]); err != nil {
				return
			}
		}
	}()
	return pair, nil
}

type bridgeClientPair struct {
	net.Conn
	control net.Conn
	once    sync.Once
}

func (c *bridgeClientPair) Close() error {
	var err error
	c.once.Do(func() { err = c.Conn.Close(); c.control.Close() })
	return err
}
