package highlight

import (
	"io"
	"testing"
)

func TestInteractivePromptRejectsOrdinaryText(t *testing.T) {
	for _, input := range []string{"$variable", "#heading", ">comparison", "log: > "} {
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
