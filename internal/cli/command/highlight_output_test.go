package command

import (
	"bytes"
	"testing"
)

func TestResolveHighlightRendererUsesEnvironmentColorPolicy(t *testing.T) {
	tests := []struct {
		name      string
		enabled   bool
		noColor   string
		force     string
		wantFound bool
	}{
		{name: "redirected output stays plain", enabled: true},
		{name: "forced color", enabled: true, force: "1", wantFound: true},
		{name: "no color wins", enabled: true, noColor: "1", force: "1"},
		{name: "flag disabled", enabled: false, force: "1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("NO_COLOR", tt.noColor)
			t.Setenv("CLICOLOR", "")
			t.Setenv("CLICOLOR_FORCE", tt.force)
			var output bytes.Buffer
			got := resolveHighlightRenderer(tt.enabled, &output)
			if (got != nil) != tt.wantFound {
				t.Errorf("resolveHighlightRenderer() found = %t, want %t", got != nil, tt.wantFound)
			}
		})
	}
}
