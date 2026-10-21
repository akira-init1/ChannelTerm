package terminal

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/core/app"
	"github.com/akira-init1/ChannelTerm/internal/core/tool"
)

type enterUBootTool struct{ *sessionTools }

// Name returns the boot-interruption tool identifier.
func (*enterUBootTool) Name() string { return "terminal_enter_uboot" }

// Description states write effects and the fresh-prompt success requirement.
func (*enterUBootTool) Description() string {
	return "Enter U-Boot on an open serial Session using Host-side raw-byte matching. Explicit mode wait_reset or reboot; reboot requires a verified idle Bash prompt and explicit command. Wait for OPERATION_STATE ready before manual reset. Only a fresh line-bound prompt after countdown confirms entry. Cancellation stops writes without recovery keys. diagnostics=capture explicitly retains sensitive raw evidence for 15 minutes or earlier eviction."
}

// InputSchema describes bounded literal patterns and an explicit interrupt key.
func (*enterUBootTool) InputSchema() tool.InputSchema {
	return tool.InputSchema{Type: "object", Properties: map[string]tool.InputProperty{
		"session_id":            {Type: "string", Description: activeSessionIDDescription},
		"mode":                  {Type: "string", Enum: []string{"wait_reset", "reboot"}},
		"reboot_command":        {Type: "string", Description: "Explicit single-line Bash command; reboot mode only."},
		"countdown_pattern":     {Type: "string", Description: "Literal 1..256 byte countdown marker."},
		"prompt_pattern":        {Type: "string", Description: "Exact 1..256 byte prompt line, including trailing spaces."},
		"boot_started_patterns": {Type: "array", Items: &tool.InputProperty{Type: "string"}, Description: "1..8 literal boot-start markers, each 1..256 bytes."},
		"interrupt_data_hex":    {Type: "string", Description: "1..32 bytes as hexadecimal; sent once."},
		"timeout_ms":            {Type: "integer", Description: "1..180000 milliseconds; default 60000."},
		"diagnostics":           {Type: "string", Enum: []string{"metadata", "capture"}, Description: "Default metadata; capture opts into a raw evidence archive."},
	}, Required: []string{"session_id", "mode", "countdown_pattern", "prompt_pattern", "boot_started_patterns", "interrupt_data_hex"}}
}

// Call delegates the operation and retains partial evidence on error.
func (t *enterUBootTool) Call(ctx context.Context, input json.RawMessage) (tool.Result, error) {
	var args struct {
		SessionID     string   `json:"session_id"`
		Mode          string   `json:"mode"`
		RebootCommand string   `json:"reboot_command"`
		Countdown     string   `json:"countdown_pattern"`
		Prompt        string   `json:"prompt_pattern"`
		BootStarted   []string `json:"boot_started_patterns"`
		Interrupt     string   `json:"interrupt_data_hex"`
		TimeoutMS     *int64   `json:"timeout_ms"`
		Diagnostics   string   `json:"diagnostics"`
	}
	if err := decodeInput(input, &args); err != nil {
		return nil, codedTerminalError{"invalid_input", err}
	}
	if args.Diagnostics != "" && args.Diagnostics != "metadata" && args.Diagnostics != "capture" {
		return nil, codedTerminalError{"invalid_input", errors.New("diagnostics must be metadata or capture")}
	}
	if len(args.Interrupt) > 64 {
		return nil, codedTerminalError{"invalid_input", app.ErrInvalidUBootRequest}
	}
	key, err := hex.DecodeString(args.Interrupt)
	if err != nil {
		return nil, codedTerminalError{"invalid_input", errors.New("interrupt_data_hex must contain complete hexadecimal bytes")}
	}
	timeout := time.Minute
	if args.TimeoutMS != nil {
		if *args.TimeoutMS < 1 || *args.TimeoutMS > 180000 {
			return nil, codedTerminalError{"invalid_input", app.ErrInvalidUBootRequest}
		}
		timeout = time.Duration(*args.TimeoutMS) * time.Millisecond
	}
	r, err := t.application.EnterUBoot(ctx, args.SessionID, app.UBootRequest{Mode: args.Mode, RebootCommand: args.RebootCommand, CountdownPattern: args.Countdown, PromptPattern: args.Prompt, BootStartedPatterns: args.BootStarted, Interrupt: key, Timeout: timeout, Capture: args.Diagnostics == "capture"})
	result := jsonToolResult(r)
	if err != nil {
		code := r.Status
		switch {
		case errors.Is(err, app.ErrInvalidUBootRequest):
			code = "invalid_input"
		case errors.Is(err, app.ErrSessionBusy):
			code = "session_busy"
		case errors.Is(err, app.ErrSessionNotFound):
			code = "session_not_found"
		}
		if code == "" {
			code = "operation_failed"
		}
		return result, codedTerminalError{code, err}
	}
	return result, nil
}
