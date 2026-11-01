package guiapi

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/core/app"
	"github.com/akira-init1/ChannelTerm/internal/core/channel"
	"github.com/akira-init1/ChannelTerm/internal/core/session"
)

// ResizePath is the fixed desktop size-control route. Credentials remain in
// trusted adapters; it is never exposed as a generic renderer HTTP request.
const ResizePath = "/gui/v1/resize"

type resizeRequest struct {
	Action    string `json:"action"`
	SessionID string `json:"session_id"`
	Owner     string `json:"owner"`
	Columns   uint16 `json:"columns"`
	Rows      uint16 `json:"rows"`
}

// NewResizeHandler exposes only size control; authentication is supplied by the Host.
func NewResizeHandler(application *app.Application) http.Handler {
	active := make(chan struct{}, 4)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/json")
		fail := func(status int, code string) { reply(w, status, map[string]string{"error": code}) }
		if r.URL.Path != ResizePath || r.URL.RawQuery != "" {
			fail(404, "not_found")
			return
		}
		if r.Header.Get("Origin") != "" {
			fail(403, "origin_forbidden")
			return
		}
		if err := app.CheckClientAccess(r.Context(), app.ClientManage); err != nil {
			fail(403, "permission_denied")
			return
		}
		if r.Method == http.MethodGet {
			reply(w, 200, map[string]bool{"available": true})
			return
		}
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "GET, POST")
			fail(405, "method_not_allowed")
			return
		}
		media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || media != "application/json" {
			fail(415, "invalid_content_type")
			return
		}
		select {
		case active <- struct{}{}:
			defer func() { <-active }()
		default:
			fail(429, "busy")
			return
		}
		controller := http.NewResponseController(w)
		_ = controller.SetReadDeadline(time.Now().Add(5 * time.Second))
		defer controller.SetReadDeadline(time.Time{})
		var input resizeRequest
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2048))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF || input.SessionID == "" || !cleanText(input.SessionID, 128) || len(input.Owner) > 64 || !cleanText(input.Owner, 64) {
			fail(400, "invalid_input")
			return
		}
		_ = controller.SetReadDeadline(time.Time{})
		result := map[string]any{"owned": false}
		switch input.Action {
		case "take":
			var owner string
			owner, err = application.TakeResizeControl(r.Context(), input.SessionID)
			result["owner"], result["owned"] = owner, err == nil
		case "release":
			err = application.ReleaseResizeControl(input.SessionID, input.Owner)
		case "status":
			result["owned"], err = application.OwnsResizeControl(input.SessionID, input.Owner)
		case "resize":
			if input.Columns < 1 || input.Columns > 1000 || input.Rows < 1 || input.Rows > 1000 {
				fail(400, "invalid_input")
				return
			}
			err = application.ResizeSessionOwned(r.Context(), input.SessionID, input.Owner, input.Columns, input.Rows)
			result["owned"] = err == nil
		default:
			fail(400, "invalid_input")
			return
		}
		if err != nil {
			switch {
			case errors.Is(err, app.ErrResizeNotOwned):
				fail(409, "resize_not_owned")
			case errors.Is(err, app.ErrSessionBusy):
				fail(409, "busy")
			case errors.Is(err, app.ErrResizeDimensions):
				fail(400, "invalid_input")
			case errors.Is(err, channel.ErrResizeUnsupported):
				fail(422, "unsupported")
			case errors.Is(err, app.ErrSessionNotFound), errors.Is(err, session.ErrNotOpen):
				fail(404, "session_not_found")
			case errors.Is(err, app.ErrClientPermissionDenied):
				fail(403, "permission_denied")
			default:
				fail(422, "resize_failed")
			}
			return
		}
		reply(w, 200, result)
	})
}
