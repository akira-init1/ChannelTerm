package terminal

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/core/app"
	"github.com/akira-init1/ChannelTerm/internal/core/tool"
)

type fileTransferTool struct{ application *app.Application }

type listFileTransfersTool struct{ application *app.Application }

func (*listFileTransfersTool) Name() string { return "file_transfer_list" }
func (*listFileTransfersTool) Description() string {
	return "Read up to 128 public Host-managed transfer summaries across clients. Only initiator, correlation IDs, state and byte counts; no paths, SSH profiles, errors or cancellation authority. Independent CLI/desktop SSH copies are not Host tasks. Does not renew presence or wait for device I/O."
}
func (*listFileTransfersTool) InputSchema() tool.InputSchema {
	return tool.InputSchema{Type: "object", Properties: map[string]tool.InputProperty{}}
}
func (t *listFileTransfersTool) Call(ctx context.Context, input json.RawMessage) (tool.Result, error) {
	var q *struct{}
	if err := decodeDebug(input, &q); err != nil {
		return nil, err
	}
	if q == nil {
		return nil, errors.New("transfer list requires an empty object")
	}
	rows, err := t.application.ListFileTransfers(ctx)
	return tool.Result{"transfers": rows}, err
}

func (t *fileTransferTool) Name() string { return "file_transfer" }
func (t *fileTransferTool) Description() string {
	return "Transfer Host-local files through one managed task: start, status, cancel or interrupt. interrupt requires a live user instance with management permission and cancels exactly one independent SFTP/SCP task across clients; it returns only a public transfer summary, may still be running during cleanup, and never writes Ctrl+C to a terminal. Serial tasks use terminal_control takeover. Supports one file, multiple paths, directories, TAR and tar.gz over serial or SSH. auto preserves files and may temporarily bundle serial uploads; tar/tar.gz explicitly keeps an archive. Host paths are relative to configured file_transfer.root, not the MCP client's computer. SSH uses a configured profile with strict host-key checking. All copies verify SHA-256 and preflight space. Start returns immediately; poll status until completed/failed/cancelled. Retry start with the same request_id and unchanged arguments. No automatic overwrite or resume."
}
func (t *fileTransferTool) InputSchema() tool.InputSchema {
	return tool.InputSchema{Type: "object", Required: []string{"action"}, Properties: map[string]tool.InputProperty{
		"action":          {Type: "string", Enum: []string{"start", "status", "cancel", "interrupt"}},
		"request_id":      {Type: "string", Description: "Required for start; unique 1..128-character retry key. Exact retries return the retained task."},
		"transfer_id":     {Type: "string", Description: "Required for status/cancel/interrupt; immutable task ID. status/cancel stay bound to the authenticated client; interrupt is human management only."},
		"wait_ms":         {Type: "integer", Description: "status/cancel only: 0..30000 ms; timeout returns a snapshot without cancelling work."},
		"direction":       {Type: "string", Enum: []string{"send", "receive"}, Description: "Required for start. Host to remote, or remote to Host."},
		"transport":       {Type: "string", Enum: []string{"serial", "sftp", "scp"}, Description: "Required for start. SCP also requires SFTP for metadata/capacity checks."},
		"session_id":      {Type: "string", Description: "serial only: active idle POSIX-shell Session ID or short reference."},
		"ssh_profile":     {Type: "string", Description: "sftp/scp only: name under Host file_transfer.ssh; never credentials or arbitrary key paths."},
		"sources":         {Type: "array", Items: &tool.InputProperty{Type: "string"}, Description: "start: 1..128 literal paths with distinct basenames. Host paths relative to transfer root; remote paths absolute POSIX. No glob or shell expansion."},
		"destination":     {Type: "string", Description: "start: exact output path, with existing parent on Host/SSH. One directory copies its contents; multiple sources retain basenames under this directory. Archive modes require an output file path."},
		"mode":            {Type: "string", Enum: []string{"auto", "files", "tar", "tar.gz"}, Description: "Default auto preserves files/structure and selects wire strategy. files forces per-file serial copies. tar/tar.gz explicitly produces an archive, without destination extraction. Existing archives in auto/files remain ordinary files."},
		"timeout_seconds": {Type: "integer", Description: "start only: 1..86400 seconds; default 3600, including preparation, transfer and publication."},
	}}
}

func (t *fileTransferTool) Call(ctx context.Context, input json.RawMessage) (tool.Result, error) {
	if len(input) > 32*1024 {
		return nil, errors.New("file-transfer request exceeds 32 KiB")
	}
	var q struct {
		app.TransferRequest
		Action     string `json:"action"`
		TransferID string `json:"transfer_id"`
		WaitMS     int    `json:"wait_ms"`
	}
	d := json.NewDecoder(bytes.NewReader(input))
	d.DisallowUnknownFields()
	if err := d.Decode(&q); err != nil {
		return nil, err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return nil, errors.New("expected one file-transfer request object")
	}
	var status app.TransferStatus
	var err error
	switch q.Action {
	case "start":
		if q.TransferID != "" || q.WaitMS != 0 {
			return nil, errors.New("start does not accept transfer_id or wait_ms")
		}
		status, err = t.application.StartFileTransfer(ctx, q.TransferRequest)
	case "status", "cancel", "interrupt":
		if q.TransferID == "" || q.WaitMS < 0 || q.WaitMS > 30000 {
			return nil, errors.New("status/cancel/interrupt requires transfer_id and wait_ms in 0..30000")
		}
		encoded, _ := json.Marshal(q.TransferRequest)
		empty, _ := json.Marshal(app.TransferRequest{})
		if !bytes.Equal(encoded, empty) {
			return nil, errors.New("status/cancel/interrupt accepts only action, transfer_id and wait_ms")
		}
		if q.Action == "interrupt" {
			if q.WaitMS != 0 {
				return nil, errors.New("interrupt does not accept wait_ms")
			}
			summary, err := t.application.InterruptSSHFileTransfer(ctx, q.TransferID)
			return tool.Result{"transfer": summary}, err
		}
		status, err = t.application.FileTransferStatus(ctx, q.TransferID, q.Action == "cancel", time.Duration(q.WaitMS)*time.Millisecond)
	default:
		return nil, errors.New("action must be start, status, cancel or interrupt")
	}
	if err != nil {
		return nil, err
	}
	return tool.Result{"status": status}, nil
}
