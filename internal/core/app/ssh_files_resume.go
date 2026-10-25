package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"

	sshtransport "github.com/akira-init1/ChannelTerm/internal/transport/ssh"
)

// copyResumedSSHFile compares the whole destination prefix before any writes.
// Reading leaves both handles at the verified offset. Only newly written bytes
// count toward the operation result; progress includes the confirmed prefix.
// The digest covers the complete source, including the prefix, for final readback.
func copyResumedSSHFile(ctx context.Context, dst io.ReadWriter, src io.Reader, name string, size, offset int64, r SSHFilesRequest) (int64, string, error) {
	if offset < 0 || offset > size {
		return 0, "", errors.New("resume destination is larger than the source or has an invalid size")
	}
	if err := startSSHVerification(r, name); err != nil {
		return 0, "", err
	}
	digest := sha256.New()
	source := io.TeeReader(src, digest)
	a, b := make([]byte, 32*1024), make([]byte, 32*1024)
	for remaining := offset; remaining > 0; {
		if err := ctx.Err(); err != nil {
			return 0, "", err
		}
		n := min(int64(len(a)), remaining)
		if _, err := io.ReadFull(source, a[:n]); err != nil {
			return 0, "", fmt.Errorf("read resume source prefix: %w", err)
		}
		if _, err := io.ReadFull(dst, b[:n]); err != nil {
			return 0, "", fmt.Errorf("read resume destination prefix: %w", err)
		}
		if !bytes.Equal(a[:n], b[:n]) {
			return 0, "", errors.New("resume prefix mismatch; destination was not modified")
		}
		remaining -= n
	}
	progress := r.Progress
	r.Progress = func(name string, n, _ int64) error {
		if progress != nil {
			return progress(name, offset+n, size)
		}
		return nil
	}
	n, err := copySSHFile(ctx, dst, source, name, size-offset, r)
	return n, fmt.Sprintf("%x", digest.Sum(nil)), err
}

func resumeSFTPUpload(ctx context.Context, client *sshtransport.Files, source io.Reader, target, name string, size int64, existing bool, r SSHFilesRequest, result *SSHFilesResult) error {
	sink, offset, err := client.OpenResume(target, existing)
	if err != nil {
		return err
	}
	n, digest, copyErr := copyResumedSSHFile(ctx, sink, source, name, size, offset, r)
	result.Bytes += n
	if err := errors.Join(copyErr, sink.Close()); err != nil {
		return err
	}
	if err := verifySSHCopy(ctx, r, name, size, digest, func() (io.ReadCloser, error) { return client.Open(target) }); err != nil {
		return err
	}
	completeSSHFile(r, result)
	return nil
}

func resumeSFTPDownload(ctx context.Context, root *os.Root, source io.Reader, local, name string, size int64, existing bool, r SSHFilesRequest, result *SSHFilesResult) error {
	flags := os.O_RDWR | os.O_CREATE | os.O_EXCL
	if existing {
		flags = os.O_RDWR
	}
	sink, err := root.OpenFile(local, flags, 0600)
	if err != nil {
		return err
	}
	info, err := sink.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return errors.Join(errors.New("resume destination must be a regular file"), err, sink.Close())
	}
	n, digest, copyErr := copyResumedSSHFile(ctx, sink, source, name, size, info.Size(), r)
	result.Bytes += n
	if copyErr == nil {
		copyErr = sink.Sync()
	}
	if err := errors.Join(copyErr, sink.Close()); err != nil {
		return err
	}
	if err := verifySSHCopy(ctx, r, name, size, digest, func() (io.ReadCloser, error) { return root.Open(local) }); err != nil {
		return err
	}
	completeSSHFile(r, result)
	return nil
}
