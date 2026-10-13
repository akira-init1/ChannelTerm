package ssh

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gofrs/flock"
	gossh "golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// HostKey is the non-secret identity displayed for an unknown SSH server.
// Address uses OpenSSH's normalized host/port form; Fingerprint uses SHA256.
// Adapters approve this identity, never supply replacement key material.
type HostKey struct {
	Address     string
	Algorithm   string
	Fingerprint string
}

func unknownHostKey(err error) bool {
	var keyError *knownhosts.KeyError
	return errors.As(err, &keyError) && len(keyError.Want) == 0
}

// preferredHostKeyAlgorithms asks the existing trust snapshot for matching
// records. Its KeyError retains hostname, port, wildcard and hash matching;
// the empty probe cannot equal a parsed key and never verifies a signature.
// These are negotiation hints only: checkHostKey still validates the real key.
func preferredHostKeyAlgorithms(verify gossh.HostKeyCallback, host string, remote net.Addr) []string {
	var mismatch *knownhosts.KeyError
	if !errors.As(verify(host, remote, hostKeyProbe{}), &mismatch) || len(mismatch.Want) == 0 {
		return nil // Unknown hosts retain the backend's default negotiation.
	}
	trusted := make(map[string]bool)
	for _, known := range mismatch.Want {
		trusted[known.Key.Type()] = true
	}
	// Keep certificates ahead of raw keys, as in backend defaults. A matching
	// @cert-authority record identifies the CA's key type, not the certified
	// host's key type, so it cannot narrow certificate algorithms safely.
	algorithms := append(gossh.SupportedAlgorithms().HostKeys, gossh.InsecureAlgorithms().HostKeys...)
	var certificates, preferred, remaining []string
	for _, algorithm := range algorithms {
		keyType := algorithm
		if algorithm == gossh.KeyAlgoRSASHA256 || algorithm == gossh.KeyAlgoRSASHA512 {
			keyType = gossh.KeyAlgoRSA
		}
		switch {
		case strings.HasSuffix(algorithm, "-cert-v01@openssh.com"):
			certificates = append(certificates, algorithm)
		case trusted[keyType]:
			preferred = append(preferred, algorithm)
		default:
			remaining = append(remaining, algorithm)
		}
	}
	// Retain other algorithms so an unavailable trusted key is still diagnosed
	// by the same strict verifier as a mismatch, without a replacement prompt.
	return append(append(certificates, preferred...), remaining...)
}

// hostKeyProbe requests the callback's matching records without supplying a
// real key. It is used only for lookup, never as a negotiated server identity.
type hostKeyProbe struct{}

// Type returns no real SSH algorithm for the lookup-only probe.
func (hostKeyProbe) Type() string { return "" }

// Marshal cannot match any nonempty, parsed known_hosts key encoding.
func (hostKeyProbe) Marshal() []byte { return nil }

// Verify rejects every signature; the lookup path must never authenticate.
func (hostKeyProbe) Verify([]byte, *gossh.Signature) error {
	return errors.New("host-key lookup probe cannot verify signatures")
}

func (t *Transport) checkHostKey(ctx context.Context, budget *setupBudget, host string, remote net.Addr, key gossh.PublicKey) error {
	err := t.verifyHostKey(host, remote, key)
	if err == nil || !unknownHostKey(err) || t.config.ConfirmHostKey == nil {
		return err
	}
	budget.pause()
	if ctx.Err() != nil {
		return context.Cause(ctx)
	}
	approved, promptErr := t.config.ConfirmHostKey(ctx, HostKey{
		Address: knownhosts.Normalize(host), Algorithm: key.Type(), Fingerprint: gossh.FingerprintSHA256(key),
	})
	budget.resume()
	if ctx.Err() != nil {
		return context.Cause(ctx)
	}
	if promptErr != nil {
		return fmt.Errorf("confirm SSH host key: %w", promptErr)
	}
	if !approved {
		return fmt.Errorf("SSH host key not trusted: %w", err)
	}
	return saveHostKey(ctx, t.config.KnownHostsPath, host, remote, key)
}

// saveHostKey rechecks under an interprocess lock after user interaction.
// Another ChannelTerm process may have trusted a different key while the prompt
// was open. Never overwrite such a record or append a duplicate. Existing file
// contents (including comments and hashed records) are preserved byte for byte.
func saveHostKey(ctx context.Context, path, host string, remote net.Addr, key gossh.PublicKey) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("create SSH trust directory: %w", err)
	}
	lock := flock.New(path + ".lock")
	locked, err := lock.TryLockContext(ctx, 25*time.Millisecond)
	if err != nil {
		return fmt.Errorf("lock SSH known_hosts: %w", err)
	}
	if !locked {
		return errors.New("SSH known_hosts lock unavailable")
	}
	defer lock.Close()
	verify, err := knownhosts.New(path)
	if err == nil {
		err = verify(host, remote, key)
		if err == nil {
			return nil
		}
		if !unknownHostKey(err) {
			return fmt.Errorf("SSH known_hosts changed during confirmation: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("reload SSH known_hosts: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0600)
	if err != nil {
		return fmt.Errorf("save SSH known_hosts: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	line := knownhosts.Line([]string{host}, key) + "\n"
	if info.Size() > 0 {
		var last [1]byte
		if _, err := file.ReadAt(last[:], info.Size()-1); err != nil {
			return err
		}
		if last[0] != '\n' {
			line = "\n" + line
		}
	}
	if n, err := io.WriteString(file, line); err != nil {
		return fmt.Errorf("save SSH known_hosts: %w", err)
	} else if n != len(line) {
		return io.ErrShortWrite
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync SSH known_hosts: %w", err)
	}
	return nil
}
