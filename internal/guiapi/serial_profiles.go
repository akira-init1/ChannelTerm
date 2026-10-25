package guiapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/core/app"
	"github.com/akira-init1/ChannelTerm/internal/core/config"
)

// SerialProfilesPath exposes only serial profiles, never arbitrary config paths.
const SerialProfilesPath = "/gui/v1/serial-profiles"

type serialProfilesService interface {
	ReadSerialProfiles(context.Context) (app.SerialProfiles, error)
	SaveSerialProfile(context.Context, string, config.SerialProfile, bool) error
	SetSerialDefault(context.Context, string) error
}

type serialProfile struct {
	Port        string `json:"port"`
	Baud        int    `json:"baud"`
	DataBits    int    `json:"dataBits"`
	Parity      string `json:"parity"`
	StopBits    string `json:"stopBits"`
	FlowControl string `json:"flowControl"`
	Wake        bool   `json:"wake"`
}

func (p serialProfile) config() config.SerialProfile {
	return config.SerialProfile{Port: p.Port, BaudRate: p.Baud, DataBits: p.DataBits, Parity: p.Parity, StopBits: p.StopBits, FlowControl: p.FlowControl, Wake: p.Wake}
}

func validProfileName(name string) bool {
	return name != "" && name == strings.TrimSpace(name) && cleanText(name, 128)
}
func (p serialProfile) valid() bool {
	return strings.TrimSpace(p.Port) != "" && cleanText(p.Port, 512) && p.Baud >= 1 && p.Baud <= 4000000 && p.DataBits >= 5 && p.DataBits <= 8 &&
		(p.Parity == "none" || p.Parity == "odd" || p.Parity == "even" || p.Parity == "mark" || p.Parity == "space") &&
		(p.StopBits == "1" || p.StopBits == "1.5" || p.StopBits == "2") &&
		(p.FlowControl == "none" || p.FlowControl == "hardware" || p.FlowControl == "software")
}

// NewHandler composes desktop routes behind the embedding Host's bearer-token
// middleware. Unknown routes do not fall through to a configuration operation.
func NewHandler(application *app.Application) http.Handler {
	mux := http.NewServeMux()
	ssh := NewSSHHandler(application)
	mux.Handle(SSHPath, ssh)
	mux.Handle(SSHConfirmPath, ssh)
	mux.Handle(SerialProfilesPath, newSerialProfilesHandler(application))
	return mux
}

// newSerialProfilesHandler bounds concurrent operations and body/lock waits.
// Errors never expose filesystem paths, configuration contents or credentials.
func newSerialProfilesHandler(application serialProfilesService) http.Handler {
	active := make(chan struct{}, 2)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		fail := func(status int, code string) { reply(w, status, map[string]string{"error": code}) }
		if r.URL.RawQuery != "" {
			fail(404, "not_found")
			return
		}
		if r.Header.Get("Origin") != "" {
			fail(403, "origin_forbidden")
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodPost {
			w.Header().Set("Allow", "GET, POST")
			fail(405, "method_not_allowed")
			return
		}
		select {
		case active <- struct{}{}:
			defer func() { <-active }()
		default:
			fail(429, "busy")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		if r.Method == http.MethodGet {
			snapshot, err := application.ReadSerialProfiles(ctx)
			if err != nil || len(snapshot.Profiles) > 256 || !cleanText(snapshot.Default, 128) {
				fail(422, "configuration")
				return
			}
			type namedProfile struct {
				Name    string        `json:"name"`
				Profile serialProfile `json:"profile"`
			}
			profiles := make([]namedProfile, 0, len(snapshot.Profiles))
			for _, entry := range snapshot.Profiles {
				p := entry.Profile
				wire := serialProfile{p.Port, p.BaudRate, p.DataBits, p.Parity, p.StopBits, p.FlowControl, p.Wake}
				if !validProfileName(entry.Name) || !wire.valid() {
					fail(422, "configuration")
					return
				}
				profiles = append(profiles, namedProfile{entry.Name, wire})
			}
			reply(w, 200, map[string]any{"default": snapshot.Default, "profiles": profiles})
			return
		}
		media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || media != "application/json" {
			fail(415, "invalid_content_type")
			return
		}
		controller := http.NewResponseController(w)
		_ = controller.SetReadDeadline(time.Now().Add(5 * time.Second))
		defer controller.SetReadDeadline(time.Time{})
		var input struct {
			Action  string         `json:"action"`
			Name    string         `json:"name"`
			Profile *serialProfile `json:"profile,omitempty"`
			Replace bool           `json:"replace,omitempty"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF || !validProfileName(input.Name) {
			fail(400, "invalid_input")
			return
		}
		switch input.Action {
		case "save":
			if input.Profile == nil || !input.Profile.valid() {
				fail(400, "invalid_input")
				return
			}
			err = application.SaveSerialProfile(ctx, input.Name, input.Profile.config(), input.Replace)
		case "default":
			if input.Profile != nil || input.Replace {
				fail(400, "invalid_input")
				return
			}
			err = application.SetSerialDefault(ctx, input.Name)
		default:
			fail(400, "invalid_input")
			return
		}
		if errors.Is(err, app.ErrSerialProfileExists) {
			fail(409, "profile_exists")
			return
		}
		if errors.Is(err, config.ErrProfileNotFound) {
			fail(404, "profile_not_found")
			return
		}
		if err != nil {
			fail(422, "configuration")
			return
		}
		reply(w, 200, map[string]bool{"saved": true})
	})
}
