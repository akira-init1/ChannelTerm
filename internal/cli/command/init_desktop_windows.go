package command

import (
	"os/exec"
	"syscall"
)

// hideDesktopHelper keeps fixed client capability probes out of the user's desktop.
func hideDesktopHelper(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
}
