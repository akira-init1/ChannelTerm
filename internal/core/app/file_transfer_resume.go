package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/akira-init1/ChannelTerm/internal/core/session"
)

// ErrTransferPublicationUncertain requires human inspection of the destination.
// A saved publishing phase must never trigger another rename or extraction.
var ErrTransferPublicationUncertain = errors.New("transfer publication is uncertain; inspect the destination before starting a new transfer")

// RunResumableTransfer resumes one explicitly selected checkpoint under the
// caller's existing Host Lease. It never reconnects, acquires authority, or
// retries rejected writes. Each invocation reloads and exclusively locks state.
// Cancellation retains snapshots and only durably acknowledged offsets.
func RunResumableTransfer(ctx context.Context, terminal FileTransferSession, root, id string, progress FileTransferProgress) (TransferCheckpoint, error) {
	lock, err := lockTransferCheckpoint(root, id)
	if err != nil {
		return TransferCheckpoint{}, err
	}
	defer lock.Close()
	c, err := ReadTransferCheckpoint(root, id)
	if err != nil {
		return c, err
	}
	if c.Phase == "publishing" {
		return c, ErrTransferPublicationUncertain
	}
	if c.Phase == "completed" {
		return c, errors.New("transfer is already completed")
	}
	if terminal == nil {
		return c, errors.New("file transfer session must not be nil")
	}
	if err := fileTransferCancellationCheckpoint(ctx, terminal, nil); err != nil {
		return c, err
	}
	// The wrapper selects the saved wire mode while forwarding cancellation,
	// contextual writes and unsafe-shell abort capabilities to the same owner.
	wrapped := checkpointSession{FileTransferSession: terminal, wire: c.Wire}
	dir := filepath.Join(root, id)
	err = WithFileTransferShell(ctx, wrapped, func(scoped FileTransferSession) error {
		p, err := newFileProtocol(ctx, scoped)
		if err != nil {
			return err
		}
		if err := p.prepareCheckpoint(ctx, dir, &c); err != nil {
			return err
		}
		if err := p.checkCheckpointPublication(ctx, c); err != nil {
			return err
		}
		if err := p.streamCheckpoint(ctx, dir, &c, progress); err != nil {
			return err
		}
		return p.publishCheckpoint(ctx, dir, &c)
	})
	return c, err
}

type checkpointSession struct {
	FileTransferSession
	wire FileTransferWireEncoding
}

func (s checkpointSession) FileTransferWireEncoding() FileTransferWireEncoding { return s.wire }
func (s checkpointSession) FileTransferCancellationCheckpoint(ctx context.Context) error {
	return fileTransferCancellationCheckpoint(ctx, s.FileTransferSession, nil)
}
func (s checkpointSession) WriteContext(ctx context.Context, r session.WriteRequest) (int, error) {
	if contextual, ok := s.FileTransferSession.(interface {
		WriteContext(context.Context, session.WriteRequest) (int, error)
	}); ok {
		return contextual.WriteContext(ctx, r)
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return s.Write(r)
}
func (s checkpointSession) AbortFileTransferRecovery(ctx context.Context) error {
	if aborter, ok := s.FileTransferSession.(fileTransferRecoveryAborter); ok {
		return aborter.AbortFileTransferRecovery(ctx)
	}
	return ErrFileTransferRecoveryUnconfirmed
}

func (p *fileProtocol) publishCheckpoint(ctx context.Context, dir string, c *TransferCheckpoint) error {
	if c.Direction == "send" {
		// Extract into the owned directory before marking publication uncertain.
		body := checkpointRemoteGuard(*c)
		if c.Kind == RemotePathDirectory {
			body += " && [ ! -L \"$d/tree\" ] && rm -rf \"$d/tree\" && mkdir \"$d/tree\" && tar -x -f \"$d/payload\" -C \"$d/tree\""
		}
		if err := p.checkpointCommand(ctx, body); err != nil {
			return err
		}
		c.Phase = "publishing"
		if err := saveTransferCheckpoint(dir, *c); err != nil {
			return err
		}
		target, _ := quoteRemotePath(c.RemotePath)
		if c.Kind == RemotePathDirectory {
			// -T prevents a racing directory from turning the move into nesting;
			// -n refuses replacement. Unsupported mv implementations fail closed.
			body = fmt.Sprintf("%s && mv -nT \"$d/tree\" %s && [ ! -e \"$d/tree\" ]", checkpointRemoteGuard(*c), target)
		} else {
			// Hard-link publication is exclusive and same-filesystem; the staging
			// link remains until the acknowledged completion record is durable.
			body = fmt.Sprintf("%s && ln -T \"$d/payload\" %s", checkpointRemoteGuard(*c), target)
		}
		if err := p.checkpointCommand(ctx, body); err != nil {
			return errors.Join(ErrTransferPublicationUncertain, err)
		}
	} else {
		staging, err := prepareCheckpointDestination(ctx, dir, c)
		if err != nil {
			return err
		}
		c.Phase = "publishing"
		if err := saveTransferCheckpoint(dir, *c); err != nil {
			_ = os.RemoveAll(staging)
			return err
		}
		if err := publishTransferPath(staging, c.LocalPath); err != nil {
			return errors.Join(ErrTransferPublicationUncertain, err)
		}
	}
	c.Phase = "completed"
	if err := saveTransferCheckpoint(dir, *c); err != nil {
		return errors.Join(ErrTransferPublicationUncertain, err)
	}
	// Keep the small completion receipt. Payload cleanup never changes the
	// published destination; failure leaves only app-owned staging artifacts.
	if c.Direction == "send" || c.Kind == RemotePathDirectory {
		body := checkpointRemoteGuard(*c) + " && rm -f \"$d/payload\" \"$d/recover\" \"$d/owner\" && rmdir \"$d\""
		if err := p.checkpointCommand(ctx, body); err != nil {
			return fmt.Errorf("transfer completed, staging cleanup failed: %w", err)
		}
	}
	if err := os.Remove(filepath.Join(dir, "payload")); err != nil {
		return fmt.Errorf("transfer completed, local snapshot cleanup failed: %w", err)
	}
	return nil
}

// prepareCheckpointDestination writes beside the target so final publication
// stays on one filesystem. Existing targets select an unused _N sibling.
func prepareCheckpointDestination(ctx context.Context, dir string, c *TransferCheckpoint) (staging string, err error) {
	wanted := c.LocalPath
	for n := 0; ; n++ {
		candidate := wanted
		if n > 0 {
			ext := filepath.Ext(wanted)
			if c.Kind == RemotePathDirectory {
				ext = ""
			}
			candidate = wanted[:len(wanted)-len(ext)] + fmt.Sprintf("_%d", n) + ext
		}
		if _, statErr := os.Lstat(candidate); errors.Is(statErr, os.ErrNotExist) {
			c.LocalPath = candidate
			break
		} else if statErr != nil {
			return "", statErr
		}
	}
	f, err := os.Open(filepath.Join(dir, "payload"))
	if err != nil {
		return "", err
	}
	defer f.Close()
	defer func() {
		if err != nil && staging != "" {
			_ = os.RemoveAll(staging)
		}
	}()
	if c.Kind == RemotePathDirectory {
		staging, err = os.MkdirTemp(filepath.Dir(c.LocalPath), ".channelterm-publish-"+c.ID+"-")
		if err != nil {
			return staging, err
		}
		err = extractDirectoryTar(transferContextReader{ctx: ctx, reader: f}, staging)
		return staging, err
	}
	out, err := os.CreateTemp(filepath.Dir(c.LocalPath), ".channelterm-publish-"+c.ID+"-")
	if err != nil {
		return "", err
	}
	staging = out.Name()
	_, err = io.Copy(&transferContextWriter{ctx: ctx, writer: out}, f)
	if err == nil {
		err = out.Sync()
	}
	err = errors.Join(err, out.Close())
	return staging, err
}

type transferContextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r transferContextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}
