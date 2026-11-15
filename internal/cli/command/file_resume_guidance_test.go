package command

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/akira-init1/ChannelTerm/internal/core/app"
	sshtransport "github.com/akira-init1/ChannelTerm/internal/transport/ssh"
)

func TestRecoveryCommandsQuoteLiteralArgumentsAndRejectControlBytes(t *testing.T) {
	for _, windows := range []bool{false, true} {
		command, ok := recoveryCommand([]string{"sftp", "a'b $(`x`)", "--", "-literal"}, windows)
		if !ok || !strings.Contains(command, "'-literal'") {
			t.Fatal(command)
		}
		if windows && !strings.Contains(command, "'a''b $(`x`)'") {
			t.Fatal(command)
		}
		if !windows && !strings.Contains(command, "'a'\"'\"'b $(`x`)'") {
			t.Fatal(command)
		}
		if _, ok := recoveryCommand([]string{"injected\ncommand"}, windows); ok {
			t.Fatal("control bytes printed")
		}
	}
}

func TestCheckpointGuidanceReadsActualPhaseAndNeverResumesUncertainPublication(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(t.TempDir(), "input")
	if err := os.WriteFile(source, []byte("snapshot"), 0600); err != nil {
		t.Fatal(err)
	}
	saved, err := app.CreateTransferCheckpoint(context.Background(), root, "send", source, "/tmp/file", "peer", app.FileTransferWireBase64)
	if err != nil {
		t.Fatal(err)
	}
	options := fileOptions{session: "opaque-target", endpoint: defaultMCPEndpoint}
	for _, phase := range []string{"preparing", "transferring", "publishing", "completed"} {
		saved.Phase = phase
		data, _ := json.Marshal(saved)
		if err := os.WriteFile(filepath.Join(root, saved.ID, "checkpoint.json"), data, 0600); err != nil {
			t.Fatal(err)
		}
		var output bytes.Buffer
		if err := writeCheckpointRecovery(&output, root, saved.ID, options); err != nil {
			t.Fatal(err)
		}
		text := output.String()
		if !strings.Contains(text, "phase="+phase) || !strings.Contains(text, "confirmed=0/8") {
			t.Fatal(text)
		}
		command := strings.Contains(text, "channelterm 'file' 'resume'")
		if command != (phase == "preparing" || phase == "transferring") {
			t.Fatal(text)
		}
	}
	var missing bytes.Buffer
	if err := writeCheckpointRecovery(&missing, root, strings.Repeat("0", 24), options); err != nil || strings.Contains(missing.String(), "channelterm 'file' 'resume'") {
		t.Fatal(missing.String(), err)
	}
}

func TestSSHFailureGuidancePreservesFailureAndDoesNotClaimAResumeOffset(t *testing.T) {
	failure := errors.New("fixture transport failure")
	for _, extra := range [][]string{nil, {"--resume"}, {"--overwrite"}, {"--recursive"}, {"--overwrite", "--safe-replace"}} {
		var output bytes.Buffer
		args := append([]string{"user@host", "put", "local name", "/tmp/remote name", "--identity", "fixture-key"}, extra...)
		err := runSSHFiles(context.Background(), "sftp", args, strings.NewReader(""), &output, func(context.Context, app.SSHFilesRequest) (app.SSHFilesResult, error) {
			return app.SSHFilesResult{Bytes: 4}, failure
		})
		if !errors.Is(err, failure) || !strings.Contains(output.String(), "unconfirmed") {
			t.Fatal(output.String(), err)
		}
		command := strings.Contains(output.String(), "channelterm 'sftp'")
		if command != (len(extra) == 0 || extra[0] == "--resume") {
			t.Fatal(output.String())
		}
		if strings.Contains(output.String(), "confirmed=4") {
			t.Fatal("payload count became resume authority")
		}
	}
	request := app.SSHFilesRequest{Config: sshtransport.Config{User: "user", Host: "host", Port: 22, Password: func(context.Context) ([]byte, error) {
		t.Fatal("guidance must not request a password")
		return nil, nil
	}}, Protocol: "sftp", Operation: "get", Remote: "/tmp/remote", Local: "file"}
	var output bytes.Buffer
	if err := writeSSHResumeGuidance(&output, request, app.SSHFilesResult{}); err != nil || !strings.Contains(output.String(), "'get' '/tmp/remote' 'file'") {
		t.Fatal(output.String(), err)
	}
}
