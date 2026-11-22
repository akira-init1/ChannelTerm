package guiapi

import (
	"errors"
	"net/http"

	"github.com/akira-init1/ChannelTerm/internal/core/app"
)

// BackgroundPath controls fully idle exit for the current desktop companion.
const BackgroundPath = "/gui/v1/background-mcp"

func backgroundHandler(application *app.Application) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.Header.Get("Origin") != "" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		if r.URL.RawQuery != "" || r.ContentLength != 0 {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if r.Method != http.MethodPut && r.Method != http.MethodDelete {
			w.Header().Set("Allow", "PUT, DELETE")
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		err := application.SetDesktopBackground(r.Context(), r.Method == http.MethodPut)
		switch {
		case errors.Is(err, app.ErrClientPermissionDenied):
			w.WriteHeader(http.StatusForbidden)
		case err != nil:
			w.WriteHeader(http.StatusConflict)
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}
}
