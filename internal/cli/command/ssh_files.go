package command

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/core/app"
	sshtransport "github.com/akira-init1/ChannelTerm/internal/transport/ssh"
)

func runSSHFiles(ctx context.Context, protocol string, args []string, input io.Reader, output io.Writer, run func(context.Context, app.SSHFilesRequest) (app.SSHFilesResult, error)) error {
	flags := flag.NewFlagSet(protocol, flag.ContinueOnError)
	flags.SetOutput(output)
	port := flags.Int("port", 22, "SSH server port")
	flags.IntVar(port, "p", 22, "alias for --port")
	var identities identityFlags
	flags.Var(&identities, "identity", "private key; repeat for ordered candidates; defaults try id_ed25519 then id_rsa")
	flags.Var(&identities, "i", "alias for --identity; repeatable")
	trusted := flags.String("known-hosts", "", "OpenSSH trust file (default ~/.ssh/known_hosts)")
	setupTimeout := flags.Duration("timeout", 10*time.Second, "network setup budget excluding interactive prompts")
	operationTimeout := flags.Duration("operation-timeout", 0, "overall operation deadline including prompts (0 disables)")
	keepAlive := flags.Duration("keepalive-interval", 0, "SSH liveness probe interval after setup (0 disables)")
	keepAliveCount := flags.Int("keepalive-count", 3, "maximum response wait in keepalive intervals")
	recursive := flags.Bool("recursive", false, "copy directories recursively; symlink uploads are rejected")
	flags.BoolVar(recursive, "r", false, "alias for --recursive")
	overwrite := flags.Bool("overwrite", false, "allow replacing existing files (failed transfers can leave partial files)")
	safeReplace := flags.Bool("safe-replace", false, "stage each SFTP file before replacement; requires --overwrite (upload needs POSIX rename)")
	flags.Usage = func() {
		fmt.Fprintf(output, "Usage: channelterm %s USER@HOST OPERATION [paths] [options]\n", protocol)
		fmt.Fprintln(output, "  put LOCAL REMOTE     Upload a file or directory")
		fmt.Fprintln(output, "  get REMOTE LOCAL     Download a file or directory")
		if protocol == "sftp" {
			fmt.Fprintln(output, "  ls [REMOTE]          List a directory (default .)")
			fmt.Fprintln(output, "  stat REMOTE          Inspect an entry without following its final symlink")
			fmt.Fprintln(output, "  mkdir REMOTE         Create one directory; parent must exist")
			fmt.Fprintln(output, "  rename OLD NEW       Move/rename a remote entry")
			fmt.Fprintln(output, "  rm REMOTE            Remove a file or symlink")
			fmt.Fprintln(output, "  rmdir REMOTE         Remove an empty directory")
		} else {
			fmt.Fprintln(output, "Uses legacy SCP with a remote POSIX scp command, not modern OpenSSH's SFTP default.")
		}
		fmt.Fprintln(output, "Dedicated SSH connection: no PTY, shared Session, or MCP listener. Ctrl+C cancels.")
		fmt.Fprintln(output, "Paths are literal; no glob or ~ expansion. Existing destination directories receive the source basename.")
		fmt.Fprintln(output, "Passwords are not saved. Unknown host keys require confirmation; changed keys are rejected.")
		flags.PrintDefaults()
	}
	if len(args) == 0 {
		flags.Usage()
		return nil
	}
	ordered, err := sshFileFlagOrder(flags, args)
	if err != nil {
		return err
	}
	if err := flags.Parse(ordered); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	positional := flags.Args()
	if len(positional) < 2 {
		return errors.New("SSH file command requires USER@HOST and an operation")
	}
	user, host, ok := strings.Cut(positional[0], "@")
	if !ok || user == "" || host == "" {
		return errors.New("SSH destination must be USER@HOST")
	}
	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		host = host[1 : len(host)-1]
	}
	if *port < 1 || *port > 65535 || *setupTimeout <= 0 || *operationTimeout < 0 || *keepAlive < 0 || *keepAliveCount < 1 || *keepAlive > time.Duration(1<<63-1)/time.Duration(*keepAliveCount) {
		return errors.New("invalid SSH port or timeout")
	}
	r := app.SSHFilesRequest{Config: sshtransport.Config{User: user, Host: host, Port: *port, KnownHostsPath: *trusted, Timeout: *setupTimeout}, Protocol: protocol, Operation: positional[1], Recursive: *recursive, Overwrite: *overwrite}
	identities.configure(&r.Config)
	r.Config.KeepAliveInterval, r.Config.KeepAliveCountMax = *keepAlive, *keepAliveCount
	r.SafeReplace = *safeReplace
	paths := positional[2:]
	if r.Operation == "ls" && len(paths) == 0 {
		paths = []string{"."}
	}
	want := 1
	if r.Operation == "put" || r.Operation == "get" || r.Operation == "rename" {
		want = 2
	}
	if len(paths) != want {
		return fmt.Errorf("%s requires %d path argument(s)", r.Operation, want)
	}
	switch r.Operation {
	case "put":
		r.Local, r.Remote = paths[0], paths[1]
	case "get":
		r.Remote, r.Local = paths[0], paths[1]
	case "rename":
		r.Remote, r.Target = paths[0], paths[1]
	case "ls", "stat", "mkdir", "rm", "rmdir":
		r.Remote = paths[0]
	default:
		return fmt.Errorf("unknown %s operation %q", protocol, r.Operation)
	}
	if protocol == "scp" && r.Operation != "put" && r.Operation != "get" {
		return errors.New("SCP supports put and get only")
	}
	if *recursive && r.Operation != "put" && r.Operation != "get" {
		return errors.New("--recursive applies only to put/get, not deletion")
	}
	if *safeReplace && (protocol != "sftp" || !*overwrite || r.Operation != "put" && r.Operation != "get") {
		return errors.New("--safe-replace requires sftp put/get and --overwrite")
	}
	if err := configureSSHAuthentication(&r.Config, input, output); err != nil {
		return err
	}
	if *operationTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, *operationTimeout)
		defer cancel()
	}
	result, err := run(ctx, r)
	if err != nil {
		return err
	}
	if r.Operation == "ls" || r.Operation == "stat" {
		for _, info := range result.Entries {
			if _, err := fmt.Fprintf(output, "%s %12d %s %q\n", info.Mode, info.Size, info.Modified.Format(time.RFC3339), info.Name); err != nil {
				return err
			}
		}
		return nil
	}
	_, err = fmt.Fprintf(output, "%s %s completed.\n", strings.ToUpper(protocol), r.Operation)
	return err
}

// sshFileFlagOrder accepts options around positional paths without treating
// Windows drive colons as remote separators. -- ends option recognition.
func sshFileFlagOrder(flags *flag.FlagSet, args []string) ([]string, error) {
	var options, positional []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			positional = append(positional, args[i+1:]...)
			break
		}
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			positional = append(positional, arg)
			continue
		}
		options = append(options, arg)
		name, _, inline := strings.Cut(strings.TrimLeft(arg, "-"), "=")
		if name == "help" || name == "h" {
			continue
		}
		option := flags.Lookup(name)
		if option == nil {
			return nil, fmt.Errorf("unknown option %q", arg)
		}
		boolean, isBoolean := option.Value.(interface{ IsBoolFlag() bool })
		if !inline && !(isBoolean && boolean.IsBoolFlag()) {
			i++
			if i == len(args) {
				return nil, fmt.Errorf("option %s requires a value", arg)
			}
			options = append(options, args[i])
		}
	}
	return append(append(options, "--"), positional...), nil
}
