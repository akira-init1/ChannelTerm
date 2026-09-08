package command

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/core/app"
	"github.com/akira-init1/ChannelTerm/internal/core/session"
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

// fileTransferEventReporter forwards client-side file-transfer status to the
// host-owned Session event stream. The file payload continues to use the
// existing attach Session byte path.
type fileTransferEventReporter interface {
	ReportFileTransferEvent(context.Context, session.EventType, map[string]any) error
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
	return withFileTransferLease(ctx, attached, identifier, func() (operationErr error) {
		started := false
		metadata := map[string]any{"direction": "send", "local_path": localPath, "remote_path": remotePath, "total": info.Size()}
		if eventErr := reportFileTransferEvent(ctx, attached, session.EventFileTransferStarted, metadata); eventErr != nil {
			return eventErr
		}
		started = true
		defer func() {
			if operationErr == nil || !started {
				return
			}
			failureCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			failureMetadata := copyFileTransferMetadata(metadata)
			failureMetadata["error"] = operationErr.Error()
			_ = reportFileTransferEvent(failureCtx, attached, session.EventFileTransferFailed, failureMetadata)
		}()
		progress := fileTransferProgress(ctx, output, attached, metadata)
		result, transferErr := app.SendFile(ctx, attached, file, info.Size(), remotePath, progress)
		if transferErr != nil {
			return transferErr
		}
		completedMetadata := copyFileTransferMetadata(metadata)
		completedMetadata["remote_path"] = result.RemotePath
		completedMetadata["sent"] = result.Size
		completedMetadata["total"] = result.Size
		completedMetadata["percent"] = float64(100)
		completedMetadata["sha256"] = result.SHA256
		if eventErr := reportFileTransferEvent(ctx, attached, session.EventFileTransferCompleted, completedMetadata); eventErr != nil {
			return eventErr
		}
		if finishErr := finishFileProgress(output); finishErr != nil {
			return finishErr
		}
		_, writeErr := fmt.Fprintf(output, "SHA-256: OK\nSaved: %s\n", result.RemotePath)
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
	localPath, err = nextAvailableLocalPath(localPath)
	if err != nil {
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

	directory := filepath.Dir(localPath)
	temporary, err := os.CreateTemp(directory, ".channelterm-receive-*")
	if err != nil {
		return fmt.Errorf("create temporary destination beside %q: %w", localPath, err)
	}
	temporaryPath := temporary.Name()
	defer func() { _ = os.Remove(temporaryPath) }()
	return withFileTransferLease(ctx, attached, identifier, func() (operationErr error) {
		started := false
		metadata := map[string]any{"direction": "receive", "local_path": localPath, "remote_path": remotePath}
		if eventErr := reportFileTransferEvent(ctx, attached, session.EventFileTransferStarted, metadata); eventErr != nil {
			return eventErr
		}
		started = true
		defer func() {
			if operationErr == nil || !started {
				return
			}
			failureCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			failureMetadata := copyFileTransferMetadata(metadata)
			failureMetadata["error"] = operationErr.Error()
			_ = reportFileTransferEvent(failureCtx, attached, session.EventFileTransferFailed, failureMetadata)
		}()
		progress := fileTransferProgress(ctx, output, attached, metadata)
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
		completedMetadata := copyFileTransferMetadata(metadata)
		completedMetadata["received"] = result.Size
		completedMetadata["total"] = result.Size
		completedMetadata["percent"] = float64(100)
		completedMetadata["sha256"] = result.SHA256
		if eventErr := reportFileTransferEvent(ctx, attached, session.EventFileTransferCompleted, completedMetadata); eventErr != nil {
			return eventErr
		}
		if finishErr := finishFileProgress(output); finishErr != nil {
			return finishErr
		}
		_, writeErr := fmt.Fprintf(output, "SHA-256: OK\nSaved: %s\n", localPath)
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
	started := time.Now()
	return func(transferred, total int64) error {
		percentage := float64(100)
		if total > 0 {
			percentage = float64(transferred) * 100 / float64(total)
		}
		filled := int(percentage / 5)
		if filled > 20 {
			filled = 20
		}
		bar := strings.Repeat("#", filled) + strings.Repeat("-", 20-filled)
		elapsed := time.Since(started).Seconds()
		speed := float64(0)
		if elapsed > 0 {
			speed = float64(transferred) / elapsed
		}
		_, err := fmt.Fprintf(output, "\r[%s] %3.0f%%\r\n%s / %s\r\n%s/s", bar, percentage, formatFileTransferBytes(transferred), formatFileTransferBytes(total), formatFileTransferBytes(int64(speed)))
		return err
	}
}

func formatFileTransferBytes(value int64) string {
	if value < 1024 {
		return fmt.Sprintf("%d B", value)
	}
	units := []string{"KiB", "MiB", "GiB"}
	amount := float64(value)
	for _, unit := range units {
		amount /= 1024
		if amount < 1024 || unit == units[len(units)-1] {
			return fmt.Sprintf("%.1f %s", amount, unit)
		}
	}
	return fmt.Sprintf("%d B", value)
}

// fileTransferProgress updates the local CLI display and independently reports
// confirmed protocol progress to the shared Session event stream.
func fileTransferProgress(ctx context.Context, output io.Writer, attached attachSession, base map[string]any) app.FileTransferProgress {
	localProgress := newFileProgress(output)
	started := time.Now()
	return func(transferred, total int64) error {
		if err := localProgress(transferred, total); err != nil {
			return err
		}
		metadata := copyFileTransferMetadata(base)
		metadata["total"] = total
		if base["direction"] == "receive" {
			metadata["received"] = transferred
		} else {
			metadata["sent"] = transferred
		}
		percentage := float64(100)
		if total > 0 {
			percentage = float64(transferred) * 100 / float64(total)
		}
		metadata["percent"] = percentage
		elapsed := time.Since(started).Seconds()
		if elapsed > 0 {
			metadata["speed"] = float64(transferred) / elapsed
		} else {
			metadata["speed"] = float64(0)
		}
		return reportFileTransferEvent(ctx, attached, session.EventFileTransferProgress, metadata)
	}
}

// reportFileTransferEvent is intentionally a no-op for legacy or test attach
// implementations. Production MCP attachments implement the reporter and
// therefore make status visible to every Session event observer.
func reportFileTransferEvent(ctx context.Context, attached attachSession, typ session.EventType, metadata map[string]any) error {
	reporter, ok := attached.(fileTransferEventReporter)
	if !ok {
		return nil
	}
	return reporter.ReportFileTransferEvent(ctx, typ, metadata)
}

func copyFileTransferMetadata(metadata map[string]any) map[string]any {
	copy := make(map[string]any, len(metadata))
	for key, value := range metadata {
		copy[key] = value
	}
	return copy
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
	return fmt.Errorf("destination %q appeared during transfer; refusing to overwrite", destinationPath)
}

// nextAvailableLocalPath chooses the requested local path when absent, or its
// first _N sibling when a file already exists. filepath is used only for PC
// paths, keeping this logic correct on Windows, Linux, and macOS.
func nextAvailableLocalPath(wanted string) (string, error) {
	directory, filename := filepath.Split(wanted)
	stem, extension := splitLocalFilenameExtension(filename)
	for sequence := 0; ; sequence++ {
		candidate := wanted
		if sequence > 0 {
			candidate = directory + stem + "_" + strconv.Itoa(sequence) + extension
		}
		info, err := os.Lstat(candidate)
		if errors.Is(err, os.ErrNotExist) {
			return candidate, nil
		}
		if err != nil {
			return "", fmt.Errorf("inspect local destination %q: %w", candidate, err)
		}
		if info.IsDir() && sequence == 0 {
			return "", fmt.Errorf("local destination %q is a directory", candidate)
		}
	}
}

func splitLocalFilenameExtension(filename string) (string, string) {
	if filename == "" || (strings.HasPrefix(filename, ".") && !strings.Contains(filename[1:], ".")) {
		return filename, ""
	}
	if index := strings.IndexByte(filename, '.'); index > 0 {
		return filename[:index], filename[index:]
	}
	return filename, ""
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
