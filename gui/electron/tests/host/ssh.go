package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"

	gossh "golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// startSSHFixture provides actual SSH password authentication and PTY echo on
// loopback. Its trust file and keys are disposable, never user configuration.
func startSSHFixture() (port, trust string, cleanup func(), err error) {
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", "", nil, err
	}
	signer, err := gossh.NewSignerFromKey(private)
	if err != nil {
		return "", "", nil, err
	}
	directory, err := os.MkdirTemp("", "channelterm-ssh-fixture-")
	if err != nil {
		return "", "", nil, err
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		_ = os.RemoveAll(directory)
		return "", "", nil, err
	}
	_, port, _ = net.SplitHostPort(listener.Addr().String())
	trust = filepath.Join(directory, "known_hosts")
	if err = os.WriteFile(trust, []byte(knownhosts.Line([]string{listener.Addr().String()}, signer.PublicKey())+"\n"), 0600); err != nil {
		_ = listener.Close()
		_ = os.RemoveAll(directory)
		return "", "", nil, err
	}
	config := &gossh.ServerConfig{PasswordCallback: func(meta gossh.ConnMetadata, password []byte) (*gossh.Permissions, error) {
		if meta.User() != "fixture-user" || string(password) != "fixture-password" {
			return nil, errors.New("fixture authentication rejected")
		}
		return nil, nil
	}}
	config.AddHostKey(signer)
	var mutex sync.Mutex
	var connections []net.Conn
	var workers sync.WaitGroup
	accepted := make(chan struct{})
	go func() {
		defer close(accepted)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			mutex.Lock()
			connections = append(connections, conn)
			mutex.Unlock()
			workers.Add(1)
			go func() {
				defer workers.Done()
				defer conn.Close()
				server, channels, requests, err := gossh.NewServerConn(conn, config)
				if err != nil {
					return
				}
				defer server.Close()
				go gossh.DiscardRequests(requests)
				for channel := range channels {
					if channel.ChannelType() != "session" {
						_ = channel.Reject(gossh.UnknownChannelType, "session only")
						continue
					}
					stream, requests, err := channel.Accept()
					if err != nil {
						return
					}
					for req := range requests {
						ok := req.Type == "pty-req" || req.Type == "shell"
						_ = req.Reply(ok, nil)
						if req.Type == "shell" {
							go gossh.DiscardRequests(requests)
							_, _ = stream.Write([]byte("SSH fixture ready\r\n"))
							_, _ = io.Copy(stream, stream)
							_ = stream.Close()
							break
						}
					}
				}
			}()
		}
	}()
	cleanup = func() {
		_ = listener.Close()
		<-accepted
		mutex.Lock()
		for _, conn := range connections {
			_ = conn.Close()
		}
		mutex.Unlock()
		workers.Wait()
		_ = os.RemoveAll(directory)
	}
	return port, trust, cleanup, nil
}
