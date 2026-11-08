package app

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math"
	"os"
	"path"
	"path/filepath"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/core/session"
)

// managedTransferSession binds every command to the original registration and
// private lease. Closing a blocked write is conservative: never replay an
// uncertain terminal payload into a replacement or recovered Session.
type managedTransferSession struct {
	app        *Application
	ref, owner string
	terminal   session.Session
}

// ReadRecent reads only the captured registration's output.
func (s managedTransferSession) ReadRecent(ctx context.Context, limit int) (session.OutputChunk, error) {
	return s.app.ReadSession(ctx, s.ref, nil, limit)
}

// ReadOutput retains independent transfer cursors and cancellation.
func (s managedTransferSession) ReadOutput(ctx context.Context, cursor session.OutputCursor, limit int) (session.OutputChunk, error) {
	return s.app.ReadSession(ctx, s.ref, &cursor, limit)
}

// Write satisfies the protocol's synchronous leased-write capability.
func (s managedTransferSession) Write(q session.WriteRequest) (int, error) {
	return s.WriteContext(context.Background(), q)
}

// WriteContext closes the original stream if cancellation interrupts a write
// whose remote byte count cannot be established. It joins its close callback.
func (s managedTransferSession) WriteContext(ctx context.Context, q session.WriteRequest) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	done := make(chan struct{})
	stop := context.AfterFunc(ctx, func() { _ = s.terminal.Close(); close(done) })
	q.Actor = session.ActorSystem
	n, err := s.app.WriteSessionWithLease(ctx, s.ref, s.owner, q)
	if !stop() {
		<-done
		err = errors.Join(err, ctx.Err(), ErrFileTransferRecoveryUnconfirmed)
	}
	return n, err
}

// FileTransferWireEncoding keeps software-flow-control-safe Base64 framing.
func (s managedTransferSession) FileTransferWireEncoding() FileTransferWireEncoding {
	return FileTransferWireBase64
}

// FileTransferCancellationCheckpoint honors existing attachment cancellation.
func (s managedTransferSession) FileTransferCancellationCheckpoint(ctx context.Context) error {
	action, err := s.app.FileTransferCheckpoint(ctx, s.ref, s.owner)
	if err != nil {
		return err
	}
	if action == FileTransferCancel {
		return context.Canceled
	}
	return nil
}

// AbortFileTransferRecovery removes only the originally captured registration.
func (s managedTransferSession) AbortFileTransferRecovery(context.Context) error {
	_, err := s.app.CloseSession(s.ref)
	return err
}

func (r *transferRun) withSerial(ctx context.Context, work func(context.Context, FileTransferSession) error) (err error) {
	q, a := r.request, r.app
	terminal, err := a.session(q.SessionID)
	if err != nil {
		return err
	}
	owner := "transfer-" + rand.Text()
	lease, err := a.AcquireLeaseForClient(ctx, q.SessionID, owner, LeaseTypeFileTransfer)
	if err != nil {
		return err
	}
	r.update(func(s *TransferStatus) { s.SessionTransferID = lease.TransferID })
	r.reportProgress = func(s TransferStatus) error {
		return a.ReportFileTransferEventForClient(ctx, q.SessionID, owner, lease.TransferID, session.EventFileTransferProgress, "system", map[string]any{"managed_transfer_id": s.ID, "current_file": s.CurrentFile, "transferred": s.CurrentBytes, "total": s.CurrentTotal, "files_verified": s.FilesVerified, "files_total": s.FilesTotal})
	}
	worker, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	stop := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				if _, err := a.RenewLease(q.SessionID, owner); err != nil {
					cancel()
					return
				}
			}
		}
	}()
	defer func() {
		r.reportProgress = nil
		close(stop)
		<-done
		cancel()
		if errors.Is(err, ErrFileTransferRecoveryUnconfirmed) {
			_, closeErr := a.CloseSession(q.SessionID)
			err = errors.Join(err, closeErr)
			return
		}
		s := r.snapshot()
		metadata := map[string]any{"source_path": q.Sources[0], "requested_path": q.Destination, "resolved_path": s.ResolvedPath, "renamed": s.ResolvedPath != q.Destination, "sha256": s.SHA256, "checksum_scope": s.ChecksumScope, "files_verified": s.FilesVerified, "files_total": s.FilesTotal, "managed_transfer_id": s.ID, "directory_mode": s.WireMode}
		typ := session.EventFileTransferCompleted
		if err != nil {
			typ = session.EventFileTransferFailed
			metadata["error"] = err.Error()
		}
		reportErr := a.ReportFileTransferEventForClient(ctx, q.SessionID, owner, lease.TransferID, typ, "system", metadata)
		releaseErr := a.ReleaseLease(q.SessionID, owner)
		err = errors.Join(err, reportErr, releaseErr)
	}()
	if err := a.ReportFileTransferEventForClient(ctx, q.SessionID, owner, lease.TransferID, session.EventFileTransferStarted, "system", map[string]any{"managed_transfer_id": r.snapshot().ID, "direction": q.Direction}); err != nil {
		return err
	}
	scoped := managedTransferSession{app: a, ref: q.SessionID, owner: owner, terminal: terminal}
	return WithFileTransferShell(worker, scoped, func(terminal FileTransferSession) error { return work(worker, terminal) })
}

func (r *transferRun) progress(name string) FileTransferProgress {
	return func(n, total int64) error {
		r.update(func(s *TransferStatus) { s.CurrentFile, s.CurrentBytes, s.CurrentTotal = name, n, total })
		return r.emitProgress()
	}
}

func (r *transferRun) emitProgress() error {
	if r.reportProgress != nil && time.Since(r.lastProgress) >= time.Second {
		r.lastProgress = time.Now()
		return r.reportProgress(r.snapshot())
	}
	return nil
}

func (r *transferRun) sendSerial(ctx context.Context, terminal FileTransferSession, payload string, single bool) error {
	q := r.request
	r.update(func(s *TransferStatus) { s.WireMode, s.ResolvedPath = "files", q.Destination })
	if single {
		f, err := os.Open(payload)
		if err != nil {
			return err
		}
		info, err := f.Stat()
		if err != nil {
			_ = f.Close()
			return err
		}
		result, copyErr := SendFile(ctx, terminal, f, info.Size(), q.Destination, r.progress(filepath.Base(payload)))
		if err := errors.Join(copyErr, f.Close()); err != nil {
			return err
		}
		r.update(func(s *TransferStatus) {
			s.ResolvedPath, s.Bytes, s.SHA256, s.ChecksumScope = result.RemotePath, result.Size, result.SHA256, "file"
			s.FilesVerified = 1
			if q.Mode == "tar" || q.Mode == "tar.gz" {
				s.ChecksumScope, s.WireMode, s.FilesTotal = "archive", q.Mode, 1
			}
		})
		return nil
	}
	m, err := localDirectoryManifest(ctx, payload)
	if err != nil {
		return err
	}
	p, err := newFileProtocol(ctx, terminal)
	if err != nil {
		return err
	}
	useTar := false
	if q.Mode == "auto" && m.result("").Files >= 16 {
		// Probe before payload. An unavailable command is a normal files fallback;
		// cancellation or a broken control channel must still fail the task.
		err = p.directoryFilesCommand(ctx, "command -v tar >/dev/null")
		var remote *fileTransferRemoteError
		if err != nil && !errors.As(err, &remote) {
			return err
		}
		useTar = err == nil
		if useTar {
			plan, err := makeDirectoryPlanContext(ctx, payload)
			if err != nil {
				return err
			}
			f := m.footprint()
			if plan.size < 0 || plan.size > math.MaxInt64-f.bytes {
				return ErrTransferSpaceUnavailable
			}
			f.bytes += plan.size
			f.entries++
			err = p.checkRemoteTransferSpace(ctx, q.Destination, f)
			if errors.Is(err, ErrTransferSpaceInsufficient) {
				useTar = false
			} else if err != nil {
				return err
			}
		}
	}
	if useTar {
		return r.sendSerialBundle(ctx, terminal, p, payload, m)
	}
	result, err := SendDirectoryFiles(ctx, terminal, payload, q.Destination, func(name string, verified, total int, n, size int64) error {
		r.update(func(s *TransferStatus) {
			s.CurrentFile, s.FilesVerified, s.FilesTotal, s.Bytes, s.TotalBytes = name, verified, total, n, size
			s.CurrentBytes, s.CurrentTotal = n, size
		})
		return r.emitProgress()
	})
	if err != nil {
		return err
	}
	r.update(func(s *TransferStatus) {
		s.ResolvedPath, s.SHA256, s.ChecksumScope = result.RemotePath, result.SHA256, "manifest"
	})
	return nil
}

func (r *transferRun) sendSerialBundle(ctx context.Context, terminal FileTransferSession, p *fileProtocol, source string, manifest directoryManifest) (err error) {
	destination, quoted, err := p.selectAvailableRemotePath(ctx, r.request.Destination)
	if err != nil {
		return err
	}
	stage := path.Join(path.Dir(destination), ".cterm-managed-"+rand.Text())
	// Probe the same non-replacing/non-nesting rename contract used by per-file
	// directories before sending an archive. Some embedded mv builds lack -T.
	probe, _ := quoteRemotePath(stage + "-probe")
	parent, _ := quoteRemotePath(path.Dir(destination))
	command := fmt.Sprintf(`p=%s; umask 077; mkdir -p %s && mkdir "$p" && { if mkdir "$p/from" "$p/to" && { mv -nT "$p/from" "$p/to"; [ -d "$p/from" ] && [ ! -e "$p/to/from" ]; } && rmdir "$p/to" && mv -nT "$p/from" "$p/to" && [ ! -e "$p/from" ]; then rc=0; else rc=1; fi; rm -rf "$p"; exit "$rc"; }`, probe, parent)
	if err := p.directoryFilesCommand(ctx, command); err != nil {
		return fmt.Errorf("TAR publication requires non-replacing mv -nT: %w", err)
	}
	r.update(func(s *TransferStatus) {
		s.WireMode, s.Reason = "tar", "at least 16 files; remote tar and archive-plus-tree capacity available"
		s.ResolvedPath = destination
	})
	result, err := SendDirectory(ctx, terminal, source, stage, r.progress("archive"))
	if err != nil {
		return err
	}
	stage = result.RemotePath
	qs, _ := quoteRemotePath(stage)
	published := false
	defer func() {
		if !published {
			cleanup, cancel := fileTransferRecoveryContext()
			defer cancel()
			err = errors.Join(err, p.recoveryError(p.directoryFilesCommand(cleanup, "rm -rf "+qs)))
		}
	}()
	got, err := p.directoryManifest(ctx, stage)
	if err != nil {
		return err
	}
	if err := compareDirectoryManifests(manifest, got); err != nil {
		return err
	}
	if err := fileTransferCancellationCheckpoint(ctx, terminal, nil); err != nil {
		return err
	}
	if err := p.directoryFilesCommand(ctx, fmt.Sprintf("mv -nT %s %s && [ ! -e %s ]", qs, quoted, qs)); err != nil {
		return errors.Join(ErrTransferPublicationUncertain, err)
	}
	published = true
	verified := manifest.result(destination)
	r.update(func(s *TransferStatus) {
		s.Bytes, s.TotalBytes, s.FilesVerified, s.SHA256, s.ChecksumScope = verified.Bytes, verified.Bytes, verified.Files, verified.SHA256, "manifest"
	})
	return nil
}

type serialSource struct {
	remote    string
	directory bool
	manifest  directoryManifest
	size      int64
	digest    string
}

func (r *transferRun) receiveSerial(ctx context.Context, terminal FileTransferSession, tree, root string) (bool, error) {
	p, err := newFileProtocol(ctx, terminal)
	if err != nil {
		return false, err
	}
	var sources []serialSource
	footprint := transferFootprint{entries: 1}
	for _, remote := range r.request.Sources {
		kind, err := DetectRemotePath(ctx, terminal, remote)
		if err != nil {
			return false, err
		}
		s := serialSource{remote: remote, directory: kind == RemotePathDirectory}
		switch kind {
		case RemotePathDirectory:
			s.manifest, err = p.directoryManifest(ctx, remote)
			f := s.manifest.footprint()
			if f.bytes > math.MaxInt64-footprint.bytes {
				return false, ErrTransferSpaceUnavailable
			}
			footprint.bytes += f.bytes
			footprint.entries += f.entries
		case RemotePathFile:
			quoted, _ := quoteRemotePath(remote)
			err = p.beginDirectoryCommand(ctx, receiveInitCommand(p.token, quoted))
			if err == nil {
				var fields []string
				fields, err = p.expectPhase(ctx, "META")
				if err == nil {
					s.size, s.digest, err = parseVerifiedFile(fields)
				}
			}
			if s.size > math.MaxInt64-footprint.bytes {
				return false, ErrTransferSpaceUnavailable
			}
			footprint.bytes += s.size
			footprint.entries++
		default:
			err = fmt.Errorf("unsupported or missing remote source %q", remote)
		}
		if err != nil {
			return false, err
		}
		if footprint.bytes < 0 || footprint.entries > directoryManifestLimit {
			return false, ErrTransferSpaceUnavailable
		}
		sources = append(sources, s)
	}
	if err := r.checkReceiveSpace(tree, root, footprint); err != nil {
		return false, err
	}
	r.update(func(s *TransferStatus) { s.WireMode = "files" })
	single := len(sources) == 1 && !sources[0].directory
	for _, s := range sources {
		local := filepath.Join(tree, path.Base(s.remote))
		if s.directory && len(sources) == 1 {
			local = tree
		}
		if s.directory {
			if local != tree {
				if err := os.Mkdir(local, 0700); err != nil {
					return false, err
				}
			}
			_, err := ReceiveDirectoryFiles(ctx, terminal, s.remote, local, func(name string, verified, total int, n, size int64) error { return r.progress(name)(n, size) })
			if err != nil {
				return false, err
			}
			got, err := localDirectoryManifest(ctx, local)
			if err != nil {
				return false, err
			}
			if err := compareDirectoryManifests(s.manifest, got); err != nil {
				return false, err
			}
		} else {
			f, err := os.OpenFile(local, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
			if err != nil {
				return false, err
			}
			result, copyErr := ReceiveFile(ctx, terminal, f, s.remote, r.progress(path.Base(s.remote)))
			if err := errors.Join(copyErr, f.Sync(), f.Close()); err != nil {
				return false, err
			}
			n, digest, err := hashTransferFile(ctx, local, s.size)
			if err != nil {
				return false, err
			}
			if n != s.size || digest != s.digest || result.SHA256 != digest {
				return false, ErrFileTransferChecksumMismatch
			}
		}
	}
	return single, nil
}

func (r *transferRun) checkReceiveSpace(tree, root string, f transferFootprint) error {
	// Stage and final publication can share a volume. Charge their combined
	// footprint conservatively to both; archive output also needs tar headers.
	if f.bytes < 0 || f.bytes > (1<<63-1)/4 || f.entries > directoryManifestLimit {
		return ErrTransferSpaceUnavailable
	}
	peak := transferFootprint{bytes: f.bytes * 2, entries: f.entries * 2}
	if r.request.Mode == "tar" || r.request.Mode == "tar.gz" {
		peak.bytes = f.bytes*4 + f.entries*4096 + 1024*1024
		peak.entries += 2
	}
	if peak.bytes < 0 {
		return ErrTransferSpaceUnavailable
	}
	if err := checkLocalTransferSpace(tree, peak); err != nil {
		return err
	}
	return checkLocalTransferSpace(filepath.Join(root, r.request.Destination), peak)
}
