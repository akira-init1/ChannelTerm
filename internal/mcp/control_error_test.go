package mcp

import (
	"github.com/akira-init1/ChannelTerm/internal/core/app"
	"testing"
)

func TestControlErrorsForbidAutomaticRetry(t *testing.T) {
	for _, err := range []error{app.ErrControlStale, app.ErrControlRequired, app.ErrControlBusy, app.ErrAgentPaused, app.ErrControlInterrupted} {
		result := errorResult("terminal_write", err)
		detail := result.StructuredContent.(map[string]any)["error"].(map[string]any)
		if !result.IsError || detail["retry_allowed"] != false {
			t.Fatalf("unsafe error: %#v", detail)
		}
	}
}
