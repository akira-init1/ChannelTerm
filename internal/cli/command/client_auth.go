package command

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/akira-init1/ChannelTerm/internal/core/app"
	"github.com/akira-init1/ChannelTerm/internal/core/config"
	"github.com/akira-init1/ChannelTerm/internal/core/session"
)

// requireClientToken binds public provenance to independent credentials before
// MCP allocates protocol state. Legacy shared credentials remain unregistered;
// neither actor arguments nor caller-provided client IDs establish identity.
func requireClientToken(legacy string, store config.ClientStore, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		if len(r.Header.Values("Authorization")) != 1 || !strings.HasPrefix(header, "Bearer ") || len(header) > 4096 {
			w.Header().Set("WWW-Authenticate", "Bearer")
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		token := strings.TrimPrefix(header, "Bearer ")
		if legacy != "" && subtle.ConstantTimeCompare([]byte(token), []byte(legacy)) == 1 {
			next.ServeHTTP(w, r)
			return
		}
		client, ok, err := store.Authenticate(token)
		if err != nil {
			http.Error(w, "client authentication unavailable", http.StatusServiceUnavailable)
			return
		}
		if !ok {
			w.Header().Set("WWW-Authenticate", "Bearer")
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		ctx := app.WithClientIdentity(r.Context(), session.ClientIdentity{ID: client.ID, Name: client.Name, Kind: client.Kind})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
