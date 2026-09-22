package command

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	selfinstall "github.com/akira-init1/ChannelTerm/internal/install"
)

// installationManager is the CLI's narrow seam around platform installation;
// tests use it without mutating the developer's real commands or PATH.
type installationManager interface {
	Install(selfinstall.InstallOptions) (selfinstall.InstallResult, error)
	Uninstall(selfinstall.UninstallOptions) (selfinstall.UninstallResult, error)
}

// newInstallationManager binds linker-provided build provenance to the running
// executable selected as the installation source.
func newInstallationManager() (installationManager, error) {
	return selfinstall.NewDefault(selfinstall.BuildInfo{
		Version: version,
		Commit:  buildCommit,
		BuiltAt: buildTime,
	})
}

func runInstall(args []string, output io.Writer) error {
	manager, err := newInstallationManager()
	if err != nil {
		return fmt.Errorf("initialize installer: %w", err)
	}
	return runInstallWithManager(args, output, manager)
}

func runInstallWithManager(args []string, output io.Writer, manager installationManager) error {
	flags := flag.NewFlagSet("install", flag.ContinueOnError)
	flags.SetOutput(output)
	noPath := flags.Bool("no-path", false, "install commands without modifying the current-user PATH")
	adopt := flags.Bool("adopt", false, "take ownership of matching manually installed command files")
	allowDowngrade := flags.Bool("allow-downgrade", false, "allow an older semantic version to replace a newer installed version")
	flags.Usage = func() {
		fmt.Fprintln(output, "Usage: channelterm install [--no-path] [--adopt] [--allow-downgrade]")
		fmt.Fprintln(output)
		fmt.Fprintln(output, "Install or update ChannelTerm for the current user.")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected install argument %q", flags.Arg(0))
	}
	result, err := manager.Install(selfinstall.InstallOptions{
		NoPath:         *noPath,
		Adopt:          *adopt,
		AllowDowngrade: *allowDowngrade,
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(output, "ChannelTerm %s for the current user.\n", result.Action)
	fmt.Fprintf(output, "  command:  %s\n", result.Binary)
	fmt.Fprintf(output, "  short:    %s\n", result.Alias)
	fmt.Fprintf(output, "  manifest: %s\n", result.Manifest)
	fmt.Fprintf(output, "  config:   %s\n", result.Configuration)
	if result.PathChanged {
		fmt.Fprintln(output, "PATH was updated. Open a new terminal before using channelterm or cterm by name.")
	}
	return nil
}

func runUninstall(args []string, input io.Reader, output io.Writer) error {
	manager, err := newInstallationManager()
	if err != nil {
		return fmt.Errorf("initialize installer: %w", err)
	}
	return runUninstallWithManager(args, input, output, manager)
}

func runUninstallWithManager(args []string, input io.Reader, output io.Writer, manager installationManager) error {
	flags := flag.NewFlagSet("uninstall", flag.ContinueOnError)
	flags.SetOutput(output)
	purge := flags.Bool("purge", false, "also remove ChannelTerm configuration, device state, and HTTP credentials")
	yes := flags.Bool("yes", false, "confirm destructive --purge removal without prompting")
	flags.Usage = func() {
		fmt.Fprintln(output, "Usage: channelterm uninstall [--purge [--yes]]")
		fmt.Fprintln(output)
		fmt.Fprintln(output, "Remove the current-user installation; user data is preserved unless --purge is set.")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected uninstall argument %q", flags.Arg(0))
	}
	if *yes && !*purge {
		return errors.New("--yes is valid only with --purge")
	}
	if *purge && !*yes {
		// Only an explicit y confirms destructive removal. Treat n, an empty
		// answer, and all other input as cancellation so accidental text cannot
		// authorize deletion.
		fmt.Fprintln(output, "This permanently deletes ChannelTerm configuration, device state, and HTTP credentials.")
		fmt.Fprint(output, "Continue? [y/N]: ")
		answer, err := bufio.NewReader(input).ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return fmt.Errorf("read purge confirmation: %w", err)
		}
		if !strings.EqualFold(strings.TrimSpace(answer), "y") {
			return errors.New("purge cancelled")
		}
	}
	result, err := manager.Uninstall(selfinstall.UninstallOptions{Purge: *purge})
	if err != nil {
		return err
	}
	fmt.Fprintln(output, "ChannelTerm uninstalled for the current user.")
	if result.PathChanged {
		fmt.Fprintln(output, "The installer-managed PATH entry was removed; existing terminals retain their old environment.")
	}
	if len(result.Purged) > 0 {
		fmt.Fprintf(output, "Purged %d user-data files.\n", len(result.Purged))
	} else if !*purge {
		fmt.Fprintln(output, "Configuration, device state, and HTTP credentials were preserved.")
	}
	if result.Deferred {
		fmt.Fprintln(output, "The running Windows executable will be removed after this process exits.")
	}
	return nil
}

// runDeferredUninstallCleanup dispatches the private Windows helper command.
func runDeferredUninstallCleanup(args []string) error {
	return selfinstall.ParseAndRunDeferredCleanup(args)
}
