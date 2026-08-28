//go:build windows

package command

import (
	"os"

	"golang.org/x/sys/windows"
)

const enableVirtualTerminalProcessing = 0x0004

// enableANSIOutput enables Windows virtual-terminal processing when the output
// handle is a console. A non-console handle leaves auto highlighting disabled.
func enableANSIOutput(output *os.File) bool {
	handle := windows.Handle(output.Fd())
	var mode uint32
	if err := windows.GetConsoleMode(handle, &mode); err != nil {
		return false
	}
	if mode&enableVirtualTerminalProcessing != 0 {
		return true
	}
	return windows.SetConsoleMode(handle, mode|enableVirtualTerminalProcessing) == nil
}
