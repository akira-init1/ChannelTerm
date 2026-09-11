package command

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
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
	info, err := os.Lstat(localPath)
	if err != nil {
		return fmt.Errorf("stat local source %q: %w", localPath, err)
	}
	if info.IsDir() {
		return runFileSendDirectory(ctx, localPath, remotePath, options, output, dependencies)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("local source %q must be a regular file or directory", localPath)
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
	info, err = file.Stat()
	if err != nil {
		return fmt.Errorf("stat local source %q: %w", localPath, err)
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
		progress := newFileTransferProgress(ctx, output, attached, metadata)
		defer func() {
			operationErr = finishFileTransferPresentation(output, progress, operationErr)
		}()
		if startErr := progress.Start(info.Size()); startErr != nil {
			return startErr
		}
		result, transferErr := app.SendFile(ctx, attached, file, info.Size(), remotePath, progress.Report)
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
		if completeErr := progress.Complete(result.Size); completeErr != nil {
			return completeErr
		}
		if finishErr := progress.finish(); finishErr != nil {
			return finishErr
		}
		_, writeErr := fmt.Fprintf(output, "SHA-256: OK\nSaved: %s\n", result.RemotePath)
		return writeErr
	})
}

func runFileSendDirectory(ctx context.Context, localPath, remotePath string, options fileOptions, output io.Writer, dependencies fileCommandDependencies) (err error) {
	archiveSize, err := app.PlanDirectory(localPath)
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
	return withFileTransferLease(ctx, attached, identifier, func() (operationErr error) {
		metadata := map[string]any{"direction": "send", "kind": "directory", "local_path": localPath, "remote_path": remotePath, "total": archiveSize}
		if eventErr := reportFileTransferEvent(ctx, attached, session.EventFileTransferStarted, metadata); eventErr != nil {
			return eventErr
		}
		defer reportFailedFileTransfer(ctx, attached, metadata, &operationErr)
		progress := newFileTransferProgress(ctx, output, attached, metadata)
		defer func() { operationErr = finishFileTransferPresentation(output, progress, operationErr) }()
		result, transferErr := app.SendDirectory(ctx, attached, localPath, remotePath, progress.Report)
		if transferErr != nil {
			return transferErr
		}
		completed := copyFileTransferMetadata(metadata)
		completed["remote_path"] = result.RemotePath
		completed["sent"] = result.Size
		completed["total"] = result.Size
		completed["percent"] = float64(100)
		if eventErr := reportFileTransferEvent(ctx, attached, session.EventFileTransferCompleted, completed); eventErr != nil {
			return eventErr
		}
		if completeErr := progress.Complete(result.Size); completeErr != nil {
			return completeErr
		}
		if finishErr := progress.finish(); finishErr != nil {
			return finishErr
		}
		_, writeErr := fmt.Fprintf(output, "Tar stream: complete\nSaved: %s\n", result.RemotePath)
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
	attached, identifier, err := attachFileSession(ctx, options, dependencies)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := attached.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("detach file transfer Session %q: %w", identifier, closeErr)
		}
	}()

	return withFileTransferLease(ctx, attached, identifier, func() (operationErr error) {
		kind, kindErr := app.DetectRemotePath(ctx, attached, remotePath)
		if kindErr != nil {
			return fmt.Errorf("inspect remote source %q: %w", remotePath, kindErr)
		}
		switch kind {
		case app.RemotePathDirectory:
			return runFileReceiveDirectory(ctx, attached, remotePath, localPath, output)
		case app.RemotePathMissing:
			return fmt.Errorf("remote source %q does not exist", remotePath)
		case app.RemotePathUnsupported:
			return fmt.Errorf("remote source %q must be a regular file or directory", remotePath)
		case app.RemotePathFile:
		default:
			return fmt.Errorf("remote source %q has unsupported type %q", remotePath, kind)
		}
		localPath, err = nextAvailableLocalPath(localPath)
		if err != nil {
			return err
		}
		directory := filepath.Dir(localPath)
		temporary, err := os.CreateTemp(directory, ".channelterm-receive-*")
		if err != nil {
			return fmt.Errorf("create temporary destination beside %q: %w", localPath, err)
		}
		temporaryPath := temporary.Name()
		defer func() { _ = os.Remove(temporaryPath) }()
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
		progress := newFileTransferProgress(ctx, output, attached, metadata)
		defer func() {
			operationErr = finishFileTransferPresentation(output, progress, operationErr)
		}()
		result, transferErr := app.ReceiveFile(ctx, attached, temporary, remotePath, progress.Report)
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
		if completeErr := progress.Complete(result.Size); completeErr != nil {
			return completeErr
		}
		if finishErr := progress.finish(); finishErr != nil {
			return finishErr
		}
		_, writeErr := fmt.Fprintf(output, "SHA-256: OK\nSaved: %s\n", localPath)
		return writeErr
	})
}

// runFileReceiveDirectory runs while the caller holds the one file-transfer
// lease, including remote tar probing, extraction, and final local rename.
func runFileReceiveDirectory(ctx context.Context, attached attachSession, remotePath, localPath string, output io.Writer) (operationErr error) {
	localPath, err := nextAvailableLocalDirectory(localPath)
	if err != nil {
		return err
	}
	staging, err := createLocalDirectoryStaging(localPath)
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(staging) }()
	metadata := map[string]any{"direction": "receive", "kind": "directory", "local_path": localPath, "remote_path": remotePath}
	if eventErr := reportFileTransferEvent(ctx, attached, session.EventFileTransferStarted, metadata); eventErr != nil {
		return eventErr
	}
	defer reportFailedFileTransfer(ctx, attached, metadata, &operationErr)
	progress := newFileTransferProgress(ctx, output, attached, metadata)
	defer func() { operationErr = finishFileTransferPresentation(output, progress, operationErr) }()
	result, transferErr := app.ReceiveDirectory(ctx, attached, remotePath, staging, progress.Report)
	if transferErr != nil {
		return transferErr
	}
	if err := replaceReceivedDirectory(staging, localPath); err != nil {
		return fmt.Errorf("install received directory %q: %w", localPath, err)
	}
	completed := copyFileTransferMetadata(metadata)
	completed["received"] = result.Size
	completed["total"] = result.Size
	completed["percent"] = float64(100)
	if eventErr := reportFileTransferEvent(ctx, attached, session.EventFileTransferCompleted, completed); eventErr != nil {
		return eventErr
	}
	if completeErr := progress.Complete(result.Size); completeErr != nil {
		return completeErr
	}
	if finishErr := progress.finish(); finishErr != nil {
		return finishErr
	}
	_, err = fmt.Fprintf(output, "Tar stream: complete\nSaved: %s\n", localPath)
	return err
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

// reportFailedFileTransfer preserves the existing failure-event contract for
// both file and directory transfers while keeping the lease owner active until
// cleanup has completed.
func reportFailedFileTransfer(ctx context.Context, attached attachSession, metadata map[string]any, operationErr *error) {
	if operationErr == nil || *operationErr == nil {
		return
	}
	failureCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	failureMetadata := copyFileTransferMetadata(metadata)
	failureMetadata["error"] = (*operationErr).Error()
	_ = reportFileTransferEvent(failureCtx, attached, session.EventFileTransferFailed, failureMetadata)
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

const fileTransferProgressBarWidth = 20

// fileTransferSnapshot is the one local measurement used by both the CLI and
// FILE_TRANSFER_PROGRESS metadata, keeping their reported speeds consistent.
type fileTransferSnapshot struct {
	transferred int64
	total       int64
	percent     float64
	speed       float64
}

// fileTransferReporter renders confirmed protocol progress locally and reports
// the same snapshot to the Session event stream.
type fileTransferReporter struct {
	ctx      context.Context
	output   io.Writer
	attached attachSession
	base     map[string]any
	started  time.Time
	rendered bool
	finished bool
	final    fileTransferSnapshot
	hasFinal bool
}

func newFileTransferProgress(ctx context.Context, output io.Writer, attached attachSession, base map[string]any) *fileTransferReporter {
	return &fileTransferReporter{
		ctx:      ctx,
		output:   output,
		attached: attached,
		base:     base,
		started:  time.Now(),
	}
}

// Report renders and publishes one confirmed transfer progress update.
func (p *fileTransferReporter) Report(transferred, total int64) error {
	if transferred == 0 && total > 0 {
		// The initial local frame is not confirmed transfer progress, so it must
		// not add a FILE_TRANSFER_PROGRESS event for other Session observers.
		return p.render(fileTransferSnapshot{total: total})
	}
	snapshot := p.snapshot(transferred, total)
	if snapshot.percent < 100 {
		if err := p.render(snapshot); err != nil {
			return err
		}
	} else {
		p.final = snapshot
		p.hasFinal = true
	}
	metadata := copyFileTransferMetadata(p.base)
	metadata["total"] = total
	if p.base["direction"] == "receive" {
		metadata["received"] = transferred
	} else {
		metadata["sent"] = transferred
	}
	metadata["percent"] = snapshot.percent
	metadata["speed"] = snapshot.speed
	return reportFileTransferEvent(p.ctx, p.attached, session.EventFileTransferProgress, metadata)
}

// Start renders a non-empty transfer's local zero-progress frame before its
// first payload. It is presentation-only and does not publish an event.
func (p *fileTransferReporter) Start(total int64) error {
	if total <= 0 {
		return nil
	}
	return p.Report(0, total)
}

// Complete ensures every successful transfer has rendered its final 100%
// state, including a zero-byte transfer.
func (p *fileTransferReporter) Complete(total int64) error {
	if p.hasFinal && p.final.transferred == total && p.final.total == total {
		return p.render(p.final)
	}
	return p.render(p.snapshot(total, total))
}

func (p *fileTransferReporter) snapshot(transferred, total int64) fileTransferSnapshot {
	if transferred < 0 {
		transferred = 0
	}
	if total < 0 {
		total = 0
	}
	percent := float64(100)
	if total > 0 {
		percent = float64(transferred) * 100 / float64(total)
	}
	percent = min(100, max(0, percent))
	speed := float64(0)
	if elapsed := time.Since(p.started).Seconds(); elapsed > 0 {
		speed = float64(transferred) / elapsed
	}
	return fileTransferSnapshot{transferred: transferred, total: total, percent: percent, speed: speed}
}

func (p *fileTransferReporter) render(snapshot fileTransferSnapshot) error {
	// Clear to the end of the line after every in-place update. ETA and speed
	// text can shrink between reports; a carriage return alone would leave the
	// tail of the previous frame visible (for example, "ETA 25ss").
	if _, err := fmt.Fprintf(p.output, "\r%s\x1b[K", formatFileTransferProgress(snapshot)); err != nil {
		return err
	}
	p.rendered = true
	return nil
}

func (p *fileTransferReporter) finish() error {
	if !p.rendered || p.finished {
		return nil
	}
	p.finished = true
	_, err := fmt.Fprintln(p.output)
	return err
}

func formatFileTransferProgress(snapshot fileTransferSnapshot) string {
	filled := int(snapshot.percent * fileTransferProgressBarWidth / 100)
	filled = min(fileTransferProgressBarWidth, max(0, filled))
	bar := strings.Repeat("#", filled) + strings.Repeat("-", fileTransferProgressBarWidth-filled)
	text := fmt.Sprintf("[%s] %5.1f%%  %s / %s", bar, snapshot.percent, formatFileTransferBytes(snapshot.transferred), formatFileTransferBytes(snapshot.total))
	if snapshot.transferred > 0 && snapshot.speed > 0 && !math.IsNaN(snapshot.speed) && !math.IsInf(snapshot.speed, 0) {
		text += "  " + formatFileTransferSpeed(snapshot.speed)
	} else if snapshot.transferred > 0 && snapshot.percent < 100 {
		text += "  0 B/s"
	}
	if snapshot.transferred > 0 && snapshot.percent < 100 {
		text += "  " + formatFileTransferETA(snapshot.transferred, snapshot.total, snapshot.speed)
	}
	return text
}

func formatFileTransferBytes(value int64) string {
	return formatFileTransferQuantity(float64(value))
}

func formatFileTransferSpeed(value float64) string {
	return formatFileTransferQuantity(value) + "/s"
}

func formatFileTransferQuantity(value float64) string {
	if value <= 0 || math.IsNaN(value) || math.IsInf(value, 0) {
		return "0 B"
	}
	if value < 1024 {
		return fmt.Sprintf("%.0f B", value)
	}
	units := []string{"KiB", "MiB", "GiB"}
	amount := value
	for _, unit := range units {
		amount /= 1024
		if amount < 1024 || unit == units[len(units)-1] {
			switch {
			case amount < 10:
				return fmt.Sprintf("%.2f %s", amount, unit)
			case amount < 100:
				return fmt.Sprintf("%.1f %s", amount, unit)
			default:
				return fmt.Sprintf("%.0f %s", amount, unit)
			}
		}
	}
	return "0 B"
}

func formatFileTransferETA(transferred, total int64, speed float64) string {
	if total <= transferred || speed <= 0 || math.IsNaN(speed) || math.IsInf(speed, 0) {
		return "ETA --"
	}
	seconds := float64(total-transferred) / speed
	if seconds < 0 || math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds > float64(math.MaxInt64) {
		return "ETA --"
	}
	wholeSeconds := int64(seconds)
	if wholeSeconds < 60 {
		return fmt.Sprintf("ETA %ds", wholeSeconds)
	}
	return fmt.Sprintf("ETA %dm %02ds", wholeSeconds/60, wholeSeconds%60)
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

func finishFileTransferPresentation(output io.Writer, progress *fileTransferReporter, operationErr error) error {
	if finishErr := progress.finish(); finishErr != nil && operationErr == nil {
		operationErr = finishErr
	}
	if operationErr == nil {
		return nil
	}
	if errors.Is(operationErr, context.Canceled) {
		_, _ = fmt.Fprintln(output, "Transfer cancelled.")
	} else {
		_, _ = fmt.Fprintf(output, "Transfer failed: %v\n", operationErr)
	}
	return operationErr
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

// replaceReceivedDirectory refuses a destination that appears after selection.
// Directory rename is same-parent and atomic when the destination remains
// absent; the explicit check avoids replacing an empty directory on platforms
// where rename otherwise permits that operation.
func replaceReceivedDirectory(stagingPath, destinationPath string) error {
	_, err := os.Lstat(destinationPath)
	if errors.Is(err, os.ErrNotExist) {
		return os.Rename(stagingPath, destinationPath)
	}
	if err != nil {
		return fmt.Errorf("inspect destination: %w", err)
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

// nextAvailableLocalDirectory applies the established _N collision policy to
// directory destinations. Unlike single-file receive, an existing directory
// is an expected collision rather than an argument-type error.
func nextAvailableLocalDirectory(wanted string) (string, error) {
	directory, filename := filepath.Split(filepath.Clean(wanted))
	if filename == "." || filename == "" {
		return "", fmt.Errorf("local directory destination %q must name a directory", wanted)
	}
	for sequence := 0; ; sequence++ {
		candidate := directory + filename
		if sequence > 0 {
			candidate = directory + filename + "_" + strconv.Itoa(sequence)
		}
		_, err := os.Lstat(candidate)
		if errors.Is(err, os.ErrNotExist) {
			return candidate, nil
		}
		if err != nil {
			return "", fmt.Errorf("inspect local directory destination %q: %w", candidate, err)
		}
	}
}

func createLocalDirectoryStaging(destination string) (string, error) {
	directory, name := filepath.Split(destination)
	staging, err := os.MkdirTemp(directory, "."+name+".cterm-part-*")
	if err != nil {
		return "", fmt.Errorf("create directory staging beside %q: %w", destination, err)
	}
	return staging, nil
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
