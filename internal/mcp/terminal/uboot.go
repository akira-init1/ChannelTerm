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

// Description explains evidence-based preparation, write effects, and confirmation.
func (*enterUBootTool) Description() string {
	return "Interrupt one U-Boot countdown on an existing serial Session and confirm entry using Host-side raw-byte matching. No skill installation is required.\n\n" +
		"Select: use terminal_list_sessions if the intended Session is unknown. If current evidence already establishes an idle U-Boot prompt, use terminal_write for authorized commands, not this entry tool or Bash-only terminal_exec. An old buffered prompt alone does not establish current state.\n\n" +
		"Prepare: inspect boot output with terminal_read (hex/base64 for non-UTF-8 bytes), supplied firmware configuration, or settings verified for this board and firmware. Select the literal markers and exact interrupt bytes from that evidence. An explicit any-key prompt permits a space; a generic countdown does not identify the required key or string. If a required setting is unknown, ask the user only for that missing detail before calling; reuse known answers. Do not guess or cycle through keys. The tool does not discover settings or ask questions during execution.\n\n" +
		"Run: stay within the user's authorized hardware action; an explicit request to enter U-Boot need not be reconfirmed. Choose wait_reset for manual reset, or reboot only from a verified idle Bash prompt with an explicit command. Before calling, save the terminal_session_events cursor; in another request observe the matching OPERATION_STATE ready event before manual reset. Prepare before resetting rather than relying on model round trips during the countdown. The Host manages its lease and sends the interrupt at most once.\n\n" +
		"Verify: only status entered confirms a fresh line-bound prompt after countdown; bytes_written alone does not. Check isError and structuredContent.result for partial evidence. boot_started means boot proceeded; timeout means entry was not confirmed, not that the key was wrong. On failure report evidence; do not automatically change keys or reboot again. Cancellation releases ownership without recovery keys or closing a healthy Session; a submitted reboot cannot be undone.\n\n" +
		"Limits: a silent countdown or firmware-disabled interruption is not handled by this flow; request firmware details instead of inventing a trigger. There is no dedicated password/credential-protection workflow; do not solicit secrets in chat or treat tool arguments as protected credential storage."
}

// InputSchema describes bounded literal patterns and an explicit interrupt key.
func (*enterUBootTool) InputSchema() tool.InputSchema {
	return tool.InputSchema{Type: "object", Properties: map[string]tool.InputProperty{
		"session_id":            {Type: "string", Description: activeSessionIDDescription},
		"mode":                  {Type: "string", Enum: []string{"wait_reset", "reboot"}, Description: "wait_reset only listens for a user-triggered reset; it does not reset hardware. reboot sends reboot_command through a verified idle Bash prompt, never through U-Boot or another shell."},
		"reboot_command":        {Type: "string", Description: "Explicit single-line Bash command; reboot mode only."},
		"countdown_pattern":     {Type: "string", Description: "Literal 1..256 byte countdown marker from this firmware's output; no regex or wildcards. Use a distinctive fixed portion before changing digits, e.g. 'Autoboot in '. This text alone does not specify the interrupt key."},
		"prompt_pattern":        {Type: "string", Description: "Exact 1..256 byte prompt line from this firmware, including trailing spaces (e.g. 'Zynq> ' or '=> '). Matched against raw bytes at an observed line boundary after countdown, not terminal colors or cursor position."},
		"boot_started_patterns": {Type: "array", Items: &tool.InputProperty{Type: "string"}, Description: "1..8 literal markers showing boot has proceeded, each 1..256 bytes. Use verified firmware output, not generic U-Boot banners; a match takes precedence over prompt success."},
		"interrupt_data_hex":    {Type: "string", Description: "The verified interrupt key or complete non-secret string: 1..32 bytes as hexadecimal pairs without spaces, sent at most once. Encodings: space='20', Esc='1b', Ctrl+C='03', 'stop'='73746f70'. These are alternatives for firmware explicitly accepting that input, not a sequence of probes. No default key; no CR/LF is appended. Ask the user if the required input is unknown."},
		"timeout_ms":            {Type: "integer", Description: "1..180000 milliseconds; default 60000."},
		"diagnostics":           {Type: "string", Enum: []string{"metadata", "capture"}, Description: "Default metadata. capture explicitly opts into retaining sensitive raw terminal evidence for 15 minutes or earlier eviction. If diagnostic_id is returned, download via terminal_read_diagnostic or the diagnostics CLI before expiry or Host exit."},
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
