package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
)

func checkpointRemoteDirectory(c TransferCheckpoint) string {
	parent := "/tmp"
	if c.Direction == "send" {
		parent = path.Dir(c.RemotePath)
	}
	return path.Join(parent, ".channelterm-transfer-"+c.ID)
}

func checkpointRemotePayload(c TransferCheckpoint) string {
	if c.Direction == "receive" && c.Kind == RemotePathFile {
		return c.RemotePath
	}
	return path.Join(checkpointRemoteDirectory(c), "payload")
}

// Probe publication flags inside owned staging before spending time on payload
// transfer. In particular, -T must reject directory nesting and -n replacement.
func (p *fileProtocol) checkCheckpointPublication(ctx context.Context, c TransferCheckpoint) error {
	if c.Direction != "send" {
		return nil
	}
	body := checkpointRemoteGuard(c) + " && rm -rf \"$d/probe\" && mkdir \"$d/probe\" && "
	if c.Kind == RemotePathDirectory {
		// GNU versions differ on the exit status of a no-clobber skip. Verify
		// the filesystem outcome, then require a successful move to a free name.
		body += "mkdir \"$d/probe/from\" \"$d/probe/to\" && { mv -nT \"$d/probe/from\" \"$d/probe/to\" 2>/dev/null; [ -d \"$d/probe/from\" ] && [ -d \"$d/probe/to\" ] && [ ! -e \"$d/probe/to/from\" ]; } && rmdir \"$d/probe/to\" && mv -nT \"$d/probe/from\" \"$d/probe/to\" && [ ! -e \"$d/probe/from\" ] && rmdir \"$d/probe/to\""
	} else {
		body += ": > \"$d/probe/from\" && mkdir \"$d/probe/to\" && { if ln -T \"$d/probe/from\" \"$d/probe/to\" 2>/dev/null; then false; else :; fi; } && rmdir \"$d/probe/to\" && ln -T \"$d/probe/from\" \"$d/probe/to\" && rm \"$d/probe/from\" \"$d/probe/to\""
	}
	return p.checkpointCommand(ctx, body+" && rmdir \"$d/probe\"")
}

// checkpointCommand emits one terminal marker even on a failing shell step.
// All interpolated paths are quoted separately and IDs/digests are validated.
func (p *fileProtocol) checkpointCommand(ctx context.Context, body string) error {
	command := fmt.Sprintf("t='%s'; if ( %s ); then printf '\\n@CTERM:%%s:CHECK:OK\\n' \"$t\"; else printf '\\n@CTERM:%%s:ERROR:checkpoint\\n' \"$t\"; fi", p.token, body)
	if len(command) > 3500 {
		return errors.New("device path is too long for checkpoint framing")
	}
	if err := p.command(ctx, command); err != nil {
		return err
	}
	_, err := p.expect(ctx, "CHECK", "OK")
	return err
}

func (p *fileProtocol) checkpointMetadata(ctx context.Context, remote string) (int64, string, error) {
	quoted, err := quoteRemotePath(remote)
	if err != nil {
		return 0, "", err
	}
	if err := p.command(ctx, verifyCommand(p.token, quoted)); err != nil {
		return 0, "", err
	}
	event, err := p.expectPhase(ctx, "FINAL")
	if err != nil {
		return 0, "", err
	}
	return parseVerifiedFile(event)
}

// Remote staging is private and identified by a random job ID. An interrupted
// setup may reuse only a real directory with its matching ownership receipt.
func checkpointRemoteGuard(c TransferCheckpoint) string {
	d, _ := quoteRemotePath(checkpointRemoteDirectory(c))
	return fmt.Sprintf("d=%s; [ -d \"$d\" ] && [ ! -L \"$d\" ] && [ -f \"$d/owner\" ] && [ ! -L \"$d/owner\" ] && [ \"$(cat \"$d/owner\")\" = '%s' ]", d, c.ID)
}

func (p *fileProtocol) prepareCheckpoint(ctx context.Context, dir string, c *TransferCheckpoint) error {
	if c.Phase != "preparing" {
		return nil
	}
	if c.Direction == "send" {
		if !checkpointDigest.MatchString(c.SHA256) {
			return errors.New("missing upload snapshot digest")
		}
		selected, _, err := p.selectAvailableRemotePath(ctx, c.RemotePath)
		if err != nil {
			return err
		}
		c.RemotePath = selected
	} else {
		quoted, _ := quoteRemotePath(c.RemotePath)
		if err := p.command(ctx, remotePathKindCommand(p.token, quoted)); err != nil {
			return err
		}
		event, err := p.expectPhase(ctx, "KIND")
		if err != nil {
			return err
		}
		if len(event) != 1 {
			return ErrFileTransferProtocol
		}
		kind := RemotePathKind(event[0])
		if kind != RemotePathFile && kind != RemotePathDirectory {
			return errors.New("remote source must be a regular file or directory")
		}
		c.Kind = kind
	}
	// Persist resolved paths/kind before creating remote state.
	if err := saveTransferCheckpoint(dir, *c); err != nil {
		return err
	}
	if c.Direction == "send" || c.Kind == RemotePathDirectory {
		d, _ := quoteRemotePath(checkpointRemoteDirectory(*c))
		parent, _ := quoteRemotePath(path.Dir(checkpointRemoteDirectory(*c)))
		setup := fmt.Sprintf("umask 077; d=%s; mkdir -p %s && { if mkdir \"$d\" 2>/dev/null; then printf '%%s' '%s' > \"$d/owner\"; else %s; fi; }", d, parent, c.ID, checkpointRemoteGuard(*c))
		if err := p.checkpointCommand(ctx, setup); err != nil {
			return err
		}
		var create string
		if c.Direction == "send" {
			create = "(set -C; : > \"$d/payload\")"
		} else {
			source, _ := quoteRemotePath(c.RemotePath)
			create = fmt.Sprintf("[ ! -L \"$d/snapshot\" ] && tar -c -f \"$d/snapshot\" -C %s . && mv \"$d/snapshot\" \"$d/payload\"", source)
		}
		body := fmt.Sprintf("%s && [ ! -L \"$d/payload\" ] && { if [ ! -e \"$d/payload\" ]; then %s; else [ -f \"$d/payload\" ]; fi; }", checkpointRemoteGuard(*c), create)
		if err := p.checkpointCommand(ctx, body); err != nil {
			return err
		}
	}
	if c.Direction == "receive" {
		var err error
		c.Size, c.SHA256, err = p.checkpointMetadata(ctx, checkpointRemotePayload(*c))
		if err != nil {
			return err
		}
		payload := filepath.Join(dir, "payload")
		if info, err := os.Lstat(payload); err == nil && !info.Mode().IsRegular() {
			return errors.New("invalid local partial file")
		} else if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		f, err := os.OpenFile(payload, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		err = errors.Join(f.Sync(), f.Close())
		if err != nil {
			return err
		}
	}
	c.Phase = "transferring"
	return saveTransferCheckpoint(dir, *c)
}

// Prefix validation precedes trimming unacknowledged bytes. Only the owned
// partial payload is replaced; neither final destinations nor sources change.
func (p *fileProtocol) validateCheckpointPrefix(ctx context.Context, c TransferCheckpoint) error {
	if c.Direction != "send" {
		return nil
	}
	quoted, _ := quoteRemotePath(checkpointRemotePayload(c))
	prefix := fmt.Sprintf("{ dd bs=%d count=%d 2>/dev/null; dd bs=1 count=%d 2>/dev/null; }", FileTransferChunkSize, c.Offset/FileTransferChunkSize, c.Offset%FileTransferChunkSize)
	body := fmt.Sprintf("%s && p=%s && [ -f \"$p\" ] && [ ! -L \"$p\" ] && n=$(wc -c < \"$p\") && [ \"$n\" -ge '%d' ] && h=$(%s < \"$p\" | sha256sum) && { set -- $h; [ \"$1\" = '%s' ]; } && { if [ \"$n\" -gt '%d' ]; then [ ! -L \"$d/recover\" ] && %s < \"$p\" > \"$d/recover\" && h=$(sha256sum < \"$d/recover\") && { set -- $h; [ \"$1\" = '%s' ]; } && mv \"$d/recover\" \"$p\"; else :; fi; }", checkpointRemoteGuard(c), quoted, c.Offset, prefix, c.PrefixSHA256, c.Offset, prefix, c.PrefixSHA256)
	return p.checkpointCommand(ctx, body)
}

func (p *fileProtocol) streamCheckpoint(ctx context.Context, dir string, c *TransferCheckpoint, progress FileTransferProgress) error {
	local := filepath.Join(dir, "payload")
	if c.Direction == "send" {
		n, h, err := hashTransferFile(ctx, local, -1)
		if err != nil {
			return err
		}
		if n != c.Size || h != c.SHA256 {
			return errors.New("upload snapshot changed; resume refused")
		}
		if err := p.validateCheckpointPrefix(ctx, *c); err != nil {
			return fmt.Errorf("remote confirmed prefix changed or unavailable: %w", err)
		}
	} else {
		if c.Kind == RemotePathDirectory {
			if err := p.checkpointCommand(ctx, checkpointRemoteGuard(*c)); err != nil {
				return err
			}
		}
		n, h, err := p.checkpointMetadata(ctx, checkpointRemotePayload(*c))
		if err != nil {
			return err
		}
		if n != c.Size || h != c.SHA256 {
			return errors.New("download source or snapshot changed; resume refused")
		}
	}
	_, prefix, err := hashTransferFile(ctx, local, c.Offset)
	if err != nil {
		return err
	}
	if prefix != c.PrefixSHA256 {
		return errors.New("local confirmed prefix changed; resume refused")
	}
	flags := os.O_RDONLY
	if c.Direction == "receive" {
		flags = os.O_RDWR
	}
	f, err := os.OpenFile(local, flags, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.CopyN(&transferContextWriter{ctx: ctx, writer: h}, f, c.Offset); err != nil {
		return err
	}
	if c.Direction == "receive" {
		if err := f.Truncate(c.Offset); err != nil {
			return err
		}
	}
	if progress != nil {
		if err := progress(c.Offset, c.Size); err != nil {
			return err
		}
	}
	quoted, _ := quoteRemotePath(checkpointRemotePayload(*c))
	buf := make([]byte, FileTransferChunkSize)
	for c.Offset < c.Size {
		if err := fileTransferCancellationCheckpoint(ctx, p.terminal, nil); err != nil {
			return err
		}
		chunk := buf[:min(int64(len(buf)), c.Size-c.Offset)]
		if c.Direction == "send" {
			if _, err := io.ReadFull(f, chunk); err != nil {
				return err
			}
			if err := p.sendBlock(ctx, quoted, chunk); err != nil {
				return err
			}
		} else {
			if err := p.receiveBlock(ctx, quoted, c.Offset/FileTransferChunkSize, chunk); err != nil {
				return err
			}
			if _, err := f.Write(chunk); err != nil {
				return err
			}
			if err := f.Sync(); err != nil {
				return err
			}
		}
		_, _ = h.Write(chunk)
		c.Offset += int64(len(chunk))
		c.PrefixSHA256 = hex.EncodeToString(h.Sum(nil))
		// Save before notifying observers: UI progress never exceeds the durable
		// checkpoint, even if the callback requests cancellation immediately.
		if err := saveTransferCheckpoint(dir, *c); err != nil {
			return err
		}
		if progress != nil {
			if err := progress(c.Offset, c.Size); err != nil {
				return err
			}
		}
	}
	if c.PrefixSHA256 != c.SHA256 {
		return ErrFileTransferChecksumMismatch
	}
	n, digest, err := p.checkpointMetadata(ctx, checkpointRemotePayload(*c))
	if err != nil {
		return err
	}
	if n != c.Size || digest != c.SHA256 {
		return ErrFileTransferChecksumMismatch
	}
	return fileTransferCancellationCheckpoint(ctx, p.terminal, nil)
}
