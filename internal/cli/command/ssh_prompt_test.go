package command

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"testing"
	"time"

	gossh "golang.org/x/crypto/ssh"
)

func TestSSHUnknownPromptAndEchoBeforeEnter(t *testing.T) {
	for _, timestamps := range []bool{false, true} {
		t.Run(fmt.Sprintf("timestamps=%t", timestamps), func(t *testing.T) {
			t.Setenv(httpAuthTokenEnvVar, "test-ssh-prompt-token")
			t.Setenv("NO_COLOR", "")
			t.Setenv("CLICOLOR_FORCE", "1")
			args, closed := sshCommandServerWithShell(t, false, func(ch gossh.Channel) {
				_, _ = io.WriteString(ch, "vendor console [A] ? ")
				reader := bufio.NewReader(ch)
				for {
					key, err := reader.ReadByte()
					if err != nil {
						return
					}
					if key == '\r' {
						_, err = io.WriteString(ch, "\r\nnext vendor [B] ? ")
					} else {
						_, err = ch.Write([]byte{key})
					}
					if err != nil {
						return
					}
				}
			})
			args = append(args, "--highlight=true")
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			input, writer := io.Pipe()
			defer input.Close()
			defer writer.Close()
			var output lockedBuffer
			done := make(chan error, 1)
			go func() { done <- Run(ctx, append([]string{"ssh"}, args...), input, &output, io.Discard) }()
			waitSSHOutput(t, &output, "vendor console [A] ? ")
			if timestamps {
				if _, err := writer.Write([]byte{0x1d, 't'}); err != nil {
					t.Fatal(err)
				}
				waitSSHOutput(t, &output, "Prompt timestamps: ON")
			}
			if _, err := writer.Write([]byte{'\r'}); err != nil {
				t.Fatal(err)
			}
			waitSSHOutput(t, &output, "next vendor [B] ? ")
			for _, echo := range []string{"e", "cho ERROR", "\b \b"} {
				if _, err := writer.Write([]byte(echo)); err != nil {
					t.Fatal(err)
				}
			}
			waitSSHOutput(t, &output, "next vendor [B] ? echo ERROR\b \b")
			if _, err := writer.Write([]byte{0x1d, 'q'}); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-ctx.Done():
				t.Fatal("SSH owner did not exit")
			}
			select {
			case <-closed:
			case <-ctx.Done():
				t.Fatal("SSH fixture remained open")
			}
		})
	}
}
