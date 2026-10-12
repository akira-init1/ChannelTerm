package ssh

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"os"

	gossh "golang.org/x/crypto/ssh"
)

// identity snapshots either a parsed signer or encrypted bytes. Unlocking is
// deferred until host verification and never mutates the shared Transport.
type identity struct {
	path      string
	signer    gossh.Signer
	encrypted []byte
	public    gossh.PublicKey
}

func loadIdentities(config Config) ([]identity, error) {
	paths := append([]string(nil), config.PrivateKeyPaths...)
	if config.PrivateKeyPath != "" {
		paths = append([]string{config.PrivateKeyPath}, paths...)
	}
	seen := make(map[string]bool)
	var identities []identity
	for _, path := range paths {
		if seen[path] {
			continue
		}
		seen[path] = true
		key, err := os.ReadFile(path)
		if err != nil {
			return nil, stageError("identity", fmt.Errorf("read SSH identity %q: %w", path, err))
		}
		signer, err := gossh.ParsePrivateKey(key)
		var missing *gossh.PassphraseMissingError
		if errors.As(err, &missing) {
			identities = append(identities, identity{path: path, encrypted: key, public: missing.PublicKey})
			continue
		}
		clear(key)
		if err != nil {
			return nil, stageError("identity", fmt.Errorf("parse SSH identity %q: %w", path, err))
		}
		identities = append(identities, identity{path: path, signer: signer})
	}
	return identities, nil
}

func (t *Transport) connectionSigners(ctx context.Context, budget *setupBudget) ([]gossh.Signer, error) {
	var signers []gossh.Signer
	for _, key := range t.identities {
		if key.signer != nil {
			signers = append(signers, key.signer)
			continue
		}
		unlock := func() (gossh.Signer, error) {
			if t.config.KeyPassphrase == nil {
				return nil, stageError("identity", fmt.Errorf("encrypted SSH identity %q requires a passphrase provider or interactive terminal", key.path))
			}
			budget.pause()
			defer budget.resume()
			for range 3 {
				if err := ctx.Err(); err != nil {
					return nil, context.Cause(ctx)
				}
				passphrase, err := t.config.KeyPassphrase(ctx, key.path)
				if ctx.Err() != nil {
					err = context.Cause(ctx)
				}
				if err != nil {
					clear(passphrase)
					return nil, stageError("authentication", err)
				}
				signer, err := gossh.ParsePrivateKeyWithPassphrase(key.encrypted, passphrase)
				clear(passphrase)
				if err == nil {
					return signer, nil
				}
				if !errors.Is(err, x509.IncorrectPasswordError) {
					return nil, stageError("identity", fmt.Errorf("decrypt SSH identity %q: %w", key.path, err))
				}
			}
			return nil, stageError("authentication", fmt.Errorf("incorrect passphrase for SSH identity %q after 3 attempts", key.path))
		}
		if key.public != nil {
			// OpenSSH encrypted files expose the public key. Only ask for the
			// passphrase if the server accepts this key, avoiding unused prompts.
			signers = append(signers, &lockedSigner{public: key.public, unlock: unlock})
		} else {
			// Legacy encrypted PEM lacks a public key until decryption.
			signer, err := unlock()
			if err != nil {
				return nil, err
			}
			signers = append(signers, signer)
		}
	}
	return signers, nil
}

// lockedSigner is connection-local and used serially by SSH authentication.
// AlgorithmSigner preserves RSA SHA-2 negotiation rather than forcing ssh-rsa.
type lockedSigner struct {
	public gossh.PublicKey
	unlock func() (gossh.Signer, error)
	signer gossh.Signer
}

// PublicKey exposes only the unencrypted public portion for server probing.
func (s *lockedSigner) PublicKey() gossh.PublicKey { return s.public }

// Sign unlocks a server-accepted identity using its default signature algorithm.
func (s *lockedSigner) Sign(random io.Reader, data []byte) (*gossh.Signature, error) {
	return s.SignWithAlgorithm(random, data, "")
}

// SignWithAlgorithm retains the algorithm selected by SSH negotiation.
func (s *lockedSigner) SignWithAlgorithm(random io.Reader, data []byte, algorithm string) (*gossh.Signature, error) {
	if s.signer == nil {
		signer, err := s.unlock()
		if err != nil {
			return nil, err
		}
		s.signer = signer
	}
	if signer, ok := s.signer.(gossh.AlgorithmSigner); ok {
		return signer.SignWithAlgorithm(random, data, algorithm)
	}
	if algorithm != "" && algorithm != s.public.Type() {
		return nil, errors.New("unsupported SSH identity signature algorithm")
	}
	return s.signer.Sign(random, data)
}
