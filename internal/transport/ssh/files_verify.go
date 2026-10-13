package ssh

import (
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// RequireSHA256 checks the fixed remote hashing utility before an SCP transfer
// can modify its destination. It requires a POSIX shell with sha256sum in PATH.
func (f *Files) RequireSHA256() error {
	_, err := f.runFileCheck("command -v sha256sum >/dev/null")
	if err != nil {
		return fmt.Errorf("SCP SHA-256 verification requires remote sha256sum: %w", err)
	}
	return nil
}

// SCPUploadDestination resolves SCP's existing-directory rule without cleaning
// literal remote path components. Concurrent remote namespace changes are not
// locked; the later digest must still match the actual file at this path.
func (f *Files) SCPUploadDestination(destination, name string) (string, error) {
	if !ValidFileName(name) {
		return "", errors.New("invalid SCP source basename")
	}
	quoted, err := scpPath(destination)
	if err != nil {
		return "", err
	}
	output, err := f.runFileCheck("if test -d " + quoted + "; then printf d; else printf f; fi")
	if err != nil {
		return "", err
	}
	switch output {
	case "d":
		return strings.TrimRight(destination, "/") + "/" + name, nil
	case "f":
		return destination, nil
	default:
		return "", errors.New("invalid SCP destination check response")
	}
}

// SHA256 hashes a remote file through a fixed, quoted command on the dedicated
// SCP connection. It never uses a terminal Session or requests a PTY. The
// connection's operation context interrupts command setup, output, and exit.
func (f *Files) SHA256(remote string) (string, error) {
	quoted, err := scpPath(remote)
	if err != nil {
		return "", err
	}
	// Reading stdin avoids implementation-specific filename escaping in output.
	output, err := f.runFileCheck("sha256sum < " + quoted)
	if err != nil {
		return "", fmt.Errorf("remote SHA-256 for %q: %w", remote, err)
	}
	fields := strings.Fields(output)
	if len(fields) != 2 || fields[1] != "-" || len(fields[0]) != 64 {
		return "", errors.New("invalid remote SHA-256 response")
	}
	if _, err := hex.DecodeString(fields[0]); err != nil {
		return "", errors.New("invalid remote SHA-256 digest")
	}
	return strings.ToLower(fields[0]), nil
}

// runFileCheck is private so callers can only invoke the fixed file operations
// above. Output is bounded while being fully drained to avoid SSH backpressure.
func (f *Files) runFileCheck(command string) (output string, err error) {
	defer func() {
		if f.ctx.Err() != nil {
			err = errors.Join(f.ctx.Err(), err)
		}
	}()
	if f.sftp != nil {
		return "", errors.New("remote file commands require an SCP file connection")
	}
	if err := f.ctx.Err(); err != nil {
		return "", err
	}
	session, err := f.client.NewSession()
	if err != nil {
		return "", err
	}
	defer session.Close()
	stdout := &fileCheckOutput{}
	stderr := &scpDiagnostic{}
	session.Stdout, session.Stderr = stdout, stderr
	if err := session.Run(command); err != nil {
		return "", fmt.Errorf("remote file check: %w (stderr %q)", err, stderr.data)
	}
	if stdout.overflow {
		return "", errors.New("remote file check response exceeds 4096 bytes")
	}
	return string(stdout.data), nil
}

type fileCheckOutput struct {
	scpDiagnostic
	overflow bool
}

// Write detects oversized stdout without retaining it or blocking the peer.
func (w *fileCheckOutput) Write(data []byte) (int, error) {
	if len(data) > 4096-len(w.data) {
		w.overflow = true
	}
	return w.scpDiagnostic.Write(data)
}
