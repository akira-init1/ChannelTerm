package command

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/akira-init1/ChannelTerm/internal/cli/terminalinput"
	sshtransport "github.com/akira-init1/ChannelTerm/internal/transport/ssh"
	"golang.org/x/term"
)

// sshInteraction keeps prompts in the CLI and SSH backend types in Transport.
// Future adapters can provide the same callbacks without terminal I/O.
type sshInteraction struct {
	input       *os.File
	output      io.Writer
	destination string
}

// configureSSHAuthentication shares path resolution and interactive callbacks
// between terminal and dedicated file connections. It never opens a connection.
func configureSSHAuthentication(config *sshtransport.Config, input io.Reader, output io.Writer) error {
	if config.PrivateKeyPath == "" && len(config.PrivateKeyPaths) == 0 || config.KnownHostsPath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("resolve SSH configuration directory: %w", err)
		}
		if config.PrivateKeyPath == "" && len(config.PrivateKeyPaths) == 0 {
			for _, name := range []string{"id_ed25519", "id_rsa"} {
				candidate := filepath.Join(home, ".ssh", name)
				if _, err := os.Stat(candidate); !errors.Is(err, os.ErrNotExist) {
					if config.PrivateKeyPath == "" {
						config.PrivateKeyPath = candidate
					} else {
						config.PrivateKeyPaths = append(config.PrivateKeyPaths, candidate)
					}
				}
			}
		}
		if config.KnownHostsPath == "" {
			config.KnownHostsPath = filepath.Join(home, ".ssh", "known_hosts")
		}
	}
	if file, ok := input.(*os.File); ok && term.IsTerminal(int(file.Fd())) {
		interaction := sshInteraction{input: file, output: output, destination: config.User + "@" + config.Host}
		config.Password = interaction.password
		config.KeyPassphrase = interaction.keyPassphrase
		config.ConfirmHostKey = interaction.confirmHostKey
	} else if config.PrivateKeyPath == "" && len(config.PrivateKeyPaths) == 0 && config.Password == nil {
		return errors.New("no default SSH private key found; password login requires a terminal on stdin, or specify --identity PATH")
	}
	return nil
}

// identityFlags preserves explicit CLI order across --identity and -i. An
// explicit list replaces default discovery; it never changes password fallback.
type identityFlags []string

// String supplies flag package presentation without reading key contents.
func (f *identityFlags) String() string { return strings.Join(*f, ", ") }

// Set appends one explicitly selected identity in command-line order.
func (f *identityFlags) Set(value string) error {
	if value == "" {
		return errors.New("SSH identity path must not be empty")
	}
	*f = append(*f, value)
	return nil
}
func (f identityFlags) configure(config *sshtransport.Config) {
	if len(f) > 0 {
		config.PrivateKeyPath = f[0]
		config.PrivateKeyPaths = append([]string(nil), f[1:]...)
	}
}

func (p sshInteraction) keyPassphrase(ctx context.Context, path string) ([]byte, error) {
	return terminalinput.ReadLine(ctx, p.input, p.output, fmt.Sprintf("Passphrase for SSH identity %q: ", path), true)
}

func (p sshInteraction) password(ctx context.Context) ([]byte, error) {
	return terminalinput.ReadLine(ctx, p.input, p.output, fmt.Sprintf("%s's password: ", p.destination), true)
}

func (p sshInteraction) confirmHostKey(ctx context.Context, key sshtransport.HostKey) (bool, error) {
	prompt := fmt.Sprintf("The authenticity of host %q cannot be established.\r\n%s key fingerprint is %s.\r\nVerify this fingerprint with the server administrator.\r\nTrust and save this host key (yes/no)? ", key.Address, key.Algorithm, key.Fingerprint)
	answer, err := terminalinput.ReadLine(ctx, p.input, p.output, prompt, false)
	return strings.TrimSpace(string(answer)) == "yes", err
}
