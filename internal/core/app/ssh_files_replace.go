package app

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	sshtransport "github.com/akira-init1/ChannelTerm/internal/transport/ssh"
)

func sshTemporaryName() (string, error) {
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", err
	}
	return fmt.Sprintf(".channelterm-%x.part", nonce), nil
}

// replaceSFTPUpload keeps the old destination until close, requested verification,
// and POSIX rename succeed. It never deletes the target as a fallback. Cleanup uses the same
// connection; after a disconnect the error names the possibly retained file.
func replaceSFTPUpload(ctx context.Context, client *sshtransport.Files, source io.Reader, target, name string, size int64, r SSHFilesRequest, result *SSHFilesResult) (err error) {
	temporary, err := sshTemporaryName()
	if err != nil {
		return err
	}
	// Preserve the literal parent, including symlink/.. components.
	temporary = target[:strings.LastIndex(target, "/")+1] + temporary
	sink, err := client.Create(temporary, false)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			if cleanupErr := client.Remove(temporary, false); cleanupErr != nil {
				err = errors.Join(err, fmt.Errorf("temporary remote file may remain at %q: %w", temporary, cleanupErr))
			}
		}
	}()
	n, digest, copyErr := copySSHFileChecked(ctx, sink, source, name, size, r)
	result.Bytes += n
	if err := errors.Join(copyErr, sink.Close()); err != nil {
		return err
	}
	if err := verifySSHCopy(ctx, r, name, size, digest, func() (io.ReadCloser, error) { return client.Open(temporary) }); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := client.Rename(temporary, target, true); err != nil {
		return err
	}
	committed = true
	completeSSHFile(r, result)
	return nil
}

// replaceSFTPDownload stages inside the same os.Root and destination directory.
// Sync, Close, and verification failures leave the old file intact. Rename semantics follow
// the local OS/filesystem; no delete-first fallback or tree rollback is used.
func replaceSFTPDownload(ctx context.Context, root *os.Root, source io.Reader, target, name string, size int64, r SSHFilesRequest, result *SSHFilesResult) (err error) {
	temporary, err := sshTemporaryName()
	if err != nil {
		return err
	}
	temporary = filepath.Join(filepath.Dir(target), temporary)
	file, err := root.OpenFile(temporary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			if cleanupErr := root.Remove(temporary); cleanupErr != nil {
				err = errors.Join(err, fmt.Errorf("remove temporary local file %q: %w", temporary, cleanupErr))
			}
		}
	}()
	n, digest, copyErr := copySSHFileChecked(ctx, file, source, name, size, r)
	result.Bytes += n
	if copyErr == nil {
		copyErr = file.Sync()
	}
	if err := errors.Join(copyErr, file.Close()); err != nil {
		return err
	}
	if err := verifySSHCopy(ctx, r, name, size, digest, func() (io.ReadCloser, error) { return root.Open(temporary) }); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := root.Rename(temporary, target); err != nil {
		return err
	}
	committed = true
	completeSSHFile(r, result)
	return nil
}
