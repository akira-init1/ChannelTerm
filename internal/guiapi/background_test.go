package guiapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/core/app"
	"github.com/akira-init1/ChannelTerm/internal/core/session"
)

func TestBackgroundMCPRequiresHumanManagementAndEmptyIntent(t *testing.T) {
	manager := session.NewManager()
	defer manager.Close()
	a, err := app.New(app.Dependencies{Manager: manager, Idle: app.IdlePolicy{Origin: "desktop", SessionTimeout: time.Second, ExitTimeout: time.Second}})
	if err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(a)
	for _, tc := range []struct {
		method, kind, origin, body string
		manage                     bool
		want                       int
	}{
		{"GET", "user", "", "", true, 405},
		{"PUT", "user", "https://example.invalid", "", true, 403},
		{"PUT", "user", "", "unexpected", true, 400},
		{"PUT", "user", "", "", false, 403},
		{"PUT", "agent", "", "", true, 403},
		{"PUT", "user", "", "", true, 204},
		{"DELETE", "user", "", "", true, 204},
	} {
		req := httptest.NewRequest(tc.method, BackgroundPath, strings.NewReader(tc.body))
		req.Header.Set("Origin", tc.origin)
		ctx := app.WithClientIdentity(req.Context(), session.ClientIdentity{ID: "CL-test", Kind: tc.kind})
		req = req.WithContext(app.WithClientAccess(ctx, app.ClientAccess{Manage: tc.manage}))
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != tc.want {
			t.Errorf("%+v: HTTP %d", tc, w.Code)
		}
		if w.Code == http.StatusNoContent {
			status, err := a.HostIdlePolicy(ctx)
			if err != nil || (status.ExitTimeoutMS == 0) != (tc.method == http.MethodPut) {
				t.Fatalf("effective policy: %+v %v", status, err)
			}
		}
	}
}
