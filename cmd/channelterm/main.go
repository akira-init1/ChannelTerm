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
	interrupts := make(chan os.Signal, 1)
	signal.Notify(interrupts, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(interrupts)
	if err := command.RunWithInterrupts(context.Background(), os.Args[1:], os.Stdin, os.Stdout, os.Stderr, interrupts); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
