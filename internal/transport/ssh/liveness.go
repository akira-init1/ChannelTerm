package ssh

import (
	"errors"
	"sync"
	"time"

	gossh "golang.org/x/crypto/ssh"
)

// ErrKeepAliveTimeout reports a peer that did not answer an SSH liveness probe.
var ErrKeepAliveTimeout = errors.New("SSH keepalive response timed out")

// ErrWriteTimeout reports shell input or resizing that exceeded WriteTimeout.
// The connection is closed because some bytes may already have been delivered.
var ErrWriteTimeout = errors.New("SSH write timed out; connection closed")

// liveClient owns the probe worker as well as the backend connection. The worker
// waits for setup ownership to transfer before applying the liveness policy.
// Shutdown closes the socket before joining workers, including a blocked probe.
type liveClient struct {
	*gossh.Client
	ready, stopped, done chan struct{}
	once                 sync.Once
	mu                   sync.Mutex
	cause                error
	closeErr             error
}

func newLiveClient(client *gossh.Client, config Config) *liveClient {
	c := &liveClient{Client: client, ready: make(chan struct{}), stopped: make(chan struct{}), done: make(chan struct{})}
	go c.keepAlive(config)
	return c
}

func (c *liveClient) shutdown(cause error) {
	c.once.Do(func() {
		c.mu.Lock()
		c.cause = cause
		c.mu.Unlock()
		close(c.stopped)
		c.closeErr = c.Client.Close()
	})
}

func (c *liveClient) failure() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cause
}

// Close interrupts probes and network I/O, then joins the liveness worker.
func (c *liveClient) Close() error {
	c.shutdown(nil)
	<-c.done
	return c.closeErr
}

func (c *liveClient) keepAlive(config Config) {
	defer close(c.done)
	if config.KeepAliveInterval == 0 {
		return
	}
	select {
	case <-c.ready:
	case <-c.stopped:
		return
	}
	ticker := time.NewTicker(config.KeepAliveInterval)
	defer ticker.Stop()
	for {
		select {
		case <-c.stopped:
			return
		case <-ticker.C:
		}
		// SSH global replies have no request IDs. Keep only one outstanding
		// probe instead of accumulating goroutines when a server stops replying.
		reply := make(chan error, 1)
		go func() {
			_, _, err := c.SendRequest("keepalive@openssh.com", true, nil)
			// A negative reply still proves that the authenticated peer is alive.
			reply <- err
		}()
		timer := time.NewTimer(config.KeepAliveInterval * time.Duration(config.KeepAliveCountMax))
		select {
		case err := <-reply:
			timer.Stop()
			if err != nil {
				c.shutdown(nil)
				return
			}
		case <-timer.C:
			c.shutdown(ErrKeepAliveTimeout)
			<-reply
			return
		case <-c.stopped:
			timer.Stop()
			<-reply
			return
		}
	}
}

// boundWrite closes the connection on timeout, also interrupting SSH channel
// flow-control waits that socket write deadlines alone cannot interrupt. The
// timer callback is joined before returning so it cannot close a later write.
func (c *liveClient) boundWrite(timeout time.Duration, write func() error) error {
	if timeout == 0 {
		return write()
	}
	done := make(chan struct{})
	timer := time.AfterFunc(timeout, func() { c.shutdown(ErrWriteTimeout); close(done) })
	err := write()
	if !timer.Stop() {
		<-done
	}
	return errors.Join(err, c.failure())
}
