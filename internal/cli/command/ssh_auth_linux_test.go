//go:build linux

package command

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSSHInteractiveAuthentication uses a real Linux PTY and generated-key
// loopback server. It needs no external sshd or personal credentials.
func TestSSHInteractiveAuthentication(t *testing.T) {
	for _, mode := range []string{"accept", "reject", "cancel-host", "cancel-password", "wrong-password", "known-host"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			t.Setenv(httpAuthTokenEnvVar, "fixture-auth-token")
			args, _ := sshCommandServerWithPassword(t, true)
			var filtered []string
			var trust string
			for i := 0; i < len(args); i++ {
				if args[i] == "--identity" {
					i++
					continue
				}
				if args[i] == "--known-hosts" {
					trust = args[i+1]
				}
				filtered = append(filtered, args[i])
			}
			if mode != "known-host" {
				trust = filepath.Join(t.TempDir(), ".ssh", "known_hosts")
				filtered = append(filtered, "--known-hosts", trust)
			}
			owner := startSSHPTYProcess(t, append([]string{"ssh"}, filtered...))
			if mode != "known-host" {
				owner.wait(0, "Trust and save this host key (yes/no)? ")
				if mode == "reject" || mode == "cancel-host" {
					if mode == "reject" {
						owner.write("no\r")
					} else {
						owner.write("\x03")
					}
					owner.waitExit(false)
					if _, err := os.Stat(trust); !os.IsNotExist(err) {
						t.Fatal("rejected trust was persisted")
					}
					return
				}
				owner.write("yes\r")
			}
			owner.wait(0, "'s password: ")
			if mode == "cancel-password" {
				owner.write("\x03")
				owner.waitExit(false)
				return
			}
			if mode == "wrong-password" {
				for i := 0; i < 3; i++ {
					after := len(owner.output.String())
					owner.write("fixture-wrong-password\r")
					if i < 2 {
						owner.wait(after, "'s password: ")
					}
				}
				owner.waitExit(false)
			} else {
				owner.write("fixture-password\r")
				owner.wait(0, "test-shell$ ")
				owner.write("uname -a\n")
				owner.wait(0, "Linux test-kernel")
				owner.write("\x1dq")
				owner.waitExit(true)
			}
			if strings.Contains(owner.output.String(), "fixture-password") || strings.Contains(owner.output.String(), "fixture-wrong-password") {
				t.Fatal("password appeared in terminal output")
			}
			if mode == "known-host" && strings.Contains(owner.output.String(), "Trust and save") {
				t.Fatal("known host prompted")
			}
		})
	}
}
