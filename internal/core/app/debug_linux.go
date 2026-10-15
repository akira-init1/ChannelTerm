package app

import (
	"context"
	"debug/elf"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/core/debugger"
	"github.com/akira-init1/ChannelTerm/internal/core/session"
	"golang.org/x/sys/unix"
)

// debugProcess holds a pidfd, so cleanup cannot signal a reused numeric PID.
// SIGKILL of gdbserver drops ptrace ownership and lets an attached process
// survive. Normal completion always uses the RSP detach/exit handshake first.
type debugProcess struct{ fd int }

func (p *debugProcess) close() {
	_ = unix.PidfdSendSignal(p.fd, unix.SIGKILL, nil, 0)
	_ = unix.Close(p.fd)
}

func validateDebugStart(q *DebugStartRequest) error {
	if !filepath.IsAbs(q.ELF) {
		return errors.New("elf must be an absolute local path")
	}
	f, e := elf.Open(q.ELF)
	if e != nil {
		return fmt.Errorf("open ELF: %w", e)
	}
	defer f.Close()
	want := map[string]elf.Machine{"amd64": elf.EM_X86_64, "arm64": elf.EM_AARCH64}[runtime.GOARCH]
	if want == 0 || f.Machine != want || f.Class != elf.ELFCLASS64 || f.Data != elf.ELFDATA2LSB {
		return fmt.Errorf("ELF architecture mismatch: %s; this backend requires native Linux %s", f.Machine, runtime.GOARCH)
	}
	if f.Type != elf.ET_EXEC && f.Type != elf.ET_DYN {
		return errors.New("ELF must be an executable or PIE")
	}
	for _, p := range []*string{&q.GDB, &q.GDBServer} {
		if *p == "" {
			name := "gdb"
			if p == &q.GDBServer {
				name = "gdbserver"
			}
			*p, e = exec.LookPath(name)
			if e != nil {
				return e
			}
		}
		if !filepath.IsAbs(*p) {
			return errors.New("gdb and gdbserver must be absolute local executable paths")
		}
		info, e := os.Stat(*p)
		if e != nil {
			return e
		}
		if !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
			return errors.New("debugger path is not executable")
		}
	}
	if q.PID < 0 || len(q.Args) > 128 {
		return errors.New("invalid PID or too many inferior arguments")
	}
	size := 0
	for _, arg := range append([]string{q.ELF, q.GDB, q.GDBServer}, q.Args...) {
		size += len(arg)
		if strings.ContainsAny(arg, "\x00\r\n") {
			return errors.New("debug paths and arguments must not contain NUL or newline")
		}
	}
	if size > 8192 {
		return errors.New("inferior command exceeds 8192 bytes")
	}
	if q.PID > 0 {
		if len(q.Args) > 0 {
			return errors.New("attach does not accept args")
		}
		return validateDebugAttach(q.PID, q.ELF)
	}
	return nil
}

func validateDebugAttach(pid int, path string) error {
	if pid <= 1 || pid == os.Getpid() {
		return errors.New("unsafe attach PID")
	}
	base := "/proc/" + strconv.Itoa(pid)
	stat, e := os.ReadFile(base + "/stat")
	if e != nil {
		return e
	}
	i := strings.LastIndexByte(string(stat), ')')
	if i < 0 {
		return errors.New("invalid process stat")
	}
	fields := strings.Fields(string(stat)[i+1:])
	if len(fields) < 20 || fields[4] != "0" {
		return errors.New("attach refused: process has a controlling terminal")
	}
	actual, e := os.Stat(base + "/exe")
	if e != nil {
		return e
	}
	expected, e := os.Stat(path)
	if e != nil {
		return e
	}
	if !os.SameFile(actual, expected) {
		return errors.New("attach ELF does not match /proc/PID/exe")
	}
	// Check every descriptor, not only stdin/stdout/stderr. A target retaining a
	// terminal descriptor can corrupt RSP even when its standard streams differ.
	entries, e := os.ReadDir(base + "/fd")
	if e != nil {
		return e
	}
	for _, entry := range entries {
		info, e := os.Stat(base + "/fd/" + entry.Name())
		if e != nil {
			return fmt.Errorf("cannot prove attach I/O isolation: %w", e)
		}
		if info.Mode()&(os.ModeNamedPipe|os.ModeSocket) != 0 {
			return errors.New("attach refused: pipe/socket I/O cannot be proven isolated from the Session")
		}
		if info.Mode()&os.ModeCharDevice != 0 {
			target, e := os.Readlink(base + "/fd/" + entry.Name())
			if e != nil {
				return e
			}
			switch target {
			case "/dev/null", "/dev/zero", "/dev/random", "/dev/urandom":
			default:
				return errors.New("attach refused: process has a potentially shared character-device descriptor")
			}
		}
	}
	return nil
}

func (r *debugRun) start(ctx context.Context) error {
	var e error
	r.dir, e = os.MkdirTemp("", "channelterm-debug-")
	if e != nil {
		return e
	}
	if e = r.heartbeat(); e != nil {
		return e
	}
	if e = os.WriteFile(filepath.Join(r.dir, "identity"), []byte(r.token), 0600); e != nil {
		return e
	}
	if e = os.WriteFile(filepath.Join(r.dir, "inferior-wrapper"), []byte("#!/bin/sh\nexec \"$@\" </dev/null >/dev/null 2>&1\n"), 0700); e != nil {
		return e
	}
	socket := filepath.Join(r.dir, "rsp.sock")
	r.listener, e = net.Listen("unix", socket)
	if e != nil {
		return e
	}
	if e = os.Chmod(socket, 0600); e != nil {
		return e
	}
	r.client, e = debugger.Start(r.request.GDB, r.record)
	if e != nil {
		return e
	}
	for _, command := range []string{"-gdb-set pagination off", "-gdb-set confirm off", "-gdb-set mi-async on", "-gdb-set non-stop off", "-gdb-set detach-on-fork on", "-gdb-set follow-fork-mode parent", "-gdb-set may-call-functions off", "-gdb-set remote noack-packet off", "-gdb-set sysroot /", "-file-exec-and-symbols " + debugger.Quote(r.request.ELF)} {
		if _, e = r.client.Command(ctx, command); e != nil {
			return e
		}
	}
	r.protocol = &terminalCommandProtocol{terminal: r.terminal, cursor: sessionOutputCursor(r.terminal), token: r.token}
	bootstrap := r.bootstrap()
	if len(bootstrap)+1 > 4095 {
		return errors.New("debug bootstrap exceeds Linux canonical input limit; shorten paths or arguments")
	}
	r.touched = true
	if e = r.write(ctx, []byte(bootstrap+"\n")); e != nil {
		return e
	}
	cursor, e := r.protocol.waitStart(ctx)
	if e != nil {
		return e
	}
	if e = r.openServer(ctx); e != nil {
		return e
	}
	bridgeCtx, cancel := context.WithCancel(context.Background())
	r.bridgeCancel = cancel
	r.bridgeDone = make(chan struct{})
	r.bridgeStarted = true
	go func() {
		conn, e := r.listener.Accept()
		if e == nil {
			e = authenticateDebugPeer(conn, r.client.PID())
			if e == nil {
				var ack debugger.RelayResult
				ack, e = debugger.RelayFinalACK(bridgeCtx, conn, &debugSessionStream{run: r, cursor: cursor})
				r.mu.Lock()
				r.bridgeFinalACK = ack.ExitACK
				r.bridgeLastACK = ack.LastACK
				r.mu.Unlock()
			}
			_ = conn.Close()
		}
		r.mu.Lock()
		r.bridgeErr = e
		r.mu.Unlock()
		close(r.bridgeDone)
	}()
	if _, e = r.client.Command(ctx, "-target-select remote "+socket); e != nil {
		return e
	}
	// Recheck after all threads have stopped, closing the descriptor-change
	// window between preflight and ptrace attachment.
	if r.request.PID > 0 {
		return validateDebugAttach(r.request.PID, r.request.ELF)
	}
	return nil
}

func authenticateDebugPeer(conn net.Conn, pid int) error {
	local, ok := conn.(*net.UnixConn)
	if !ok {
		return errors.New("RSP peer is not local")
	}
	raw, e := local.SyscallConn()
	if e != nil {
		return e
	}
	var peer *unix.Ucred
	var credentialErr error
	if e = raw.Control(func(fd uintptr) {
		peer, credentialErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	}); e != nil {
		return e
	}
	if credentialErr != nil {
		return credentialErr
	}
	if int(peer.Pid) != pid || int(peer.Uid) != os.Getuid() {
		return errors.New("unauthorized RSP peer")
	}
	return nil
}

func processStartTime(pid int) (string, error) {
	data, e := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if e != nil {
		return "", e
	}
	i := strings.LastIndexByte(string(data), ')')
	if i < 0 {
		return "", errors.New("invalid process stat")
	}
	fields := strings.Fields(string(data)[i+1:])
	if len(fields) < 20 {
		return "", errors.New("short process stat")
	}
	return fields[19], nil
}
func (r *debugRun) heartbeat() error {
	path := filepath.Join(r.dir, "heartbeat")
	if e := os.WriteFile(path+".new", []byte(strconv.FormatInt(time.Now().Unix(), 10)), 0600); e != nil {
		return e
	}
	return os.Rename(path+".new", path)
}
func (r *debugRun) openServer(ctx context.Context) error {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		data, e := os.ReadFile(filepath.Join(r.dir, "server.pid"))
		if e == nil {
			fields := strings.Fields(string(data))
			if len(fields) == 2 {
				pid, e := strconv.Atoi(fields[0])
				actualStart, se := processStartTime(pid)
				if e == nil && se == nil && pid > 1 && actualStart == fields[1] {
					if r.server == nil {
						fd, e := unix.PidfdOpen(pid, 0)
						if e == nil {
							again, e := processStartTime(pid)
							if e == nil && again == fields[1] {
								r.server = &debugProcess{fd: fd}
							} else {
								unix.Close(fd)
							}
						}
					}
					actual, ae := os.Stat(fmt.Sprintf("/proc/%d/exe", pid))
					expected, ee := os.Stat(r.request.GDBServer)
					if r.server != nil && ae == nil && ee == nil && os.SameFile(actual, expected) {
						return nil
					}
				}
			}
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("gdbserver did not start in this Linux environment: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

func (r *debugRun) bootstrap() string {
	q := quoteTerminalCommand
	command := "env SHELL=/bin/sh " + q(r.request.GDBServer) + " --once "
	if r.request.PID > 0 {
		command += "--attach stdio " + strconv.Itoa(r.request.PID)
	} else {
		// setsid removes the inferior's controlling terminal; explicit redirections
		// prevent inherited stdio from consuming or producing RSP bytes.
		command += "--wrapper " + q(filepath.Join(r.dir, "inferior-wrapper")) + " -- stdio " + q(r.request.ELF)
		for _, arg := range r.request.Args {
			command += " " + q(arg)
		}
	}
	hostStart, _ := processStartTime(os.Getpid())
	statStart := `awk '{sub(/^.*\) /, "");print $20}' `
	// This detached watchdog has no terminal descriptors. It handles Host death
	// and a wedged renewal loop without trusting a reusable numeric PID alone.
	watchdog := "while [ \"$(" + statStart + "/proc/$$/stat 2>/dev/null)\" = \"$ct_birth\" ];do sleep 2;ct_beat=$(cat " + q(filepath.Join(r.dir, "heartbeat")) + " 2>/dev/null);case $ct_beat in ''|*[!0-9]*) ct_beat=0;;esac;if [ \"$(" + statStart + "/proc/" + strconv.Itoa(os.Getpid()) + "/stat 2>/dev/null)\" != " + q(hostStart) + " ] || [ $(( $(date +%s) - ct_beat )) -ge 45 ];then [ \"$(" + statStart + "/proc/$$/stat 2>/dev/null)\" != \"$ct_birth\" ] || kill -KILL $$;exit;fi;done"
	server := "umask 077;ct_birth=$(" + statStart + "/proc/$$/stat)||exit;printf '%s %s' \"$$\" \"$ct_birth\" > " + q(filepath.Join(r.dir, "server.pid")) + "||exit;(" + watchdog + ") </dev/null >/dev/null 2>&1 & exec " + command + " 2>" + q(filepath.Join(r.dir, "server.log"))
	token := quoteSplitTerminalCommandToken(r.token)
	return "if [ -n \"$BASH_VERSION\" ];then " + bashDeleteCurrentHistoryEntry + ";(ct_tty=$(stty -g)||exit;trap 'stty \"$ct_tty\"' 0;[ \"$(cat " + q(filepath.Join(r.dir, "identity")) + ")\" = " + token + " ]||exit;stty raw -echo||exit;printf '\\036\\037%s\\037\\036' " + token + ";/usr/bin/setsid --wait /bin/sh -c " + q(server) + ";ct_rc=$?;stty \"$ct_tty\"||exit;trap - 0;printf '\\035\\034%s:%s\\034\\035' " + token + " \"$ct_rc\");else printf '\\035\\034%s:UNSUPPORTED\\034\\035' " + token + ";fi"
}

type debugSessionStream struct {
	run    *debugRun
	cursor session.OutputCursor
}

func (s *debugSessionStream) Read(ctx context.Context) ([]byte, bool, error) {
	chunk, e := s.run.terminal.ReadOutput(ctx, s.cursor, 32*1024)
	s.cursor = chunk.Next
	return chunk.Data, chunk.Dropped, e
}
func (s *debugSessionStream) Write(ctx context.Context, b []byte) error { return s.run.write(ctx, b) }

func (r *debugRun) stop(terminate bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s := r.status()
	if s.State == "running" {
		if _, e := r.client.Command(ctx, "-exec-interrupt"); e != nil {
			r.fail(e)
			return e
		}
		if e := r.waitStopped(ctx); e != nil {
			r.fail(e)
			return e
		}
	}
	if r.status().State != "exited" {
		command := "-target-detach"
		if terminate {
			command = `-interpreter-exec console "kill"`
		}
		if _, e := r.client.Command(ctx, command); e != nil {
			r.fail(e)
			return e
		}
	}
	if r.status().State == "exited" || terminate {
		select {
		case <-r.bridgeDone:
		case <-ctx.Done():
			r.fail(ctx.Err())
			return ctx.Err()
		}
		r.mu.Lock()
		ack, bridgeErr := r.bridgeFinalACK, r.bridgeErr
		if terminate {
			ack = r.bridgeLastACK
		}
		r.mu.Unlock()
		if !ack || bridgeErr != nil {
			e := fmt.Errorf("final RSP exit ACK was not confirmed: %v", bridgeErr)
			r.fail(e)
			return e
		}
		// gdbserver stdio can wait forever for EOF after W/X. The inferior has
		// exited and its final packet is acknowledged, so only this pidfd-owned
		// server may now be stopped. Its wrapper still must restore the TTY.
		if r.server != nil {
			_ = unix.PidfdSendSignal(r.server.fd, unix.SIGKILL, nil, 0)
		}
	}
	r.transition("recovering", nil)
	// Keep both bridge directions alive while gdbserver consumes the last ACK.
	// Its shell footer is independent evidence of protocol termination. A socket
	// EOF or a quiet interval alone is never accepted as successful restoration.
	_, _, _, e := r.protocol.waitEnd(ctx)
	if e != nil {
		e = fmt.Errorf("gdbserver footer: %w", e)
	}
	if e == nil {
		if r.bridgeCancel != nil {
			r.bridgeCancel()
		}
		if r.listener != nil {
			_ = r.listener.Close()
		}
		if r.bridgeStarted {
			select {
			case <-r.bridgeDone:
				r.mu.Lock()
				bridgeErr := r.bridgeErr
				r.mu.Unlock()
				r.bridgeStarted = false
				if bridgeErr != nil && !errors.Is(bridgeErr, context.Canceled) && !errors.Is(bridgeErr, net.ErrClosed) && !errors.Is(bridgeErr, io.EOF) {
					e = bridgeErr
				}
			case <-ctx.Done():
				e = ctx.Err()
			}
		}
	}
	if e == nil {
		e = r.probeShell(ctx)
		if e != nil {
			e = fmt.Errorf("shell probe: %w", e)
		}
	}
	if e != nil {
		r.fail(fmt.Errorf("debug recovery failed; Session closed: %w", e))
		return r.err
	}
	if e = r.app.releaseLease(r.terminal, r.owner); e != nil {
		r.fail(e)
		return e
	}
	r.transition("closed", nil)
	r.cleanup()
	close(r.finished)
	return nil
}
func (r *debugRun) waitStopped(ctx context.Context) error {
	for {
		r.mu.Lock()
		state := r.state
		changed := r.changed
		r.mu.Unlock()
		if state == "stopped" || state == "exited" {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-changed:
		}
	}
}
func (r *debugRun) probeShell(ctx context.Context) error {
	token, e := newTerminalCommandToken()
	if e != nil {
		return e
	}
	p := &terminalCommandProtocol{terminal: r.terminal, cursor: sessionOutputCursor(r.terminal), token: token}
	// printf is side-effect-free. Splitting the random token prevents input echo
	// from being mistaken for the probe's response, even with echo enabled.
	command := bashDeleteCurrentHistoryEntry + ";printf '\\036\\037%s\\037\\036' " + quoteSplitTerminalCommandToken(token) + "\n"
	if e = r.write(ctx, []byte(command)); e != nil {
		return e
	}
	_, e = p.waitStart(ctx)
	return e
}
