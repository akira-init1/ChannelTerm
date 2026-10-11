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
// Future adapters can provide the same two callbacks without terminal I/O.
type sshInteraction struct {
	input       *os.File
	output      io.Writer
	destination string
}

// configureSSHAuthentication shares path resolution and interactive callbacks
// between terminal and dedicated file connections. It never opens a connection.
func configureSSHAuthentication(config *sshtransport.Config, input io.Reader, output io.Writer) error {
	if config.PrivateKeyPath == "" || config.KnownHostsPath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("resolve SSH configuration directory: %w", err)
		}
		if config.PrivateKeyPath == "" {
			for _, name := range []string{"id_ed25519", "id_rsa"} {
				candidate := filepath.Join(home, ".ssh", name)
				if _, err := os.Stat(candidate); !errors.Is(err, os.ErrNotExist) {
					config.PrivateKeyPath = candidate
					break
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
		config.ConfirmHostKey = interaction.confirmHostKey
	} else if config.PrivateKeyPath == "" {
		return errors.New("no default SSH private key found; password login requires a terminal on stdin, or specify --identity PATH")
	}
	return nil
}

func (p sshInteraction) password(ctx context.Context) ([]byte, error) {
	return terminalinput.ReadLine(ctx, p.input, p.output, fmt.Sprintf("%s's password: ", p.destination), true)
}

func (p sshInteraction) confirmHostKey(ctx context.Context, key sshtransport.HostKey) (bool, error) {
	prompt := fmt.Sprintf("The authenticity of host %q cannot be established.\r\n%s key fingerprint is %s.\r\nVerify this fingerprint with the server administrator.\r\nTrust and save this host key (yes/no)? ", key.Address, key.Algorithm, key.Fingerprint)
	answer, err := terminalinput.ReadLine(ctx, p.input, p.output, prompt, false)
	return strings.TrimSpace(string(answer)) == "yes", err
}
