package command

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/core/app"
)

var (
	// ErrFileSessionRequired is returned when no open shared Session can be
	// selected for a file transfer.
	ErrFileSessionRequired = errors.New("an open shared Session is required")
	// ErrFileSessionAmbiguous is returned when an omitted --session could select
	// more than one open shared Session.
	ErrFileSessionAmbiguous = errors.New("multiple open shared Sessions are available")
)

type fileCommandDependencies struct {
	newAttach    attachSessionFactory
	listSessions func(context.Context, string) ([]mcpListedSession, error)
}

// fileLeaseSession is implemented by attachments that can request Host-side
// writer coordination for an entire file transfer.
type fileLeaseSession interface {
	AcquireFileTransferLease(context.Context) error
	ReleaseFileTransferLease(context.Context) error
}

// runFile parses CLI file transfers and uses the same Session attachment
// boundary as attach. It does not open Serial Transport or invoke a new MCP
// tool; all bytes pass through the existing Session read/write operations.
func runFile(ctx context.Context, args []string, output io.Writer, newAttach attachSessionFactory) error {
	return runFileWithDependencies(ctx, args, output, fileCommandDependencies{newAttach: newAttach, listSessions: listMCPSessions})
}

func runFileWithDependencies(ctx context.Context, args []string, output io.Writer, dependencies fileCommandDependencies) error {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" || args[0] == "help" {
		writeFileUsage(output)
		return nil
	}
	if dependencies.newAttach == nil || dependencies.listSessions == nil {
		return errors.New("file command dependencies must not be nil")
	}
	switch args[0] {
	case "send":
		return runFileSend(ctx, args[1:], output, dependencies)
	case "receive":
		return runFileReceive(ctx, args[1:], output, dependencies)
	default:
		return fmt.Errorf("unknown file command %q; use send or receive", args[0])
	}
}

func runFileSend(ctx context.Context, args []string, output io.Writer, dependencies fileCommandDependencies) (err error) {
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		_, err := parseFileOptions("file send", args, output, writeFileSendUsage)
		return err
	}
	if len(args) < 2 {
		writeFileSendUsage(output)
		return errors.New("local source and remote destination paths are required")
	}
	localPath, remotePath := args[0], args[1]
	options, err := parseFileOptions("file send", args[2:], output, writeFileSendUsage)
	if err != nil || options.help {
		return err
	}
	attached, identifier, err := attachFileSession(ctx, options, dependencies)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := attached.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("detach file transfer Session %q: %w", identifier, closeErr)
		}
	}()

	file, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("open local source %q: %w", localPath, err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("close local source %q: %w", localPath, closeErr)
		}
	}()
	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("stat local source %q: %w", localPath, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("local source %q must be a regular file", localPath)
	}
	return withFileTransferLease(ctx, attached, identifier, func() error {
		progress := newFileProgress(output)
		result, transferErr := app.SendFile(ctx, attached, file, info.Size(), remotePath, progress)
		if transferErr != nil {
			return transferErr
		}
		if finishErr := finishFileProgress(output); finishErr != nil {
			return finishErr
		}
		_, writeErr := fmt.Fprintf(output, "Sent %d bytes to %s via %s\nSHA-256: %s\n", result.Size, remotePath, identifier, result.SHA256)
		return writeErr
	})
}

func runFileReceive(ctx context.Context, args []string, output io.Writer, dependencies fileCommandDependencies) (err error) {
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		_, err := parseFileOptions("file receive", args, output, writeFileReceiveUsage)
		return err
	}
	if len(args) < 2 {
		writeFileReceiveUsage(output)
		return errors.New("remote source and local destination paths are required")
	}
	remotePath, localPath := args[0], args[1]
	options, err := parseFileOptions("file receive", args[2:], output, writeFileReceiveUsage)
	if err != nil || options.help {
		return err
	}
	if existing, statErr := os.Lstat(localPath); statErr == nil && existing.IsDir() {
		return fmt.Errorf("local destination %q is a directory", localPath)
	} else if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return fmt.Errorf("inspect local destination %q: %w", localPath, statErr)
	}
	attached, identifier, err := attachFileSession(ctx, options, dependencies)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := attached.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("detach file transfer Session %q: %w", identifier, closeErr)
		}
	}()

	directory := filepath.Dir(localPath)
	temporary, err := os.CreateTemp(directory, ".channelterm-receive-*")
	if err != nil {
		return fmt.Errorf("create temporary destination beside %q: %w", localPath, err)
	}
	temporaryPath := temporary.Name()
	defer func() { _ = os.Remove(temporaryPath) }()
	return withFileTransferLease(ctx, attached, identifier, func() error {
		progress := newFileProgress(output)
		result, transferErr := app.ReceiveFile(ctx, attached, temporary, remotePath, progress)
		if transferErr != nil {
			_ = temporary.Close()
			return transferErr
		}
		if syncErr := temporary.Sync(); syncErr != nil {
			_ = temporary.Close()
			return fmt.Errorf("sync temporary destination for %q: %w", localPath, syncErr)
		}
		if closeErr := temporary.Close(); closeErr != nil {
			return fmt.Errorf("close temporary destination for %q: %w", localPath, closeErr)
		}
		if replaceErr := replaceReceivedFile(temporaryPath, localPath); replaceErr != nil {
			return fmt.Errorf("install received file %q: %w", localPath, replaceErr)
		}
		if finishErr := finishFileProgress(output); finishErr != nil {
			return finishErr
		}
		_, writeErr := fmt.Fprintf(output, "Received %d bytes from %s via %s\nSHA-256: %s\n", result.Size, remotePath, identifier, result.SHA256)
		return writeErr
	})
}

// withFileTransferLease holds a Host-side file-transfer lease for one complete
// command, including all protocol cleanup. It releases with a fresh bounded
// context so cancellation of the transfer cannot leave the Session locked.
func withFileTransferLease(ctx context.Context, attached attachSession, identifier string, operation func() error) (err error) {
	lease, ok := attached.(fileLeaseSession)
	if !ok {
		return errors.New("attached Session does not support file transfer leases")
	}
	if err := lease.AcquireFileTransferLease(ctx); err != nil {
		return fmt.Errorf("acquire file transfer lease for Session %q: %w", identifier, err)
	}
	defer func() {
		releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if releaseErr := lease.ReleaseFileTransferLease(releaseCtx); releaseErr != nil && err == nil {
			err = fmt.Errorf("release file transfer lease for Session %q: %w", identifier, releaseErr)
		}
	}()
	return operation()
}

type fileOptions struct {
	session  string
	endpoint string
	help     bool
}

func parseFileOptions(name string, args []string, output io.Writer, usage func(io.Writer)) (fileOptions, error) {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(output)
	identifier := flags.String("session", "", "shared Session ID or short reference; omit when exactly one Session is open")
	endpoint := flags.String("endpoint", defaultMCPEndpoint, "Session Host endpoint")
	help := flags.Bool("help", false, "show help and exit")
	shortHelp := flags.Bool("h", false, "show help and exit")
	flags.Usage = func() {
		usage(output)
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return fileOptions{help: true}, nil
		}
		return fileOptions{}, err
	}
	if flags.NArg() != 0 {
		return fileOptions{}, fmt.Errorf("unexpected file argument %q", flags.Arg(0))
	}
	showHelp := *help || *shortHelp
	if showHelp {
		flags.Usage()
	}
	return fileOptions{session: strings.TrimSpace(*identifier), endpoint: strings.TrimSpace(*endpoint), help: showHelp}, nil
}

func attachFileSession(ctx context.Context, options fileOptions, dependencies fileCommandDependencies) (attachSession, string, error) {
	identifier := options.session
	if identifier == "" {
		sessions, err := dependencies.listSessions(ctx, options.endpoint)
		if err != nil {
			return nil, "", fmt.Errorf("list shared Sessions at %q: %w", options.endpoint, err)
		}
		open := make([]mcpListedSession, 0, len(sessions))
		for _, listed := range sessions {
			if listed.State == "open" {
				open = append(open, listed)
			}
		}
		switch len(open) {
		case 0:
			return nil, "", fmt.Errorf("%w; create one with channelterm attach SER-<PORT>", ErrFileSessionRequired)
		case 1:
			identifier = open[0].Reference
			if identifier == "" {
				identifier = open[0].ID
			}
		default:
			return nil, "", fmt.Errorf("%w; select one with --session", ErrFileSessionAmbiguous)
		}
	}
	attached, err := dependencies.newAttach(ctx, options.endpoint, identifier)
	if err != nil {
		return nil, "", err
	}
	return attached, identifier, nil
}

func newFileProgress(output io.Writer) app.FileTransferProgress {
	return func(transferred, total int64) error {
		percentage := float64(100)
		if total > 0 {
			percentage = float64(transferred) * 100 / float64(total)
		}
		_, err := fmt.Fprintf(output, "\rTransferred %d/%d bytes (%5.1f%%)", transferred, total, percentage)
		return err
	}
}

func finishFileProgress(output io.Writer) error {
	_, err := fmt.Fprintln(output)
	return err
}

// replaceReceivedFile keeps an existing destination recoverable while
// providing Windows-compatible replacement semantics. Both moves stay in the
// destination directory, so a failed installation can restore the old path.
func replaceReceivedFile(temporaryPath, destinationPath string) error {
	existing, err := os.Lstat(destinationPath)
	if errors.Is(err, os.ErrNotExist) {
		return os.Rename(temporaryPath, destinationPath)
	}
	if err != nil {
		return fmt.Errorf("inspect destination: %w", err)
	}
	if existing.IsDir() {
		return errors.New("destination is a directory")
	}
	backupPath := temporaryPath + ".previous"
	if err := os.Rename(destinationPath, backupPath); err != nil {
		return fmt.Errorf("preserve existing destination: %w", err)
	}
	if err := os.Rename(temporaryPath, destinationPath); err != nil {
		if restoreErr := os.Rename(backupPath, destinationPath); restoreErr != nil {
			return errors.Join(fmt.Errorf("move verified temporary file: %w", err), fmt.Errorf("restore existing destination: %w", restoreErr))
		}
		return fmt.Errorf("move verified temporary file: %w", err)
	}
	if err := os.Remove(backupPath); err != nil {
		return fmt.Errorf("remove replaced destination backup %q: %w", backupPath, err)
	}
	return nil
}

func writeFileUsage(output io.Writer) {
	fmt.Fprintln(output, "Usage: channelterm file send LOCAL_PATH REMOTE_PATH [options]")
	fmt.Fprintln(output, "       channelterm file receive REMOTE_PATH LOCAL_PATH [options]")
	fmt.Fprintln(output)
	fmt.Fprintln(output, "Stream a file through an existing shared Session and verify it with SHA-256.")
	fmt.Fprintln(output, "When exactly one Session is open, --session may be omitted.")
}

func writeFileSendUsage(output io.Writer) {
	fmt.Fprintln(output, "Usage: channelterm file send LOCAL_PATH REMOTE_PATH [--session SESSION] [--endpoint URL]")
}

func writeFileReceiveUsage(output io.Writer) {
	fmt.Fprintln(output, "Usage: channelterm file receive REMOTE_PATH LOCAL_PATH [--session SESSION] [--endpoint URL]")
}
