package guiapi

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/akira-init1/ChannelTerm/internal/core/app"
	"github.com/akira-init1/ChannelTerm/internal/core/session"
)

func TestReleaseBindsHostAndRequiresHumanManagement(t *testing.T) {
	a, err := app.New(app.Dependencies{Manager: session.NewManager()})
	if err != nil {
		t.Fatal(err)
	}
	handle := NewHandler(a)
	call := func(method, body, kind, origin string, manage bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, ReleasePath, strings.NewReader(body))
		r.Header.Set("Origin", origin)
		ctx := app.WithClientIdentity(r.Context(), session.ClientIdentity{ID: "client", Kind: kind})
		w := httptest.NewRecorder()
		handle.ServeHTTP(w, r.WithContext(app.WithClientAccess(ctx, app.ClientAccess{Manage: manage})))
		return w
	}
	if w := call("HEAD", "", "user", "", true); w.Code != 204 {
		t.Fatal(w.Code)
	}
	w := call("GET", "", "user", "", true)
	var preview ReleasePreview
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &preview) != nil || preview.Host == "" || len(preview.Targets) != 0 {
		t.Fatal(w.Code, w.Body)
	}
	for _, tc := range []struct {
		kind, origin string
		manage       bool
	}{{"agent", "", true}, {"user", "", false}, {"user", "https://example.invalid", true}} {
		if w := call("POST", "{}", tc.kind, tc.origin, tc.manage); w.Code != 403 {
			t.Fatal(w.Code)
		}
	}
	req := ReleaseRequest{Host: preview.Host, ID: "board", Reference: "SER-1"}
	body, _ := json.Marshal(req)
	if w := call("POST", string(body), "user", "", true); w.Code != 410 {
		t.Fatal(w.Code)
	}
	handle = NewHandler(a)
	if w := call("POST", string(body), "user", "", true); w.Code != 409 {
		t.Fatal(w.Code)
	}
	if w := call("POST", `{"all":true}`, "user", "", true); w.Code != 400 {
		t.Fatal(w.Code)
	}
}
