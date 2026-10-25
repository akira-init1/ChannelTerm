package command

import (
	"crypto/subtle"
	"net/http"
	"os"
	"strings"

	"github.com/akira-init1/ChannelTerm/internal/core/app"
	"github.com/akira-init1/ChannelTerm/internal/core/config"
	"github.com/akira-init1/ChannelTerm/internal/core/session"
)

// requireClientToken binds public provenance to independent credentials before
// MCP allocates protocol state. Legacy shared credentials are read-only;
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
			next.ServeHTTP(w, r.WithContext(app.WithClientAccess(r.Context(), app.ClientAccess{})))
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
		ctx := app.WithClientAccess(r.Context(), app.ClientAccess{Write: client.WriteAllowed || client.Kind == "user", Manage: client.Kind == "user"})
		ctx = app.WithClientIdentity(ctx, session.ClientIdentity{ID: client.ID, Name: client.Name, Kind: client.Kind})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// loadHTTPClientToken preserves explicit client credentials. Only the trusted
// local account's implicit default uses the separate management registration.
func loadHTTPClientToken() (string, error) {
	token, err := loadHTTPAuthToken()
	if err != nil || strings.TrimSpace(os.Getenv(httpAuthTokenEnvVar)) != "" {
		return token, err
	}
	store, err := config.DefaultClientStore(token)
	if err != nil {
		return "", err
	}
	return store.LoadOrCreateLocalClientToken()
}

// requireDesktopManagement applies the same application grants to non-MCP
// desktop routes. GET observes profiles; all current mutations require management.
func requireDesktopManagement(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		effect := app.ClientManage
		if r.Method == http.MethodGet {
			effect = app.ClientRead
		}
		if err := app.CheckClientAccess(r.Context(), effect); err != nil {
			http.Error(w, "client permission denied", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// localHTTPTokenKey is only set by a foreground Host composition for its own
// attachment. External client inputs and environment credentials cannot set it.
type localHTTPTokenKey struct{}
