package command

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/cli/highlight"
	serialtransport "github.com/akira-init1/ChannelTerm/internal/core/transport/serial"
)

func TestPresentationFlushUnknownPromptKeepsEchoStreaming(t *testing.T) {
	for _, colors := range []bool{false, true} {
		for _, timestamps := range []bool{false, true} {
			t.Run(fmt.Sprintf("colors=%t/timestamps=%t", colors, timestamps), func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				var output lockedBuffer
				var mu sync.Mutex
				var highlighter *highlight.Renderer
				if colors {
					highlighter = highlight.New(&output)
				}
				renderer := newPromptTimestampRenderer(terminalOutputWriter(&output, highlighter), terminalOutputFlusher(highlighter), fixedPromptTimestampClock)
				if timestamps {
					if _, err := renderer.Toggle(); err != nil {
						t.Fatal(err)
					}
				}
				if err := renderer.Write([]byte("Choose action? ")); err != nil {
					t.Fatal(err)
				}
				stop := startPresentationFlush(ctx, &mu, renderer, highlighter, cancel)
				defer stop()
				waitSSHOutput(t, &output, "Choose action? ")
				// Stop the timer: every following key must still be forwarded without
				// a periodic flush, a newline, or a local echo implementation.
				if err := stop(); err != nil {
					t.Fatal(err)
				}
				want := "Choose action? "
				for _, echo := range []string{"e", "cho ERROR", "\b \b", "\r", "\n"} {
					if err := renderer.Write([]byte(echo)); err != nil {
						t.Fatal(err)
					}
					want += echo
					if output.String() != want {
						t.Fatalf("got %q want %q", output.String(), want)
					}
				}
				if err := renderer.Write([]byte("ERROR\n")); err != nil {
					t.Fatal(err)
				}
				if !strings.HasSuffix(output.String(), "ERROR\n") && !colors {
					t.Fatal("lost next line")
				}
				if colors && !strings.Contains(output.String()[len(want):], "\x1b[") {
					t.Fatal("highlighting did not recover")
				}
			})
		}
	}
}

func TestPresentationFlushPreservesSplitRemoteBytes(t *testing.T) {
	for _, input := range []string{"\x1b[32mcustom ? \x1b[0m", "\x1b]0;title\a请输入: ", "请选择: ", "\x1b[?1049hmenu\x1b[?1049l"} {
		var output bytes.Buffer
		highlighter := highlight.New(&output)
		renderer := newPromptTimestampRenderer(terminalOutputWriter(&output, highlighter), terminalOutputFlusher(highlighter), fixedPromptTimestampClock)
		if _, err := renderer.Toggle(); err != nil {
			t.Fatal(err)
		}
		for _, value := range []byte(input) {
			if err := renderer.Write([]byte{value}); err != nil {
				t.Fatal(err)
			}
			if err := renderer.releasePending(); err != nil {
				t.Fatal(err)
			}
			if err := highlighter.ReleasePending(); err != nil {
				t.Fatal(err)
			}
		}
		if output.String() != input {
			t.Fatalf("remote bytes changed: got %q want %q", output.String(), input)
		}
	}
}

type presentationFailWriter struct{ calls int }

func (w *presentationFailWriter) Write([]byte) (int, error) {
	w.calls++
	return 0, io.ErrClosedPipe
}

func TestPresentationFlushFailureCancelsAndJoins(t *testing.T) {
	for _, colors := range []bool{false, true} {
		t.Run(fmt.Sprintf("colors=%t", colors), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			output := &presentationFailWriter{}
			var mu sync.Mutex
			var highlighter *highlight.Renderer
			if colors {
				highlighter = highlight.New(output)
			}
			renderer := newPromptTimestampRenderer(terminalOutputWriter(output, highlighter), terminalOutputFlusher(highlighter), fixedPromptTimestampClock)
			if _, err := renderer.Toggle(); err != nil {
				t.Fatal(err)
			}
			if err := renderer.Write([]byte("Choose action? ")); err != nil {
				t.Fatal(err)
			}
			stop := startPresentationFlush(ctx, &mu, renderer, highlighter, cancel)
			defer stop()
			select {
			case <-ctx.Done():
			case <-time.After(2 * time.Second):
				t.Fatal("write failure did not cancel the owner")
			}
			for range 2 {
				if err := stop(); !errors.Is(err, io.ErrClosedPipe) {
					t.Fatalf("stop error = %v", err)
				}
			}
			if output.calls != 1 {
				t.Fatalf("retried failed write %d times", output.calls)
			}
		})
	}
}

func TestPresentationFlushCancellationLeavesFinalFlushToOwner(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var output bytes.Buffer
	var mu sync.Mutex
	highlighter := highlight.New(&output)
	renderer := newPromptTimestampRenderer(terminalOutputWriter(&output, highlighter), terminalOutputFlusher(highlighter), fixedPromptTimestampClock)
	if err := renderer.Write([]byte("Choose action? ")); err != nil {
		t.Fatal(err)
	}
	cancel()
	stop := startPresentationFlush(ctx, &mu, renderer, highlighter, cancel)
	for range 2 {
		if err := stop(); err != nil {
			t.Fatal(err)
		}
	}
	if output.Len() != 0 {
		t.Fatal("cancelled worker wrote pending output")
	}
	if err := renderer.Flush(); err != nil {
		t.Fatal(err)
	}
	if output.String() != "Choose action? " {
		t.Fatalf("final output = %q", output.String())
	}
}

// pendingFailureOutput accepts status text, but fails the delayed remote write.
type pendingFailureOutput struct{ lockedBuffer }

func (w *pendingFailureOutput) Write(data []byte) (int, error) {
	if bytes.Contains(data, []byte("Choose action?")) {
		return 0, io.ErrClosedPipe
	}
	return w.lockedBuffer.Write(data)
}

func TestPendingOutputFailureReachesCLI(t *testing.T) {
	for _, mode := range []string{"serial", "attach"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("NO_COLOR", "")
			t.Setenv("CLICOLOR_FORCE", "1")
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			input, writer := io.Pipe()
			defer input.Close()
			defer writer.Close()
			var output pendingFailureOutput
			var err error
			if mode == "serial" {
				terminal := &fakeCLISession{outputData: []byte("Choose action? ")}
				err = runWithIO(ctx, serialArgsWithConfig(t, "serial", "--port", "COM8"), input, &output, func(serialtransport.Config) (cliSession, error) { return terminal, nil })
				if terminal.closeCount() != 1 {
					t.Fatal("serial owner did not close")
				}
			} else {
				attached := &fakeAttachSession{recentData: []byte("Choose action? ")}
				err = runAttachSession(ctx, []string{"SER-1"}, input, &output, func(context.Context, string, string) (attachSession, error) { return attached, nil })
			}
			if !errors.Is(err, io.ErrClosedPipe) {
				t.Fatalf("CLI error = %v, want output failure", err)
			}
		})
	}
}
