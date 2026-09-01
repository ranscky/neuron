package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/ranscky/neuron-registry/pkg/auth"
	"github.com/ranscky/neuron-registry/pkg/store"
)

// SearchHandler handles GET /v1/search
type SearchHandler struct {
	store   store.Store
	authMgr *auth.Manager
}

// NewSearchHandler creates a new SearchHandler
func NewSearchHandler(s store.Store, m *auth.Manager) *SearchHandler {
	return &SearchHandler{store: s, authMgr: m}
}

// ServeHTTP handles the GET /v1/search request
func (h *SearchHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error": "method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	// AUTHENTICATION
	rawKey := r.Header.Get("Authorization")
	if rawKey == "" {
		http.Error(w, `{"error": "missing authorization header"}`, http.StatusUnauthorized)
		return
	}
	if len(rawKey) > 7 && rawKey[:7] == "Bearer " {
		rawKey = rawKey[7:]
	}

	key, orgID, err := h.authMgr.Authenticate(rawKey)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error": "auth error: %s"}`, err.Error()), http.StatusInternalServerError)
		return
	}
	if key == nil {
		http.Error(w, `{"error": "invalid or revoked API key"}`, http.StatusUnauthorized)
		return
	}

	query := r.URL.Query().Get("q")
	if query == "" {
		http.Error(w, `{"error": "query parameter 'q' is required"}`, http.StatusBadRequest)
		return
	}

	// Search scoped to the authenticated org
	results, err := h.store.Search(orgID, query)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error": "internal search error: %s"}`, err.Error()), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(results)
}
