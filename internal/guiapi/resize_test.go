package guiapi

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/akira-init1/ChannelTerm/internal/core/app"
	"github.com/akira-init1/ChannelTerm/internal/core/session"
)

func TestResizeRouteRejectsUnauthorizedAndMalformedRequests(t *testing.T) {
	m := session.NewManager()
	defer m.Close()
	a, _ := app.New(app.Dependencies{Manager: m})
	h := NewHandler(a)
	for _, test := range []struct {
		name, method, path, body, origin, media string
		access                                  app.ClientAccess
		status                                  int
	}{
		{name: "discovery", method: "GET", status: 200},
		{name: "agent", method: "POST", body: `{"action":"take","session_id":"missing"}`, access: app.ClientAccess{Write: true}, status: 403},
		{name: "read-only", method: "POST", body: `{"action":"resize","session_id":"missing"}`, status: 403},
		{name: "origin", method: "POST", origin: "http://127.0.0.1", status: 403},
		{name: "unknown field", method: "POST", body: `{"action":"take","session_id":"missing","url":"bad"}`, status: 400},
		{name: "trailing value", method: "POST", body: `{"action":"take","session_id":"missing"}{}`, status: 400},
		{name: "oversized", method: "POST", body: strings.Repeat(" ", 2049), status: 400},
		{name: "fraction", method: "POST", body: `{"action":"resize","session_id":"missing","columns":1.5}`, status: 400},
		{name: "unknown action", method: "POST", body: `{"action":"execute","session_id":"missing"}`, status: 400},
		{name: "unknown session", method: "POST", body: `{"action":"take","session_id":"missing"}`, status: 404},
		{name: "query", method: "POST", path: ResizePath + "?owner=secret", status: 404},
		{name: "method", method: "DELETE", status: 405},
		{name: "media", method: "POST", media: "text/plain", status: 415},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := test.path
			if path == "" {
				path = ResizePath
			}
			r := httptest.NewRequest(test.method, path, strings.NewReader(test.body))
			media := test.media
			if media == "" {
				media = "application/json"
			}
			r.Header.Set("Content-Type", media)
			r.Header.Set("Origin", test.origin)
			access := test.access
			if test.name != "agent" && test.name != "read-only" {
				access = app.ClientAccess{Manage: true, Write: true}
			}
			r = r.WithContext(app.WithClientAccess(r.Context(), access))
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != test.status {
				t.Fatalf("status %d: %s", w.Code, w.Body.String())
			}
			if strings.Contains(w.Body.String(), "secret") {
				t.Fatal("leaked request secret")
			}
		})
	}
}
