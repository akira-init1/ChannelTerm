package guiapi

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/core/app"
)

// ReleasePath previews registrations and releases one confirmed target per request.
const ReleasePath = "/gui/v1/release"

// ReleasePreview is scoped to one live Host route instance.
type ReleasePreview struct {
	Host    string              `json:"host"`
	Targets []app.ReleaseTarget `json:"targets"`
}

// ReleaseRequest pins the Host, opaque ID and non-reused registration reference.
type ReleaseRequest struct {
	Host      string `json:"host"`
	ID        string `json:"id"`
	Reference string `json:"reference"`
}

func releaseHandler(application *app.Application) http.HandlerFunc {
	host := rand.Text()
	active := make(chan struct{}, 4)
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.Header.Get("Origin") != "" || app.ClientIdentityFromContext(r.Context()).Kind != "user" || app.CheckClientAccess(r.Context(), app.ClientManage) != nil {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		switch r.Method {
		case http.MethodHead:
			w.WriteHeader(http.StatusNoContent)
		case http.MethodGet:
			target := r.URL.Query().Get("target")
			if !cleanText(target, 512) || r.ContentLength != 0 {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			rows, err := application.ReleaseTargets(r.Context(), target)
			if err != nil || len(rows) > 10000 {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			reply(w, http.StatusOK, ReleasePreview{host, rows})
		case http.MethodPost:
			var request ReleaseRequest
			d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2048))
			d.DisallowUnknownFields()
			if r.URL.RawQuery != "" || d.Decode(&request) != nil || d.Decode(&struct{}{}) != io.EOF || request.ID == "" || request.Reference == "" || !cleanText(request.ID, 512) || !cleanText(request.Reference, 128) {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			if request.Host != host {
				w.WriteHeader(http.StatusConflict)
				return
			}
			select {
			case active <- struct{}{}:
				// Hold this slot through actual device cleanup, even after an HTTP timeout.
			default:
				w.WriteHeader(http.StatusTooManyRequests)
				return
			}
			ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
			defer cancel()
			finished := make(chan error, 1)
			go func() {
				defer func() { <-active }()
				finished <- application.ReleaseRegistration(ctx, request.ID, request.Reference)
			}()
			var err error
			select {
			case err = <-finished:
			case <-ctx.Done():
				w.WriteHeader(http.StatusGatewayTimeout)
				return
			}
			switch {
			case errors.Is(err, app.ErrSessionNotFound):
				w.WriteHeader(http.StatusGone)
			case err != nil:
				w.WriteHeader(http.StatusInternalServerError)
			default:
				w.WriteHeader(http.StatusNoContent)
			}
		default:
			w.Header().Set("Allow", "HEAD, GET, POST")
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}
}
