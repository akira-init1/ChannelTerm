package command

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/cli/terminalinput"
	"github.com/akira-init1/ChannelTerm/internal/core/app"
	"github.com/akira-init1/ChannelTerm/internal/core/session"
	sshtransport "github.com/akira-init1/ChannelTerm/internal/transport/ssh"
	"golang.org/x/term"
)

// runSSH owns one foreground SSH Session Host and uses the existing attachment
// UI. Other clients join through the unchanged authenticated MCP tools. The
// foreground command's exit closes its Manager and all owned SSH resources.
func runSSH(ctx context.Context, args []string, input io.Reader, output io.Writer, interrupts <-chan os.Signal) (err error) {
	flags := flag.NewFlagSet("ssh", flag.ContinueOnError)
	flags.SetOutput(output)
	port := flags.Int("port", 22, "SSH server port")
	flags.IntVar(port, "p", 22, "alias for --port")
	identity := flags.String("identity", "", "unencrypted private key (default ~/.ssh/id_ed25519, then id_rsa)")
	flags.StringVar(identity, "i", "", "alias for --identity")
	hosts := flags.String("known-hosts", "", "trusted host keys (default ~/.ssh/known_hosts)")
	terminalType := flags.String("term", "xterm-256color", "remote PTY terminal type")
	columns := flags.Int("cols", 0, "initial PTY columns (default local terminal size, or 80)")
	rows := flags.Int("rows", 0, "initial PTY rows (default local terminal size, or 24)")
	timeout := flags.Duration("timeout", 10*time.Second, "timeout for TCP, authentication, PTY, and shell setup")
	listen := flags.String("listen", defaultMCPListen, "local sharing address; must use a loopback IP")
	label := flags.String("label", "", "display-only Session label")
	highlight := flags.Bool("highlight", true, "apply semantic colors to plain terminal text")
	flags.Usage = func() {
		fmt.Fprintln(output, "Usage: channelterm ssh USER@HOST [options]")
		fmt.Fprintln(output, "Open a private-key-authenticated PTY shell and share it as SSH-1.")
		fmt.Fprintln(output, "In another terminal: channelterm attach SSH-1")
		fmt.Fprintln(output, "The foreground SSH command owns the Host; Ctrl+] q closes it and its Sessions.")
		fmt.Fprintln(output, "Trust the server key in known_hosts before connecting. Passwords, encrypted keys,")
		fmt.Fprintln(output, "ssh-agent, ssh_config, SSH exec, and SFTP are not supported.")
		fmt.Fprintln(output, "The owning terminal controls PTY resizing; secondary attachments do not resize it.")
		flags.PrintDefaults()
	}
	// Match target-first attach syntax while retaining standard flag-first use.
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		args = append(append([]string{}, args[1:]...), args[0])
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 1 {
		return errors.New("ssh requires exactly one USER@HOST destination")
	}
	user, host, found := strings.Cut(flags.Arg(0), "@")
	if !found || user == "" || host == "" {
		return errors.New("SSH destination must be USER@HOST")
	}
	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		host = host[1 : len(host)-1]
	}
	if *port < 1 || *port > 65535 || *timeout <= 0 {
		return errors.New("SSH port must be between 1 and 65535 and timeout must be positive")
	}
	bindHost, _, splitErr := net.SplitHostPort(*listen)
	if splitErr != nil || net.ParseIP(bindHost) == nil || !net.ParseIP(bindHost).IsLoopback() {
		return errors.New("SSH sharing --listen must be a loopback IP and port, for example 127.0.0.1:37099")
	}
	if *identity == "" || *hosts == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("resolve SSH configuration directory: %w", err)
		}
		if *identity == "" {
			*identity = filepath.Join(home, ".ssh", "id_ed25519")
			if _, err := os.Stat(*identity); errors.Is(err, os.ErrNotExist) {
				*identity = filepath.Join(home, ".ssh", "id_rsa")
			}
		}
		if *hosts == "" {
			*hosts = filepath.Join(home, ".ssh", "known_hosts")
		}
	}
	// Prefer the displayed terminal, then the input terminal when output is
	// redirected. Pipes and buffers retain explicit dimensions or defaults.
	var terminal *os.File
	var initialSize terminalinput.Size
	for _, candidate := range []any{output, input} {
		if file, ok := candidate.(*os.File); ok && term.IsTerminal(int(file.Fd())) {
			terminal = file
			initialSize, _ = terminalinput.ReadSize(file)
			break
		}
	}
	if *columns == 0 {
		*columns = int(initialSize.Columns)
	}
	if *rows == 0 {
		*rows = int(initialSize.Rows)
	}
	configuration := sshtransport.Config{
		User: user, Host: host, Port: *port, PrivateKeyPath: *identity, KnownHostsPath: *hosts,
		Term: *terminalType, Columns: *columns, Rows: *rows, Timeout: *timeout,
	}
	if _, err := sshtransport.New(configuration); err != nil {
		return err
	}
	token, err := loadHTTPAuthToken()
	if err != nil {
		return err
	}
	// Reserve the sharing address before dialing SSH. Never replace an existing
	// Host or create an unshareable remote shell when the local port is occupied.
	listener, err := net.Listen("tcp", *listen)
	if err != nil {
		return fmt.Errorf("reserve SSH sharing address %s (use --listen 127.0.0.1:37100 for another Host): %w", *listen, err)
	}
	defer listener.Close()
	manager := session.NewManager()
	defer func() { err = errors.Join(err, manager.Close()) }()
	application, err := app.New(app.Dependencies{Manager: manager})
	if err != nil {
		return err
	}
	registry, err := newMCPRegistry(manager, nil)
	if err != nil {
		return err
	}
	hostCtx, stopHost := context.WithCancel(ctx)
	defer stopHost()
	// During setup any process signal cancels the connection. Once attached,
	// Ctrl+C is remote input, while termination signals still stop this Host.
	setupDone, signalDone := make(chan struct{}), make(chan struct{})
	attachInterrupts := make(chan os.Signal, 1)
	go func() {
		defer close(signalDone)
		for {
			select {
			case <-hostCtx.Done():
				return
			case signal, ok := <-interrupts:
				if !ok {
					return
				}
				select {
				case <-setupDone:
					if signal != os.Interrupt {
						stopHost()
						return
					}
					select {
					case attachInterrupts <- signal:
					case <-hostCtx.Done():
						return
					}
				default:
					stopHost()
					return
				}
			}
		}
	}()
	defer func() { stopHost(); <-signalDone }()
	opened, err := application.OpenSSH(hostCtx, app.OpenSSHRequest{Config: configuration, Label: *label})
	close(setupDone)
	if err != nil {
		return err
	}
	// Only the foreground owner installs a watcher. Ordinary attachments have
	// no resize path, preventing competing windows from changing the shared PTY.
	resizeCtx, stopResize := context.WithCancel(hostCtx)
	resizeDone := make(chan error, 1)
	if terminal == nil {
		resizeDone <- nil
	} else {
		go func() {
			resizeErr := terminalinput.WatchSize(resizeCtx, terminal, initialSize, func(cols, rows uint16) error {
				return application.ResizeSession(opened.Info.ID, cols, rows)
			})
			if resizeCtx.Err() != nil {
				resizeErr = nil
			}
			resizeDone <- resizeErr
			if resizeErr != nil {
				stopHost()
			}
		}()
	}
	defer func() {
		stopResize()
		// A request can block in a socket write. Close the owned Channel before
		// joining the watcher, including on failures before the HTTP Host starts.
		closeErr := manager.Close()
		err = errors.Join(err, closeErr, <-resizeDone)
	}()
	endpoint := httpEndpoint(listener.Addr().String(), defaultMCPPath)
	if _, err := fmt.Fprintf(output, "SSH Session %s (%s)\nShared Host: %s (Bearer authentication required)\nAttach: channelterm attach %s --endpoint %s\nCtrl+] q closes this Host and its Sessions.\n", opened.Info.Metadata.Reference, opened.Info.Metadata.Endpoint, endpoint, opened.Info.Metadata.Reference, endpoint); err != nil {
		return err
	}
	// Keep HTTP available while the owner detaches, even if the process
	// context is cancelled. The deferred cleanup below then stops the Host.
	serverCtx, stopServer := context.WithCancel(context.WithoutCancel(hostCtx))
	defer stopServer()
	serverDone := make(chan error, 1)
	go func() {
		serverDone <- serveMCPHTTP(serverCtx, registry, listener, defaultMCPPath, token, false, io.Discard, true)
		stopHost()
	}()
	defer func() {
		// Release Session readers/writers before closing the Host's remaining
		// client connections. No shared resource outlives this foreground owner.
		stopResize()
		closeErr := manager.Close()
		stopServer()
		err = errors.Join(err, closeErr, <-serverDone)
	}()
	return runAttachSessionWithInputEnd(hostCtx, []string{
		"--endpoint", endpoint, "--highlight=" + strconv.FormatBool(*highlight), opened.Info.ID,
	}, input, output, newMCPAttachSession, attachInterrupts, stopHost)
}
