package command

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/akira-init1/ChannelTerm/internal/cli/terminalinput"
	sshtransport "github.com/akira-init1/ChannelTerm/internal/transport/ssh"
)

// sshInteraction keeps prompts in the CLI and SSH backend types in Transport.
// Future adapters can provide the same two callbacks without terminal I/O.
type sshInteraction struct {
	input       *os.File
	output      io.Writer
	destination string
}

func (p sshInteraction) password(ctx context.Context) ([]byte, error) {
	return terminalinput.ReadLine(ctx, p.input, p.output, fmt.Sprintf("%s's password: ", p.destination), true)
}

func (p sshInteraction) confirmHostKey(ctx context.Context, key sshtransport.HostKey) (bool, error) {
	prompt := fmt.Sprintf("The authenticity of host %q cannot be established.\r\n%s key fingerprint is %s.\r\nVerify this fingerprint with the server administrator.\r\nTrust and save this host key (yes/no)? ", key.Address, key.Algorithm, key.Fingerprint)
	answer, err := terminalinput.ReadLine(ctx, p.input, p.output, prompt, false)
	return strings.TrimSpace(string(answer)) == "yes", err
}
