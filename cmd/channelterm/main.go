// Command channelterm starts the ChannelTerm CLI adapter.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/akira-init1/ChannelTerm/internal/cli/command"
)

// main assembles process-level cancellation and standard streams before
// delegating all command behavior to the CLI adapter.
func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := command.Run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
