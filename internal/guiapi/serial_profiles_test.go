package guiapi

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/akira-init1/ChannelTerm/internal/core/app"
	"github.com/akira-init1/ChannelTerm/internal/core/config"
)

type profileFixture struct {
	calls int
	err   error
}

func (f *profileFixture) ReadSerialProfiles(context.Context) (app.SerialProfiles, error) {
	f.calls++
	return app.SerialProfiles{}, f.err
}
func (f *profileFixture) SaveSerialProfile(context.Context, string, config.SerialProfile, bool) error {
	f.calls++
	return f.err
}
func (f *profileFixture) SetSerialDefault(context.Context, string) error { f.calls++; return f.err }

func TestSerialProfilesRejectUnsafeInputAndSanitizeErrors(t *testing.T) {
	valid := `{"action":"save","name":"board","replace":false,"profile":{"port":"FAKE","baud":115200,"dataBits":8,"parity":"none","stopBits":"1","flowControl":"none","wake":false}}`
	for _, tc := range []struct {
		name, method, body, origin, content string
		err                                 error
		status, calls                       int
	}{
		{"read", "GET", "", "", "", nil, 200, 1},
		{"origin", "GET", "", "https://example.invalid", "", nil, 403, 0},
		{"method", "DELETE", "", "", "", nil, 405, 0},
		{"save", "POST", valid, "", "application/json", nil, 200, 1},
		{"default", "POST", `{"action":"default","name":"board"}`, "", "application/json", nil, 200, 1},
		{"type", "POST", valid, "", "text/plain", nil, 415, 0},
		{"path", "POST", `{"action":"default","name":"board","path":"private"}`, "", "application/json", nil, 400, 0},
		{"invalid", "POST", strings.Replace(valid, "115200", "-1", 1), "", "application/json", nil, 400, 0},
		{"trailing", "POST", valid + "{}", "", "application/json", nil, 400, 0},
		{"oversized", "POST", strings.Repeat("x", 8193), "", "application/json", nil, 400, 0},
		{"conflict", "POST", valid, "", "application/json", app.ErrSerialProfileExists, 409, 1},
		{"missing", "POST", `{"action":"default","name":"board"}`, "", "application/json", config.ErrProfileNotFound, 404, 1},
		{"private", "POST", valid, "", "application/json", errors.New("private-config-path"), 422, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := &profileFixture{err: tc.err}
			handler := newSerialProfilesHandler(fixture)
			r := httptest.NewRequest(tc.method, SerialProfilesPath, strings.NewReader(tc.body))
			r.Header.Set("Origin", tc.origin)
			r.Header.Set("Content-Type", tc.content)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != tc.status || fixture.calls != tc.calls || strings.Contains(w.Body.String(), "private-config-path") {
				t.Fatalf("status=%d calls=%d body=%s", w.Code, fixture.calls, w.Body)
			}
		})
	}
}
