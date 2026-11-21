package command

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/akira-init1/ChannelTerm/internal/guiapi"
	sshtransport "github.com/akira-init1/ChannelTerm/internal/transport/ssh"
)

// runSharedSSH makes one creation request, then uses the regular attachment
// lifecycle. Setup interrupts cancel prompts and HTTP; after setup the same
// signal channel belongs to attach (Ctrl+C is terminal input).
func runSharedSSH(ctx context.Context, endpoint string, c sshtransport.Config, label string, password, passphrase, highlight, statusBar bool, input io.Reader, output io.Writer, interrupts <-chan os.Signal) error {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "http" || !net.ParseIP(u.Hostname()).IsLoopback() || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path == "" {
		return errors.New("--endpoint requires an http URL with a numeric loopback address and MCP path")
	}
	if os.Getenv("CHANNELTERM_CLIENT_DELEGATION") != "" {
		return errors.New("shared SSH creation requires a directly authenticated management client")
	}
	id, err := func() (string, error) {
		setup, cancel := context.WithCancel(ctx)
		done := make(chan struct{})
		go func() {
			defer close(done)
			select {
			case <-interrupts:
				cancel()
			case <-setup.Done():
			}
		}()
		defer func() { cancel(); <-done }()
		// Verify the selected MCP path before creating a persistent Session on
		// its origin. A typo in a custom endpoint must not leave a shell behind.
		probe, stop := context.WithTimeout(setup, 5*time.Second)
		remote, _, err := connectMCPClient(probe, endpoint)
		stop()
		if err != nil {
			return "", err
		}
		defer remote.Close()
		return createSharedSSH(setup, u, c, label, password, passphrase)
	}()
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(output, "SSH Session %s\nShared Host: %s\nCtrl+] q detaches. Reattach: channelterm attach %s --endpoint %s\n", id, endpoint, id, endpoint); err != nil {
		return err
	}
	return runAttachSessionWithInterrupts(ctx, []string{"--endpoint", endpoint, "--highlight=" + strconv.FormatBool(highlight), "--status-bar=" + strconv.FormatBool(statusBar), id}, input, output, newMCPAttachSession, interrupts)
}

var sharedSSHIdentifier = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,128}$`)
var sharedSSHChallenge = regexp.MustCompile(`^[0-9a-f]{64}$`)

// createSharedSSH uses the same creation/confirmation protocol as Electron.
// Relative paths are made absolute in the invoking process, before the Host
// resolves them. Secrets are read from the terminal and sent only in the body.
func createSharedSSH(ctx context.Context, endpoint *url.URL, c sshtransport.Config, label string, askPassword, askPassphrase bool) (string, error) {
	if c.Timeout%time.Second != 0 || c.KeepAliveInterval%time.Second != 0 || c.WriteTimeout%time.Second != 0 {
		return "", errors.New("shared Host timeouts must use whole seconds")
	}
	if c.Columns == 0 {
		c.Columns = 80
	}
	if c.Rows == 0 {
		c.Rows = 24
	}
	request := guiapi.SSHRequest{
		User: c.User, Host: c.Host, Port: c.Port, Label: label,
		ConfirmHostKey: c.ConfirmHostKey != nil,
		Advanced: &guiapi.SSHAdvanced{
			TimeoutSeconds:      int(c.Timeout / time.Second),
			KeepAliveSeconds:    int(c.KeepAliveInterval / time.Second),
			KeepAliveCount:      c.KeepAliveCountMax,
			WriteTimeoutSeconds: int(c.WriteTimeout / time.Second),
			Term:                c.Term, Columns: c.Columns, Rows: c.Rows,
		},
	}
	var err error
	request.KnownHostsFile, err = filepath.Abs(c.KnownHostsPath)
	if err != nil {
		return "", err
	}
	for _, path := range append([]string{c.PrivateKeyPath}, c.PrivateKeyPaths...) {
		if path == "" {
			continue
		}
		absolute, err := filepath.Abs(path)
		if err != nil {
			return "", err
		}
		request.IdentityFiles = append(request.IdentityFiles, absolute)
	}
	if askPassword || len(request.IdentityFiles) == 0 {
		if c.Password == nil {
			return "", errors.New("password input requires an interactive terminal")
		}
		value, err := c.Password(ctx)
		if err != nil {
			return "", err
		}
		secret := string(value)
		clear(value)
		request.Password = &secret
	}
	if askPassphrase {
		if c.KeyPassphrase == nil || len(request.IdentityFiles) == 0 {
			return "", errors.New("--ask-passphrase requires an identity and an interactive terminal")
		}
		value, err := c.KeyPassphrase(ctx, request.IdentityFiles[0])
		if err != nil {
			return "", err
		}
		secret := string(value)
		clear(value)
		request.KeyPassphrase = &secret
	}
	token, err := loadHTTPClientToken()
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, 250*time.Second)
	defer cancel()
	// Never replay a create, including on redirects or ambiguous delivery.
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	post := func(path string, payload any) (*http.Response, error) {
		body, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		route := url.URL{Scheme: endpoint.Scheme, Host: endpoint.Host, Path: path}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, route.String(), bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		return client.Do(req)
	}
	response, err := post(guiapi.SSHPath, request)
	if err != nil {
		return "", fmt.Errorf("open SSH on the local Host (check channelterm list before retrying): %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusUnprocessableEntity {
		return "", fmt.Errorf("shared SSH creation returned HTTP %d; check Host version, management credential and SSH settings", response.StatusCode)
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, 8192))
	for attempt := 0; attempt < 2; attempt++ {
		var event struct {
			Event                                  string
			SessionID                              string `json:"session_id"`
			Challenge                              string `json:"challenge_id"`
			Address, Algorithm, Fingerprint, Error string
		}
		if err := decoder.Decode(&event); err != nil {
			return "", fmt.Errorf("read SSH creation result (check channelterm list before retrying): %w", err)
		}
		if event.Error != "" {
			switch event.Error {
			case "authentication_failed":
				return "", errors.New("SSH authentication failed; verify the identity or use --ask-password or --ask-passphrase")
			case "host_key_rejected":
				return "", errors.New("SSH host key rejected; check known_hosts")
			case "cancelled":
				return "", errors.New("SSH setup cancelled")
			default:
				return "", errors.New("Host could not open SSH; check the target and connection settings")
			}
		}
		if response.StatusCode == http.StatusOK && (event.Event == "result" || !request.ConfirmHostKey && event.Event == "") && sharedSSHIdentifier.MatchString(event.SessionID) {
			return event.SessionID, nil
		}
		if attempt != 0 || c.ConfirmHostKey == nil || event.Event != "trust" || !sharedSSHChallenge.MatchString(event.Challenge) ||
			event.Address == "" || event.Algorithm == "" || event.Fingerprint == "" ||
			strings.ContainsFunc(event.Address+event.Algorithm+event.Fingerprint, unicode.IsControl) {
			return "", errors.New("invalid shared SSH result; check channelterm list before retrying")
		}
		accept, err := c.ConfirmHostKey(ctx, sshtransport.HostKey{Address: event.Address, Algorithm: event.Algorithm, Fingerprint: event.Fingerprint})
		if err != nil {
			return "", err
		}
		decision, err := post(guiapi.SSHConfirmPath, map[string]any{"challenge_id": event.Challenge, "accept": accept})
		if err != nil {
			return "", err
		}
		decision.Body.Close()
		if decision.StatusCode != http.StatusOK || !accept {
			return "", errors.New("SSH host key was not approved")
		}
	}
	return "", errors.New("missing SSH creation result")
}
