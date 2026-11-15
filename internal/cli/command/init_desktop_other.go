//go:build !windows

package command

import "os/exec"

// hideDesktopHelper needs no console suppression on non-Windows subprocesses.
func hideDesktopHelper(_ *exec.Cmd) {}
