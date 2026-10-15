package debugger

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"sync"
	"time"
)

// Client owns one GDB process and its MI reader. Commands are serialized;
// asynchronous records are delivered independently of command responses.
type Client struct {
	cmd          *exec.Cmd
	in           io.WriteCloser
	out          io.ReadCloser
	mu           sync.Mutex
	sequence     uint64
	pending      chan Record
	pendingToken string
	failure      error
	done         chan struct{}
	waited       chan struct{}
	once         sync.Once
	commandGate  chan struct{}
	onRecord     func(Record)
}

// Start launches GDB with init files, auto-loaded code, and debuginfod disabled.
// onRecord must return promptly and must not call Command synchronously.
func Start(path string, onRecord func(Record)) (*Client, error) {
	cmd := exec.Command(path, "--nx", "--nh", "--quiet", "--interpreter=mi2", "-iex", "set auto-load off", "-iex", "set debuginfod enabled off")
	in, e := cmd.StdinPipe()
	if e != nil {
		return nil, e
	}
	out, e := cmd.StdoutPipe()
	if e != nil {
		in.Close()
		return nil, e
	}
	cmd.Stderr = io.Discard
	cmd.WaitDelay = 2 * time.Second
	c := &Client{cmd: cmd, in: in, out: out, done: make(chan struct{}), waited: make(chan struct{}), commandGate: make(chan struct{}, 1), onRecord: onRecord}
	if e = cmd.Start(); e != nil {
		in.Close()
		out.Close()
		return nil, e
	}
	go c.read(out)
	return c, nil
}

// PID returns the owned GDB process ID for local socket peer authentication.
func (c *Client) PID() int { return c.cmd.Process.Pid }

func (c *Client) fail(err error) {
	c.once.Do(func() { c.mu.Lock(); c.failure = err; c.mu.Unlock(); close(c.done) })
}
func (c *Client) read(out io.Reader) {
	defer close(c.waited)
	scanner := bufio.NewScanner(out)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	for scanner.Scan() {
		r, e := Parse(scanner.Text())
		if e != nil {
			c.fail(e)
			c.cmd.Process.Kill()
			break
		}
		if r.Kind == "prompt" {
			continue
		}
		if r.Kind == "^" {
			if r.Class == "running" && c.onRecord != nil {
				c.onRecord(r)
			}
			c.mu.Lock()
			if c.pending != nil && c.pendingToken == r.Token {
				c.pending <- r
			}
			c.mu.Unlock()
		} else if c.onRecord != nil {
			c.onRecord(r)
		}
	}
	e := scanner.Err()
	if e == nil {
		e = io.EOF
	}
	c.fail(e)
	_ = c.cmd.Wait()
}

// Command waits for its token-correlated response. Cancellation kills GDB:
// once a command may have executed, retrying it without a result is unsafe.
// Application owns target recovery and must not release its terminal lease on
// this error without independently proving shell restoration.
func (c *Client) Command(ctx context.Context, command string) (Record, error) {
	if err := ctx.Err(); err != nil {
		return Record{}, err
	}
	select {
	case c.commandGate <- struct{}{}:
	case <-ctx.Done():
		return Record{}, ctx.Err()
	}
	defer func() { <-c.commandGate }()
	c.mu.Lock()
	if c.failure != nil {
		e := c.failure
		c.mu.Unlock()
		return Record{}, e
	}
	c.sequence++
	token := strconv.FormatUint(c.sequence, 10)
	ch := make(chan Record, 1)
	c.pending = ch
	c.pendingToken = token
	c.mu.Unlock()
	defer func() { c.mu.Lock(); c.pending = nil; c.mu.Unlock() }()
	stopCancellation := context.AfterFunc(ctx, func() { c.stopProcess(); c.fail(ctx.Err()) })
	defer stopCancellation()
	if _, e := io.WriteString(c.in, token+command+"\n"); e != nil {
		return Record{}, e
	}
	select {
	case r := <-ch:
		if r.Class == "error" {
			return r, &CommandError{Record: r}
		}
		return r, nil
	case <-ctx.Done():
		c.cmd.Process.Kill()
		c.fail(ctx.Err())
		return Record{}, ctx.Err()
	case <-c.done:
		c.mu.Lock()
		e := c.failure
		c.mu.Unlock()
		return Record{}, e
	}
}

// CommandError is an authoritative GDB rejection rather than a lost response.
type CommandError struct{ Record Record }

// Error returns GDB's MI error message.
func (e *CommandError) Error() string {
	return fmt.Sprintf("GDB rejected command: %v", e.Record.Results["msg"])
}

// IsCommandError reports a completed MI error response that leaves correlation intact.
func IsCommandError(e error) bool { var v *CommandError; return errors.As(e, &v) }

// Done closes when GDB's MI stream fails or exits.
func (c *Client) Done() <-chan struct{} { return c.done }

// Close kills only the owned GDB process, closes stdin, and reaps it. Target
// detach/termination must already have been handled by Application.
func (c *Client) Close()       { c.stopProcess(); <-c.waited }
func (c *Client) stopProcess() { _ = c.cmd.Process.Kill(); _ = c.in.Close(); _ = c.out.Close() }
