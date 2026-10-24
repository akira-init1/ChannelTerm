package command

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/core/app"
	"github.com/akira-init1/ChannelTerm/internal/core/config"
	"github.com/akira-init1/ChannelTerm/internal/core/session"
)

func fileCheckpointRoot(dependencies fileCommandDependencies) (string, error) {
	if dependencies.transferRoot != nil {
		return dependencies.transferRoot()
	}
	return config.DefaultTransferPath()
}

// Bind durable intent to the Host URL and physical endpoint, not a recycled
// short reference or a Session ID that changes when the Host restarts. Content
// and prefix verification remains mandatory; a port name is not device identity.
func fileCheckpointPeer(ctx context.Context, options fileOptions, dependencies fileCommandDependencies) (string, string, error) {
	endpoint, err := url.Parse(options.endpoint)
	if err != nil || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" {
		return "", "", errors.New("checkpoint endpoint must not contain credentials, query, or fragment")
	}
	listed, err := dependencies.listSessions(ctx, options.endpoint)
	if err != nil {
		return "", "", err
	}
	var selected *mcpListedSession
	for _, item := range listed {
		if item.State != "open" {
			continue
		}
		if options.session != "" && options.session != item.ID && options.session != item.Reference {
			continue
		}
		if selected != nil {
			return "", "", ErrFileSessionAmbiguous
		}
		copy := item
		selected = &copy
	}
	if selected == nil {
		return "", "", ErrFileSessionRequired
	}
	if selected.Transport != "serial" || selected.Endpoint == "" {
		return "", "", errors.New("resumable transfer requires an open serial Session")
	}
	binding, _ := json.Marshal([]string{strings.TrimRight(endpoint.String(), "/"), selected.Transport, selected.Endpoint})
	return string(binding), selected.ID, nil
}

func runNewResumableFile(ctx context.Context, direction, localPath, remotePath string, options fileOptions, output io.Writer, dependencies fileCommandDependencies) error {
	peer, id, err := fileCheckpointPeer(ctx, options, dependencies)
	if err != nil {
		return err
	}
	root, err := fileCheckpointRoot(dependencies)
	if err != nil {
		return err
	}
	options.session = id
	c := app.TransferCheckpoint{Direction: direction, LocalPath: localPath, RemotePath: remotePath, Peer: peer, Wire: options.wire}
	return executeFileCheckpoint(ctx, root, c, options, output, dependencies, true)
}

func runFileResume(ctx context.Context, args []string, output io.Writer, dependencies fileCommandDependencies) error {
	if len(args) == 0 {
		return errors.New("resume requires JOB_ID and --session SESSION")
	}
	if args[0] == "--help" || args[0] == "-h" {
		writeFileUsage(output)
		return nil
	}
	for _, arg := range args[1:] {
		if arg == "--wire" || strings.HasPrefix(arg, "--wire=") || arg == "--resumable" || strings.HasPrefix(arg, "--resumable=") {
			return errors.New("resume uses its saved wire encoding and checkpoint; omit --wire and --resumable")
		}
	}
	options, err := parseFileOptions("file resume", args[1:], output, writeFileUsage)
	if err != nil || options.help {
		return err
	}
	if options.session == "" {
		return errors.New("resume requires an explicit --session; no automatic selection or reconnect")
	}
	root, err := fileCheckpointRoot(dependencies)
	if err != nil {
		return err
	}
	c, err := app.ReadTransferCheckpoint(root, args[0])
	if err != nil {
		return err
	}
	if c.Phase == "publishing" {
		return app.ErrTransferPublicationUncertain
	}
	peer, id, err := fileCheckpointPeer(ctx, options, dependencies)
	if err != nil {
		return err
	}
	if peer != c.Peer {
		return errors.New("selected Host/serial endpoint differs from the saved transfer")
	}
	options.session = id
	return executeFileCheckpoint(ctx, root, c, options, output, dependencies, false)
}

func executeFileCheckpoint(ctx context.Context, root string, c app.TransferCheckpoint, options fileOptions, output io.Writer, dependencies fileCommandDependencies, create bool) (err error) {
	output = machineFileOutput(output, options.json)
	attached, id, err := attachFileSession(ctx, options, output, dependencies)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, attached.Close()) }()
	err = withFileTransferLease(ctx, attached, id, func(ctx context.Context) (operationErr error) {
		source, target := c.LocalPath, c.RemotePath
		if c.Direction == "receive" {
			source, target = c.RemotePath, c.LocalPath
		}
		metadata := fileTransferPathMetadata(c.Direction, source, target)
		metadata["kind"] = string(c.Kind)
		metadata["total"] = c.Size
		if err := reportFileTransferEvent(ctx, attached, session.EventFileTransferStarted, metadata); err != nil {
			return err
		}
		published := false
		defer func() {
			if !published {
				reportFailedFileTransfer(ctx, attached, metadata, &operationErr)
			}
		}()
		progress := newFileTransferProgress(ctx, output, attached, metadata)
		progress.resumed = c.Offset
		defer func() { operationErr = finishFileTransferPresentation(output, progress, operationErr) }()
		terminal := internalFileTransferSession{attachSession: attached, wire: c.Wire}
		if create {
			// Snapshot preparation may be large. Keep it inside the same visible
			// Host operation, with cancellation available before any device bytes.
			err := withFilePreparationCancellation(ctx, terminal, func(snapshotCtx context.Context) error {
				var createErr error
				c, createErr = app.CreateTransferCheckpoint(snapshotCtx, root, c.Direction, c.LocalPath, c.RemotePath, c.Peer, c.Wire)
				return createErr
			})
			if err != nil {
				return err
			}
		}
		if machine, ok := output.(*fileMachineWriter); ok {
			if err := machine.emit(map[string]any{"type": "checkpoint", "checkpoint_id": c.ID}); err != nil {
				return err
			}
		} else {
			fmt.Fprintf(output, "Resume checkpoint: %s\n", c.ID)
		}
		result, err := app.RunResumableTransfer(ctx, terminal, root, c.ID, fileTransferProgressWithCancellation(progress.Report, dependencies.transferCancelRequested))
		if err != nil && result.Phase != "completed" {
			return err
		}
		cleanupErr := err
		published = result.Phase == "completed"
		resolved := result.RemotePath
		if c.Direction == "receive" {
			resolved = result.LocalPath
		}
		completed := copyFileTransferMetadata(metadata)
		completed["kind"] = string(result.Kind)
		setResolvedTransferPathMetadata(completed, target, resolved, target != resolved)
		completed["total"] = result.Size
		completed["percent"] = float64(100)
		completed["sha256"] = result.SHA256
		completed["local_sha256"] = result.SHA256
		completed["remote_sha256"] = result.SHA256
		key := "sent"
		if c.Direction == "receive" {
			key = "received"
		}
		completed[key] = result.Size
		if err := reportFileTransferEvent(ctx, attached, session.EventFileTransferCompleted, completed); err != nil {
			return err
		}
		if err := progress.Complete(result.Size); err != nil {
			return err
		}
		if err := progress.finish(); err != nil {
			return err
		}
		return errors.Join(writeFileTransferVerificationSummary(output, result.SHA256, resolved), cleanupErr)
	})
	if err != nil && c.ID != "" {
		if saved, checkpointErr := app.ReadTransferCheckpoint(root, c.ID); checkpointErr == nil && saved.Phase != "completed" {
			return fmt.Errorf("checkpoint %s retained; use file jobs and explicitly resume after checking the Session: %w", c.ID, err)
		}
	}
	return err
}

// Local snapshot I/O has no device block to finish. Poll the authoritative
// cancellation checkpoint while copying; a confirmed cancel stops that copy.
// The worker is joined before returning and the outer Lease remains authoritative.
func withFilePreparationCancellation(ctx context.Context, terminal internalFileTransferSession, prepare func(context.Context) error) error {
	workCtx, cancel := context.WithCancel(ctx)
	result := make(chan error, 1)
	go func() {
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()
		for {
			if err := terminal.FileTransferCancellationCheckpoint(workCtx); err != nil {
				if workCtx.Err() != nil {
					result <- nil
				} else {
					cancel()
					result <- err
				}
				return
			}
			select {
			case <-workCtx.Done():
				result <- nil
				return
			case <-ticker.C:
			}
		}
	}()
	err := prepare(workCtx)
	cancel()
	return errors.Join(err, <-result)
}

// fileCheckpointSummary intentionally omits local paths and Host binding from
// the machine listing used by the GUI. Job IDs are validated again on execution.
type fileCheckpointSummary struct {
	ID        string             `json:"id"`
	Name      string             `json:"name"`
	Direction string             `json:"direction"`
	Kind      app.RemotePathKind `json:"kind"`
	Offset    int64              `json:"offset"`
	Total     int64              `json:"total"`
	Phase     string             `json:"phase"`
}

func runFileJobs(ctx context.Context, args []string, output io.Writer, dependencies fileCommandDependencies) error {
	options, err := parseFileOptions("file jobs", args, output, writeFileUsage)
	if err != nil || options.help {
		return err
	}
	root, err := fileCheckpointRoot(dependencies)
	if err != nil {
		return err
	}
	jobs, err := app.ListTransferCheckpoints(root)
	if err != nil {
		return err
	}
	peer := ""
	if options.session != "" {
		peer, _, err = fileCheckpointPeer(ctx, options, dependencies)
		if err != nil {
			return err
		}
	}
	rows := []fileCheckpointSummary{}
	for _, c := range jobs {
		if peer != "" && peer != c.Peer {
			continue
		}
		name := filepath.Base(c.LocalPath)
		if c.Direction == "receive" {
			name = path.Base(c.RemotePath)
		}
		rows = append(rows, fileCheckpointSummary{c.ID, name, c.Direction, c.Kind, c.Offset, c.Size, c.Phase})
	}
	if options.json {
		return json.NewEncoder(output).Encode(map[string]any{"jobs": rows})
	}
	for _, c := range rows {
		fmt.Fprintf(output, "%s  %s  %s  %d/%d B  %s  %s\n", c.ID, c.Direction, c.Kind, c.Offset, c.Total, c.Phase, c.Name)
	}
	if len(rows) == 0 {
		fmt.Fprintln(output, "No resumable transfers.")
	}
	return nil
}
