package command

import (
	"fmt"
	"io"
	"runtime"
	"strconv"
	"strings"
	"unicode"

	"github.com/akira-init1/ChannelTerm/internal/core/app"
)

// recoveryCommand quotes literal arguments for the explicitly named local shell.
// Control bytes suppress the command rather than becoming terminal or shell input.
// Credentials are never accepted by the callers constructing these argument lists.
func recoveryCommand(args []string, windows bool) (string, bool) {
	quoted := make([]string, 0, len(args))
	for _, arg := range args {
		if strings.IndexFunc(arg, unicode.IsControl) >= 0 {
			return "", false
		}
		escaped := strings.ReplaceAll(arg, "'", "'\"'\"'")
		if windows {
			escaped = strings.ReplaceAll(arg, "'", "''")
		}
		quoted = append(quoted, "'"+escaped+"'")
	}
	label := "POSIX shell"
	if windows {
		label = "PowerShell"
	}
	return label + ": channelterm " + strings.Join(quoted, " ") + "\n", true
}

func writeCheckpointRecovery(output io.Writer, root, id string, options fileOptions) error {
	saved, err := app.ReadTransferCheckpoint(root, id)
	if err != nil {
		_, writeErr := fmt.Fprintln(output, "Checkpoint state unavailable; no verified resume offset. Inspect local state before another operation.")
		return writeErr
	}
	if _, err := fmt.Fprintf(output, "Checkpoint %s: phase=%s, confirmed=%d/%d bytes.\n", saved.ID, saved.Phase, saved.Offset, saved.Size); err != nil {
		return err
	}
	if saved.Phase == "publishing" {
		_, err := fmt.Fprintln(output, "Publication outcome is uncertain. Inspect both destinations; do not replay resume or overwrite.")
		return err
	}
	if saved.Phase == "completed" {
		_, err := fmt.Fprintln(output, "Checkpoint publication is complete; inspect the cleanup error. Do not resume a completed job.")
		return err
	}
	if _, err := fmt.Fprintln(output, "Retained state is not proof the device is ready. Check the live Session and saved source/target; resume revalidates the snapshot and destination prefix. No automatic reconnect or retry."); err != nil {
		return err
	}
	command, ok := recoveryCommand([]string{"file", "resume", id, "--session", options.session, "--endpoint", options.endpoint}, runtime.GOOS == "windows")
	if !ok {
		_, err := fmt.Fprintln(output, "Copyable command unavailable for non-printable target values; use file jobs and select an explicit target.")
		return err
	}
	_, err = io.WriteString(output, command)
	return err
}

// SFTP has no persisted job/offset authority. A later explicit invocation verifies
// the entire destination prefix; progress or payload bytes do not prove durability.
func writeSSHResumeGuidance(output io.Writer, request app.SSHFilesRequest, result app.SSHFilesResult) error {
	if request.Operation != "put" && request.Operation != "get" {
		return nil
	}
	if _, err := fmt.Fprintf(output, "Recovery: %d completed file(s), %d observed payload bytes; final destination state is unconfirmed.\n", result.Files, result.Bytes); err != nil {
		return err
	}
	if request.Protocol != "sftp" || request.Recursive || request.SafeReplace || request.Overwrite || result.Files != 0 {
		_, err := fmt.Fprintln(output, "Inspect completed/partial destinations before a new operation. This mode has no automatic resume command; do not treat staging, overwrite or recursive progress as a resumable offset.")
		return err
	}
	if _, err := fmt.Fprintln(output, "After inspecting the same single-file source and destination, an explicit SFTP --resume can verify the full existing prefix and final SHA-256. Missing/changed source or different destination prefix must be resolved first. Do not combine resume with overwrite, safe replacement or recursive mode. Authentication is requested again; no PTY input is sent."); err != nil {
		return err
	}
	host := request.Config.Host
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	args := []string{"sftp", "--resume", "--port", strconv.Itoa(request.Config.Port)}
	if request.Config.KnownHostsPath != "" {
		args = append(args, "--known-hosts", request.Config.KnownHostsPath)
	}
	for _, path := range append([]string{request.Config.PrivateKeyPath}, request.Config.PrivateKeyPaths...) {
		if path != "" {
			args = append(args, "--identity", path)
		}
	}
	args = append(args, "--", request.Config.User+"@"+host, request.Operation)
	if request.Operation == "put" {
		args = append(args, request.Local, request.Remote)
	} else {
		args = append(args, request.Remote, request.Local)
	}
	command, ok := recoveryCommand(args, runtime.GOOS == "windows")
	if !ok {
		_, err := fmt.Fprintln(output, "Copyable command unavailable for non-printable arguments; re-enter the verified literal paths.")
		return err
	}
	_, err := io.WriteString(output, command)
	return err
}
