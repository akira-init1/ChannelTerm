package highlight

import (
	"io"
	"testing"
)

func TestInteractivePromptRejectsOrdinaryText(t *testing.T) {
	for _, input := range []string{"fs0: device", "log: fs0:\\> ", "grub rescue failed", "log: grub rescue> ", "barebox booting", "log: barebox@board:/ ", "$variable", "#heading", ">comparison", "log: > ", "log: BBG U-Boot > ", "BBG U-Boot version "} {
		if HasInteractivePromptPrefix([]byte(input)) {
			t.Errorf("recognized ordinary output %q", input)
		}
	}
}

func TestInteractivePromptPropagatesWriteFailure(t *testing.T) {
	for _, input := range []string{"# ", "$ echo ERROR\r\n", "> "} {
		if _, err := New(bootloaderErrorWriter{}).Write([]byte(input)); err != io.ErrClosedPipe {
			t.Errorf("Write(%q) = %v, want %v", input, err, io.ErrClosedPipe)
		}
	}
}
