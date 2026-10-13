package app

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gossh "golang.org/x/crypto/ssh"
)

// TestSCPVerifiedTree checks exact destination mapping through nested entries and
// preserves earlier completed counts when a later file's verification fails.
func TestSCPVerifiedTree(t *testing.T) {
	for _, operation := range []string{"put", "get"} {
		for _, mismatch := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/mismatch=%t", operation, mismatch), func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				rootName := "tree"
				if operation == "get" {
					rootName = "newtree"
				}
				payload := []byte{'A', 0, 'B'}
				config := fileServerWithRequests(t, false, func(ch gossh.Channel, request *gossh.Request) {
					var exec struct{ Command string }
					if gossh.Unmarshal(request.Payload, &exec) != nil {
						t.Error("invalid exec")
						return
					}
					exit := func() { _, _ = ch.SendRequest("exit-status", false, gossh.Marshal(struct{ Status uint32 }{0})) }
					switch {
					case exec.Command == "command -v sha256sum >/dev/null":
						exit()
					case exec.Command == "if test -d '/newtree'; then printf d; else printf f; fi":
						_, _ = io.WriteString(ch, "f")
						exit()
					case strings.HasPrefix(exec.Command, "sha256sum < "):
						var data []byte
						if exec.Command == "sha256sum < '/newtree/nested/b'" {
							data = payload
							if mismatch {
								data = []byte("bad")
							}
						} else if exec.Command != "sha256sum < '/newtree/a'" {
							t.Errorf("wrong tree path: %q", exec.Command)
							return
						}
						_, _ = fmt.Fprintf(ch, "%x  -\n", sha256.Sum256(data))
						exit()
					default:
						if !strings.Contains(exec.Command, "scp -r ") {
							t.Errorf("unexpected command: %q", exec.Command)
							return
						}
						reader := bufio.NewReader(ch)
						ack := func() bool { b, err := reader.ReadByte(); return err == nil && b == 0 }
						if operation == "put" {
							_, _ = ch.Write([]byte{0})
						} else if !ack() {
							return
						}
						for _, record := range []struct {
							header string
							data   []byte
						}{
							{"D0700 0 " + rootName + "\n", nil}, {"C0600 0 a\n", nil},
							{"D0700 0 nested\n", nil}, {"C0600 3 b\n", payload}, {"E\n", nil}, {"E\n", nil},
						} {
							if operation == "put" {
								line, err := reader.ReadString('\n')
								if err != nil {
									return
								} // A checksum failure closes the connection mid-tree.
								if len(line) != len(record.header) || line[0] != record.header[0] || len(line) > 5 && line[5:] != record.header[5:] {
									t.Errorf("header=%q want=%q", line, record.header)
									return
								}
								_, _ = ch.Write([]byte{0})
								if line[0] == 'C' {
									got := make([]byte, len(record.data)+1)
									if _, err := io.ReadFull(reader, got); err != nil || !bytes.Equal(got, append(append([]byte{}, record.data...), 0)) {
										t.Errorf("payload=%v %v", got, err)
										return
									}
									_, _ = ch.Write([]byte{0})
								}
							} else {
								_, _ = io.WriteString(ch, record.header)
								if !ack() {
									return
								}
								if record.header[0] == 'C' {
									_, _ = ch.Write(append(append([]byte{}, record.data...), 0))
									if !ack() {
										return
									}
								}
							}
						}
						if operation == "put" {
							_, _ = io.Copy(io.Discard, reader)
						}
						exit()
					}
				})
				local := filepath.Join(t.TempDir(), "tree")
				if operation == "put" {
					if err := os.MkdirAll(filepath.Join(local, "nested"), 0700); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(local, "a"), nil, 0600); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(local, "nested", "b"), payload, 0600); err != nil {
						t.Fatal(err)
					}
				}
				result, err := RunSSHFiles(ctx, SSHFilesRequest{Config: config, Protocol: "scp", Operation: operation, Local: local, Remote: "/newtree", Recursive: true, Overwrite: true, Verify: "sha256"})
				want := int64(2)
				if mismatch {
					want = 1
				}
				if result.Files != want || result.VerifiedFiles != want || result.Bytes != 3 || (err != nil) != mismatch {
					t.Fatalf("result=%+v error=%v", result, err)
				}
				if mismatch && !strings.Contains(err.Error(), "SHA-256 mismatch") {
					t.Fatal(err)
				}
			})
		}
	}
}
