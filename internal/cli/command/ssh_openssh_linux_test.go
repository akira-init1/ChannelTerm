//go:build linux

package command

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

// These opt-in tests use a separately started local OpenSSH server and real
// Linux PTYs. Ordinary go test never reads personal SSH configuration or keys.
type openSSHTestConfig struct {
	User       string `json:"user"`
	Host       string `json:"host"`
	Port       int    `json:"port"`
	Identity   string `json:"identity"`
	KnownHosts string `json:"known_hosts"`
	Htop       string `json:"htop"`
}

func loadOpenSSHTestConfig(t *testing.T) openSSHTestConfig {
	t.Helper()
	path := os.Getenv("CHANNELTERM_TEST_OPENSSH_CONFIG")
	if path == "" {
		t.Skip("set CHANNELTERM_TEST_OPENSSH_CONFIG to opt into a dedicated localhost OpenSSH server")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var config openSSHTestConfig
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	ip := net.ParseIP(config.Host)
	if ip == nil || !ip.IsLoopback() || config.User == "" || config.Port < 1 || config.Identity == "" || config.KnownHosts == "" {
		t.Fatal("OpenSSH test config must specify a loopback host, user, port, identity, and known_hosts")
	}
	return config
}

func (c openSSHTestConfig) args() []string {
	return []string{"ssh", c.User + "@" + c.Host, "--port", strconv.Itoa(c.Port), "--identity", c.Identity,
		"--known-hosts", c.KnownHosts, "--listen", "127.0.0.1:0", "--highlight=false"}
}

type sshPTYProcess struct {
	t        *testing.T
	master   *os.File
	slave    *os.File
	original *term.State
	output   lockedBuffer
	command  *exec.Cmd
	done     chan struct{}
	copied   chan struct{}
	result   error
}

// startSSHPTYProcess gives the real CLI dispatcher a controlling terminal.
// Keeping the slave open in the parent lets the test verify termios restoration
// even after the child process exits; all owned descriptors are closed at cleanup.
func startSSHPTYProcess(t *testing.T, args []string) *sshPTYProcess {
	t.Helper()
	fd, err := unix.Open("/dev/ptmx", unix.O_RDWR|unix.O_NOCTTY|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if err != nil {
		t.Fatal(err)
	}
	master := os.NewFile(uintptr(fd), "ssh-test-ptmx")
	t.Cleanup(func() { _ = master.Close() })
	if err := unix.IoctlSetPointerInt(fd, unix.TIOCSPTLCK, 0); err != nil {
		t.Fatal(err)
	}
	number, err := unix.IoctlGetInt(fd, unix.TIOCGPTN)
	if err != nil {
		t.Fatal(err)
	}
	slave, err := os.OpenFile(fmt.Sprintf("/dev/pts/%d", number), os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = slave.Close() })
	if err := unix.IoctlSetWinsize(fd, unix.TIOCSWINSZ, &unix.Winsize{Col: 100, Row: 36}); err != nil {
		t.Fatal(err)
	}
	original, err := term.GetState(int(slave.Fd()))
	if err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	t.Cleanup(cancel)
	command := exec.CommandContext(ctx, executable, append([]string{"-test.run=^TestSSHCLIHelperProcess$", "--"}, args...)...)
	command.Env = append(os.Environ(), "CHANNELTERM_TEST_SSH_CLI_HELPER=1")
	command.Stdin, command.Stdout, command.Stderr = slave, slave, slave
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	p := &sshPTYProcess{t: t, master: master, slave: slave, original: original, command: command, done: make(chan struct{}), copied: make(chan struct{})}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	go func() { p.result = command.Wait(); close(p.done) }()
	go func() { _, _ = io.Copy(&p.output, master); close(p.copied) }()
	t.Cleanup(func() {
		_ = command.Process.Kill()
		<-p.done
		_ = master.Close()
		_ = slave.Close()
		<-p.copied
	})
	return p
}

func (p *sshPTYProcess) write(data string) {
	p.t.Helper()
	if _, err := io.WriteString(p.master, data); err != nil {
		p.t.Fatal(err)
	}
}

func (p *sshPTYProcess) wait(after int, expected string) string {
	p.t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		output := p.output.String()
		if strings.Contains(output[after:], expected) {
			return output
		}
		time.Sleep(10 * time.Millisecond)
	}
	p.t.Fatalf("PTY output did not contain %q; output=%q", expected, p.output.String())
	return ""
}

func (p *sshPTYProcess) waitExit(success bool) {
	p.t.Helper()
	select {
	case <-p.done:
		if success != (p.result == nil) {
			p.t.Fatalf("CLI exit=%v, want success=%v; output=%q", p.result, success, p.output.String())
		}
	case <-time.After(10 * time.Second):
		p.t.Fatalf("CLI did not exit; output=%q", p.output.String())
	}
	current, err := term.GetState(int(p.slave.Fd()))
	if err != nil || !reflect.DeepEqual(current, p.original) {
		p.t.Fatalf("local terminal mode was not restored: %v", err)
	}
}

func (p *sshPTYProcess) ready() string {
	p.t.Helper()
	p.wait(0, "SSH Session SSH-1")
	p.write("stty -echo; unset PROMPT_COMMAND; PS1='CT> '; printf '\\n__CT_READY__\\n'\r")
	output := p.wait(0, "\r\n__CT_READY__\r\n")
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, "Shared Host: ") {
			return strings.Fields(line)[2]
		}
	}
	p.t.Fatal("SSH sharing endpoint missing")
	return ""
}

func sshTestQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }

// TestOpenSSHPTYInteractivePrograms verifies the real shell, dimensions, Vim
// resize/edit/save, htop redraw after resize, remote Ctrl+C, and raw-mode recovery.
func TestOpenSSHPTYInteractivePrograms(t *testing.T) {
	config := loadOpenSSHTestConfig(t)
	t.Setenv(httpAuthTokenEnvVar, "local-openssh-integration-token")
	owner := startSSHPTYProcess(t, config.args())
	owner.ready()
	owner.write("stty size\r")
	owner.wait(0, "\r\n36 100\r\n")
	file := filepath.Join(t.TempDir(), "vim.txt")
	start := len(owner.output.String())
	owner.write("vim -Nu NONE -i NONE -n " + sshTestQuote(file) + "\r")
	owner.wait(start, "\x1b[?1049h")
	startResize := len(owner.output.String())
	owner.resize(132, 43)
	owner.waitCursorRow(startResize, 42)
	vimSize := filepath.Join(t.TempDir(), "vim-size.txt")
	owner.write(":call writefile([printf('%d %d', &lines, &columns)], '" + vimSize + "')\r")
	deadline := time.Now().Add(5 * time.Second)
	for {
		data, err := os.ReadFile(vimSize)
		if err == nil && string(data) == "43 132\n" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("Vim dimensions = %q, %v; want 43 132", data, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	owner.write("iCHANNELTERM_VIM_EDIT\x1b:wq\r")
	owner.wait(start, "\x1b[?1049l")
	data, err := os.ReadFile(file)
	if err != nil || string(data) != "CHANNELTERM_VIM_EDIT\n" {
		t.Fatalf("Vim edit/save = %q, %v", data, err)
	}
	if config.Htop == "" {
		t.Fatal("set htop in the integration config to verify the real program")
	}
	start = len(owner.output.String())
	owner.write(sshTestQuote(config.Htop) + " -C\r")
	owner.wait(start, "\x1b[?1049h")
	owner.wait(start, "Help")
	startResize = len(owner.output.String())
	owner.resize(119, 51)
	owner.waitCursorRow(startResize, 51)
	owner.write("q")
	owner.wait(start, "\x1b[?1049l")
	owner.expectSize(119, 51)
	owner.write("printf '\\n__CT_SLEEP__\\n'; sleep 30\r")
	owner.wait(0, "\r\n__CT_SLEEP__\r\n")
	owner.write("\x03")
	owner.write("printf '\\n__CT_INTERRUPTED__\\n'\r")
	owner.wait(0, "\r\n__CT_INTERRUPTED__\r\n")
	owner.write("\x1dq")
	owner.waitExit(true)
}

// TestOpenSSHOwnerCloseEndsAttachments verifies a live secondary terminal sees
// the read-path termination and restores its console when the owner exits.
func TestOpenSSHOwnerCloseEndsAttachments(t *testing.T) {
	config := loadOpenSSHTestConfig(t)
	t.Setenv(httpAuthTokenEnvVar, "local-openssh-integration-token")
	owner := startSSHPTYProcess(t, config.args())
	endpoint := owner.ready()
	observer := startSSHPTYProcess(t, []string{"attach", "SSH-1", "--endpoint", endpoint, "--highlight=false"})
	observer.wait(0, "__CT_READY__")
	owner.write("printf '\\n__CT_SHARED__\\n'\r")
	owner.wait(0, "\r\n__CT_SHARED__\r\n")
	observer.wait(0, "\r\n__CT_SHARED__\r\n")
	owner.write("\x1dq")
	owner.waitExit(true)
	observer.waitExit(false)
	observer.wait(0, "read attached session output")
	address := strings.TrimSuffix(strings.TrimPrefix(endpoint, "http://"), "/mcp")
	if connection, err := net.DialTimeout("tcp", address, time.Second); err == nil {
		_ = connection.Close()
		t.Fatal("owner exit left the sharing listener open")
	}
}

// TestOpenSSHRemoteExitRestoresTerminal verifies a genuine remote PTY EOF ends
// the owning CLI and restores local input settings without manual intervention.
func TestOpenSSHRemoteExitRestoresTerminal(t *testing.T) {
	config := loadOpenSSHTestConfig(t)
	t.Setenv(httpAuthTokenEnvVar, "local-openssh-integration-token")
	owner := startSSHPTYProcess(t, config.args())
	owner.ready()
	owner.resize(132, 43)
	owner.expectSize(132, 43)
	owner.write("exit\r")
	owner.resize(120, 40)
	owner.waitExit(false)
	owner.wait(0, "read attached session output")
}

// TestOpenSSHPTYResize follows successive owner sizes while a differently
// sized secondary attachment cannot take over the remote PTY dimensions.
func TestOpenSSHPTYResize(t *testing.T) {
	config := loadOpenSSHTestConfig(t)
	t.Setenv(httpAuthTokenEnvVar, "local-openssh-integration-token")
	owner := startSSHPTYProcess(t, config.args())
	endpoint := owner.ready()
	owner.expectSize(100, 36)
	observer := startSSHPTYProcess(t, []string{"attach", "SSH-1", "--endpoint", endpoint, "--highlight=false"})
	observer.wait(0, "__CT_READY__")
	for _, size := range [][2]uint16{{132, 43}, {80, 24}, {155, 50}, {100, 36}} {
		owner.resize(size[0], size[1])
		owner.expectSize(size[0], size[1])
		observer.resize(size[0]+10, size[1]+5)
		// Give an incorrectly installed secondary watcher time to contend.
		time.Sleep(300 * time.Millisecond)
		owner.expectSize(size[0], size[1])
	}
	// Rapid notifications may coalesce, but the final size must arrive.
	for cols := uint16(110); cols <= 140; cols++ {
		owner.resize(cols, 44)
	}
	owner.expectSize(140, 44)
	owner.resize(0, 0)
	time.Sleep(100 * time.Millisecond)
	owner.expectSize(140, 44)
	owner.resize(132, 43)
	owner.expectSize(132, 43)
	owner.write("\x1dq")
	owner.waitExit(true)
	observer.waitExit(false)
}

// TestOpenSSHExplicitInitialSize preserves initial flag overrides until the
// owner actually changes size; subsequent updates use both local dimensions.
func TestOpenSSHExplicitInitialSize(t *testing.T) {
	config := loadOpenSSHTestConfig(t)
	t.Setenv(httpAuthTokenEnvVar, "local-openssh-integration-token")
	owner := startSSHPTYProcess(t, append(config.args(), "--cols", "83", "--rows", "25"))
	owner.ready()
	owner.expectSize(83, 25)
	owner.resize(132, 43)
	owner.expectSize(132, 43)
	owner.write("\x1dq")
	owner.waitExit(true)
}

// waitCursorRow requires the full-screen application to redraw in the newly
// available row. Merely receiving a remote stty result would not prove
// that the running application handled SIGWINCH.
func (p *sshPTYProcess) waitCursorRow(after, row int) {
	p.t.Helper()
	cursor := regexp.MustCompile(fmt.Sprintf("\x1b\\[%d(;[0-9]+)?[Hfd]", row))
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if cursor.MatchString(p.output.String()[after:]) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	p.t.Fatalf("application did not redraw row %d after resize; output=%q", row, p.output.String()[after:])
}

func (p *sshPTYProcess) resize(cols, rows uint16) {
	p.t.Helper()
	if err := unix.IoctlSetWinsize(int(p.slave.Fd()), unix.TIOCSWINSZ, &unix.Winsize{Col: cols, Row: rows}); err != nil {
		p.t.Fatal(err)
	}
	width, height, err := term.GetSize(int(p.slave.Fd()))
	if err != nil || width != int(cols) || height != int(rows) {
		p.t.Fatalf("local window = %dx%d, %v; want %dx%d", width, height, err, cols, rows)
	}
}

func (p *sshPTYProcess) expectSize(cols, rows uint16) {
	p.t.Helper()
	start := len(p.output.String())
	// Resize delivery is asynchronous. Poll the actual remote PTY with a bounded
	// shell loop, and require its measured dimensions, never a local log message.
	p.write(fmt.Sprintf("for i in {1..60}; do s=$(stty size); if [ \"$s\" = '%d %d' ]; then printf '\\n__CT_SIZE__%%s\\n' \"$s\"; break; fi; sleep .05; done\r", rows, cols))
	p.wait(start, fmt.Sprintf("\r\n__CT_SIZE__%d %d\r\n", rows, cols))
	p.t.Logf("remote stty size confirmed: %d %d", rows, cols)
}

// TestOpenSSHServerDisconnectEndsAttachments terminates only the sshd child
// serving this test shell. It must not terminate the listening daemon.
func TestOpenSSHServerDisconnectEndsAttachments(t *testing.T) {
	config := loadOpenSSHTestConfig(t)
	t.Setenv(httpAuthTokenEnvVar, "local-openssh-integration-token")
	owner := startSSHPTYProcess(t, config.args())
	endpoint := owner.ready()
	observer := startSSHPTYProcess(t, []string{"attach", "SSH-1", "--endpoint", endpoint, "--highlight=false"})
	observer.wait(0, "__CT_READY__")
	owner.resize(132, 43)
	owner.expectSize(132, 43)
	owner.write("kill -TERM \"$PPID\"\r")
	owner.resize(120, 40)
	owner.waitExit(false)
	observer.waitExit(false)
	owner.wait(0, "read attached session output")
	observer.wait(0, "read attached session output")
	// The original listener must still support an explicit fresh connection.
	reopened := startSSHPTYProcess(t, config.args())
	reopened.ready()
	reopened.write("\x1dq")
	reopened.waitExit(true)
}

// TestOpenSSHNetworkBlackholeCanExitLocally drops SSH packets through a local
// relay after the shell starts, without sending FIN/RST. It tests the idle/
// small-write case; transport tests separately interrupt a saturated write.
func TestOpenSSHNetworkBlackholeCanExitLocally(t *testing.T) {
	config := loadOpenSSHTestConfig(t)
	t.Setenv(httpAuthTokenEnvVar, "local-openssh-integration-token")
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	var dropped atomic.Bool
	connected := make(chan struct{})
	finished := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() {
		defer close(finished)
		client, err := listener.Accept()
		if err != nil {
			return
		}
		defer client.Close()
		server, err := (&net.Dialer{}).DialContext(ctx, "tcp", net.JoinHostPort(config.Host, strconv.Itoa(config.Port)))
		if err != nil {
			return
		}
		defer server.Close()
		stop := context.AfterFunc(ctx, func() { _ = client.Close(); _ = server.Close() })
		defer stop()
		close(connected)
		copied := make(chan struct{})
		go func() {
			_, _ = io.Copy(sshDroppingWriter{Writer: client, drop: &dropped}, server)
			_ = client.Close()
			close(copied)
		}()
		_, _ = io.Copy(sshDroppingWriter{Writer: server, drop: &dropped}, client)
		_ = server.Close()
		<-copied
	}()
	t.Cleanup(func() { cancel(); _ = listener.Close(); <-finished })
	// Rebind only this server's already-trusted, unhashed host-key entry to
	// the loopback relay. Never discover or accept a new host key from the wire.
	data, err := os.ReadFile(config.KnownHosts)
	if err != nil {
		t.Fatal(err)
	}
	originalAddress := "[" + config.Host + "]:" + strconv.Itoa(config.Port)
	_, port, _ := net.SplitHostPort(listener.Addr().String())
	var trusted []string
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 3 && fields[0] == originalAddress {
			trusted = append(trusted, "[127.0.0.1]:"+port+" "+fields[1]+" "+fields[2])
		}
	}
	if len(trusted) == 0 {
		t.Fatal("blackhole test requires a dedicated unhashed [host]:port known_hosts entry")
	}
	proxyConfig := config
	proxyConfig.Port, _ = strconv.Atoi(port)
	proxyConfig.Host = "127.0.0.1"
	proxyConfig.KnownHosts = filepath.Join(t.TempDir(), "relay_known_hosts")
	if err := os.WriteFile(proxyConfig.KnownHosts, []byte(strings.Join(trusted, "\n")+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	owner := startSSHPTYProcess(t, proxyConfig.args())
	owner.ready()
	<-connected
	dropped.Store(true)
	owner.resize(132, 43)
	start := len(owner.output.String())
	owner.write("printf '\\n__CT_SHOULD_NOT_ARRIVE__\\n'\r")
	select {
	case <-owner.done:
		t.Fatalf("a silent blackhole unexpectedly ended the CLI: %v", owner.result)
	case <-time.After(200 * time.Millisecond):
	}
	if strings.Contains(owner.output.String()[start:], "\r\n__CT_SHOULD_NOT_ARRIVE__\r\n") {
		t.Fatal("relay did not drop the SSH command/output")
	}
	owner.write("\x1dq")
	owner.waitExit(true)
}

type sshDroppingWriter struct {
	io.Writer
	drop *atomic.Bool
}

// Write keeps the relay TCP stream alive while discarding encrypted packets.
func (w sshDroppingWriter) Write(data []byte) (int, error) {
	if w.drop.Load() {
		return len(data), nil
	}
	return w.Writer.Write(data)
}
