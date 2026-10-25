package command

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/core/app"
	sshtransport "github.com/akira-init1/ChannelTerm/internal/transport/ssh"
	"golang.org/x/term"
)

func runSSHFiles(ctx context.Context, protocol string, args []string, input io.Reader, output io.Writer, run func(context.Context, app.SSHFilesRequest) (app.SSHFilesResult, error)) error {
	interactive := false
	if file, ok := output.(*os.File); ok && term.IsTerminal(int(file.Fd())) {
		interactive = enableANSIOutput(file)
	}
	return runSSHFilesWithProgress(ctx, protocol, args, input, output, run, interactive)
}

// runSSHFilesWithProgress keeps terminal detection outside the command logic
// so both interactive and redirected presentation can be tested without a TTY.
func runSSHFilesWithProgress(ctx context.Context, protocol string, args []string, input io.Reader, output io.Writer, run func(context.Context, app.SSHFilesRequest) (app.SSHFilesResult, error), interactive bool) error {
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
	verify := flags.String("verify", "none", "file verification: none or sha256 (put/get; SCP requires remote sha256sum)")
	resume := flags.Bool("resume", false, "continue a single SFTP file after comparing its existing prefix; implies SHA-256 verification")
	machine := flags.Bool("json", false, "emit bounded JSON-line results for put/get and SFTP ls/stat")
	credentials := flags.Bool("credentials-stdin", false, "read one bounded JSON credential object from stdin; requires --json; never prompts")
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
		fmt.Fprintln(output, "Terminal output shows per-file progress, speed and ETA; redirected output omits the progress bar.")
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
	r.Verify = *verify
	r.Resume = *resume
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
	if *verify != "none" && *verify != "sha256" {
		return errors.New("--verify must be none or sha256")
	}
	if *verify == "sha256" && r.Operation != "put" && r.Operation != "get" {
		return errors.New("--verify=sha256 requires put or get")
	}
	if *resume && (protocol != "sftp" || r.Operation != "put" && r.Operation != "get" || *recursive || *overwrite || *safeReplace) {
		return errors.New("--resume requires a single SFTP put/get without --recursive, --overwrite or --safe-replace")
	}
	if *resume {
		r.Verify = "sha256"
	}
	if *machine && r.Operation != "put" && r.Operation != "get" && r.Operation != "ls" && r.Operation != "stat" {
		return errors.New("--json requires put/get or SFTP ls/stat")
	}
	if *credentials && !*machine {
		return errors.New("--credentials-stdin requires --json")
	}
	if *credentials {
		if err := readSSHFileCredentials(input, &r.Config); err != nil {
			return err
		}
	}
	authInput := input
	if *machine {
		// Machine output must never mix with interactive authentication prompts.
		authInput = strings.NewReader("")
	}
	if err := configureSSHAuthentication(&r.Config, authInput, output); err != nil {
		return err
	}
	if *operationTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, *operationTimeout)
		defer cancel()
	}
	if *machine {
		return runSSHFilesJSON(ctx, output, r, run)
	}
	var progress *sshFileProgress
	if interactive && (r.Operation == "put" || r.Operation == "get") {
		progress = &sshFileProgress{output: output, now: time.Now, request: r}
		r.Progress = progress.Report
	}
	if r.Verify == "sha256" {
		r.Verification = func(event app.SSHFileVerification) error {
			if progress != nil {
				if err := progress.endLine(); err != nil {
					return err
				}
			}
			if event.SourceSHA256 == "" {
				_, err := fmt.Fprintf(output, "Verifying SHA-256: %q\n", event.Name)
				return err
			}
			local, remote := event.SourceSHA256, event.DestinationSHA256
			if r.Operation == "get" {
				local, remote = remote, local
			}
			_, err := fmt.Fprintf(output, "Local SHA-256:  %s\nRemote SHA-256: %s\nVerify: SHA-256 matched (%q)\n", local, remote, event.Name)
			return err
		}
	}
	result, err := run(ctx, r)
	if progress != nil {
		return errors.Join(err, progress.Finish(result, err))
	}
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
