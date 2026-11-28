package command

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/cli/terminalinput"
	"github.com/akira-init1/ChannelTerm/internal/guiapi"
	"golang.org/x/term"
)

// runRelease confirms a Host snapshot before submitting each exact registration once.
func runRelease(ctx context.Context, args []string, input io.Reader, out io.Writer) error {
	flags := flag.NewFlagSet("release", flag.ContinueOnError)
	flags.SetOutput(out)
	all := flags.Bool("all", false, "release every connection listed in the confirmation")
	yes := flags.Bool("yes", false, "skip the interactive confirmation")
	endpoint := flags.String("endpoint", defaultMCPEndpoint, "local authenticated HTTP Host endpoint")
	flags.Usage = func() {
		fmt.Fprintln(out, "Usage: channelterm release TARGET [options] | channelterm release --all [options]")
		fmt.Fprintln(out, "Targets: COM8, /dev/ttyUSB0, /dev/ttyACM0, /dev/cu.usbserial-1234, SER-1, or a Session ID.")
		flags.PrintDefaults()
	}
	target := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		target, args = args[0], args[1:]
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() == 1 && target == "" {
		target = flags.Arg(0)
	} else if flags.NArg() != 0 {
		return errors.New("expected exactly one target or --all")
	}
	if (target != "") == *all {
		return errors.New("specify a serial port, Session ID/reference, or --all")
	}
	console, _ := input.(*os.File)
	if !*yes {
		if console == nil || !term.IsTerminal(int(console.Fd())) {
			return errors.New("non-interactive release requires --yes")
		}
	}
	u, err := url.Parse(*endpoint)
	if err != nil || u.Scheme != "http" || !net.ParseIP(u.Hostname()).IsLoopback() || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path == "" {
		return errors.New("--endpoint requires a numeric loopback HTTP MCP URL")
	}
	if os.Getenv("CHANNELTERM_CLIENT_DELEGATION") != "" {
		return errors.New("release requires a directly authenticated management client")
	}
	// Validate the selected MCP path before addressing the sibling management route.
	probe, stop := context.WithTimeout(ctx, 5*time.Second)
	remote, _, err := connectMCPClient(probe, *endpoint)
	stop()
	if err != nil {
		return err
	}
	defer remote.Close()
	token, err := loadHTTPClientToken()
	if err != nil {
		return err
	}
	client := http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	route := url.URL{Scheme: u.Scheme, Host: u.Host, Path: guiapi.ReleasePath}
	request := func(method string, location url.URL, body io.Reader) (*http.Response, error) {
		req, err := http.NewRequestWithContext(ctx, method, location.String(), body)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		return client.Do(req)
	}
	previewURL := route
	if target != "" {
		previewURL.RawQuery = url.Values{"target": {target}}.Encode()
	}
	resp, err := request(http.MethodGet, previewURL, nil)
	if err != nil {
		return fmt.Errorf("read release preview: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("release preview returned HTTP %d; check the Host version, endpoint and management credential", resp.StatusCode)
	}
	var preview guiapi.ReleasePreview
	if err = json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(&preview); err != nil || preview.Host == "" || preview.Targets == nil || len(preview.Targets) > 10000 {
		return errors.New("invalid Host release preview")
	}
	for _, r := range preview.Targets {
		if r.ID == "" || r.Reference == "" {
			return errors.New("incomplete Host release target")
		}
		fmt.Fprintf(out, "%q  %q  %q  %q  %q\n", r.Reference, r.Endpoint, r.Transport, r.Reason, r.ID)
	}
	if len(preview.Targets) == 0 {
		if *all {
			fmt.Fprintln(out, "This Host has no open connections.")
			return nil
		}
		return fmt.Errorf("this Host does not hold %q", target)
	}
	if !*all && len(preview.Targets) != 1 {
		return errors.New("multiple matches; select one Session ID or reference")
	}
	if !*yes {
		prompt := fmt.Sprintf("Disconnect all users and interrupt work on %d listed connection(s)? [y/N] ", len(preview.Targets))
		answer, err := terminalinput.ReadLine(ctx, console, out, prompt, false)
		if err != nil {
			return err
		}
		line := strings.TrimSpace(string(answer))
		if !strings.EqualFold(line, "y") && !strings.EqualFold(line, "yes") {
			fmt.Fprintln(out, "Cancelled.")
			return nil
		}
	}
	var failed bool
	for _, row := range preview.Targets {
		if err := ctx.Err(); err != nil {
			return err
		}
		body, err := json.Marshal(guiapi.ReleaseRequest{Host: preview.Host, ID: row.ID, Reference: row.Reference})
		if err != nil {
			return err
		}
		res, err := request(http.MethodPost, route, bytes.NewReader(body))
		status := "outcome unknown; refresh before retrying"
		if err == nil {
			res.Body.Close()
			switch res.StatusCode {
			case http.StatusNoContent:
				status = "released"
			case http.StatusGone:
				status = "original registration already ended"
			case http.StatusConflict:
				status = "Host instance changed; not released"
				failed = true
			case http.StatusForbidden, http.StatusUnauthorized, http.StatusTooManyRequests:
				status = fmt.Sprintf("release rejected (HTTP %d)", res.StatusCode)
				failed = true
			default:
				failed = true
			}
		} else {
			failed = true
		}
		fmt.Fprintf(out, "%q %q: %s\n", row.Endpoint, row.Reference, status)
	}
	if failed {
		return errors.New("some releases were not confirmed")
	}
	return nil
}
