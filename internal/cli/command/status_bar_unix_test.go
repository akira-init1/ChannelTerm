//go:build !windows

package command

import (
	"os"
	"testing"
)

func statusConsoleFiles(_ *testing.T) (*os.File, *os.File) { return os.Stdin, os.Stdout }
