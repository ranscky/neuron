package handlers

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/ranscky/neuron-registry/pkg/auth"
	"github.com/ranscky/neuron-registry/pkg/store"
)

// OrgStore is the slice of the store that org management needs.
type OrgStore interface {
	CreateOrg(name string) (*store.Org, string, error)
	CreateAPIKey(orgID, label string) (string, error)
}

// OrgHandler creates orgs and API keys.
type OrgHandler struct {
	store OrgStore
	// adminToken gates org creation. Empty disables it, so an instance that was
	// never configured cannot be claimed by whoever finds it first.
	adminToken string
}

// NewOrgHandler builds an org handler. adminToken comes from the environment.
func NewOrgHandler(s OrgStore, adminToken string) *OrgHandler {
	return &OrgHandler{store: s, adminToken: adminToken}
}

// Create handles POST /v1/orgs, gated by the registry's admin token.
func (h *OrgHandler) Create(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if h.adminToken == "" {
		writeJSONError(w, http.StatusServiceUnavailable,
			"org creation is disabled: set NEURON_REGISTRY_ADMIN_TOKEN on the registry")
		return
	}
	given := r.Header.Get("X-Admin-Token")
	if subtle.ConstantTimeCompare([]byte(given), []byte(h.adminToken)) != 1 {
		writeJSONError(w, http.StatusUnauthorized, "invalid admin token")
		return
	}

	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	org, key, err := h.store.CreateOrg(req.Name)
	switch {
	case err == nil:
	case errors.Is(err, store.ErrInvalidOrgName):
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	case errors.Is(err, store.ErrOrgExists):
		writeJSONError(w, http.StatusConflict, err.Error())
		return
	default:
		writeJSONError(w, http.StatusInternalServerError, "could not create the org")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"org": map[string]string{
			"id":   org.ID,
			"name": org.Name,
		},
		"api_key": key,
		"note":    "store this key now — only its hash is kept, so it cannot be shown again",
	})
}

// CreateKey handles POST /v1/keys for the authenticated org.
func (h *OrgHandler) CreateKey(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	org, ok := auth.OrgFrom(r.Context())
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "an API key is required")
		return
	}

	var req struct {
		Label string `json:"label"`
	}
	if r.ContentLength > 0 {
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
	}

	key, err := h.store.CreateAPIKey(org.ID, req.Label)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "could not create the key")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"org": map[string]string{
			"id":   org.ID,
			"name": org.Name,
		},
		"api_key": key,
		"note":    "store this key now — only its hash is kept, so it cannot be shown again",
	})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
