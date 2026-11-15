package command

import (
	"context"
	"fmt"
	"strings"

	"github.com/akira-init1/ChannelTerm/internal/core/app"
)

// statusBarText uses a targetless public list, so observation neither renews
// control nor registers another Session use. Matching also pins the original
// never-reused registration; a replacement target must not look like this one.
func (s *mcpAttachSession) statusBarText(ctx context.Context) (string, error) {
	var out struct {
		Sessions []struct {
			ID        string             `json:"session_id"`
			Reference string             `json:"session_ref"`
			Label     string             `json:"label"`
			Endpoint  string             `json:"endpoint"`
			State     string             `json:"state"`
			Control   *app.ControlStatus `json:"control"`
			Lease     *mcpSessionLease   `json:"lease"`
		} `json:"sessions"`
	}
	if err := s.call(ctx, "terminal_list_sessions", map[string]any{}, &out); err != nil {
		return "", err
	}
	for _, item := range out.Sessions {
		if item.Reference != s.recoveryReference || (item.ID != s.id && item.Reference != s.id) {
			continue
		}
		target := item.Label
		if target == "" {
			target = item.Endpoint
		}
		target = statusCells(target, 12) + " " + item.Reference
		state := "control unknown"
		if c := item.Control; c != nil && c.SessionID == item.ID && c.Version != "" {
			access := "read-only"
			if c.Writable && item.State == "open" {
				access = "input allowed"
			}
			owner := c.Client.Kind
			if owner == "" {
				owner = "none"
			}
			if c.Client.Name != "" {
				owner += "/" + statusCells(c.Client.Name, 8)
			}
			parts := []string{owner, c.State, access}
			if item.State != "open" {
				parts = append(parts, item.State)
			}
			if c.AIPaused {
				parts = append(parts, "AI paused")
			}
			if item.Lease != nil {
				parts = append(parts, "op="+item.Lease.Type)
			} else if c.OperationID != "" {
				parts = append(parts, "op="+statusCells(c.OperationID, 8))
			}
			state = strings.Join(parts, " | ")
		}
		return fmt.Sprintf("%s | %s | ^] i: take control", target, state), nil
	}
	return "", ErrAttachedSessionNotFound
}
