package app

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
)

// copySSHFileChecked hashes exactly the source bytes consumed by the copy.
// Destination verification reopens the closed file, not the outgoing stream.
func copySSHFileChecked(ctx context.Context, dst io.Writer, src io.Reader, name string, size int64, r SSHFilesRequest) (int64, string, error) {
	if r.Verify != "sha256" {
		n, err := copySSHFile(ctx, dst, src, name, size, r)
		return n, "", err
	}
	digest := sha256.New()
	n, err := copySSHFile(ctx, dst, io.TeeReader(src, digest), name, size, r)
	return n, fmt.Sprintf("%x", digest.Sum(nil)), err
}

// hashSSHFile bounds reads by the expected size and observes cancellation during
// local reads too. The SSH connection context interrupts blocked remote reads.
func hashSSHFile(ctx context.Context, size int64, open func() (io.ReadCloser, error)) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	file, err := open()
	if err != nil {
		return "", err
	}
	digest := sha256.New()
	_, readErr := copySSHFile(ctx, digest, file, "", size, SSHFilesRequest{})
	if err := errors.Join(readErr, file.Close()); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", digest.Sum(nil)), nil
}

func startSSHVerification(r SSHFilesRequest, name string) error {
	if r.Verification != nil {
		return r.Verification(SSHFileVerification{Name: name})
	}
	return nil
}

func finishSSHVerification(r SSHFilesRequest, name, source, destination string) error {
	if source != destination {
		return fmt.Errorf("SHA-256 mismatch for %q: source=%s destination=%s", name, source, destination)
	}
	if r.Verification != nil {
		return r.Verification(SSHFileVerification{Name: name, SourceSHA256: source, DestinationSHA256: destination})
	}
	return nil
}

func verifySSHCopy(ctx context.Context, r SSHFilesRequest, name string, size int64, source string, open func() (io.ReadCloser, error)) error {
	if r.Verify != "sha256" {
		return nil
	}
	if err := startSSHVerification(r, name); err != nil {
		return err
	}
	destination, err := hashSSHFile(ctx, size, open)
	if err != nil {
		return fmt.Errorf("verify SHA-256 for %q: %w", name, err)
	}
	return finishSSHVerification(r, name, source, destination)
}

func completeSSHFile(r SSHFilesRequest, result *SSHFilesResult) {
	result.Files++
	if r.Verify == "sha256" {
		result.VerifiedFiles++
	}
}
