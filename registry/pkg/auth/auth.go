// Package auth authenticates registry API keys and carries the calling org to
// handlers through the request context.
//
// Publishing is the only authenticated action: reads stay public, so this is the
// whole authentication surface.
package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/ranscky/neuron-registry/pkg/store"
)

// Resolver maps an API key to the org that owns it.
type Resolver interface {
	OrgForKey(key string) (*store.Org, error)
}

type orgContextKey struct{}

// RequireOrg rejects a request without a valid API key and hands the handler the
// org behind it. The key is never stored in the context, only the org.
func RequireOrg(resolver Resolver, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := BearerToken(r)
		if key == "" {
			writeError(w, http.StatusUnauthorized,
				"publishing requires an API key: send Authorization: Bearer <key>")
			return
		}

		org, err := resolver.OrgForKey(key)
		if err != nil {
			// Deliberately uniform: do not reveal whether a key ever existed.
			writeError(w, http.StatusUnauthorized, "that API key is not valid")
			return
		}

		next.ServeHTTP(w, r.WithContext(WithOrg(r.Context(), org)))
	})
}

// WithOrg attaches an authenticated org to a context.
func WithOrg(ctx context.Context, org *store.Org) context.Context {
	return context.WithValue(ctx, orgContextKey{}, org)
}

// OrgFrom returns the org an authenticated request belongs to.
func OrgFrom(ctx context.Context) (*store.Org, bool) {
	org, ok := ctx.Value(orgContextKey{}).(*store.Org)
	return org, ok && org != nil
}

// BearerToken extracts a bearer token from the Authorization header.
func BearerToken(r *http.Request) string {
	header := r.Header.Get("Authorization")
	const prefix = "bearer "
	if len(header) <= len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return ""
	}
	return strings.TrimSpace(header[len(prefix):])
}

func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}
