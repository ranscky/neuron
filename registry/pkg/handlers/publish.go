package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/ranscky/neuron-registry/pkg/auth"
	"github.com/ranscky/neuron-registry/pkg/store"
)

// PublishHandler handles POST /v1/publish
type PublishHandler struct {
	store   store.Store
	authMgr *auth.Manager
}

// NewPublishHandler creates a new PublishHandler
func NewPublishHandler(s store.Store, m *auth.Manager) *PublishHandler {
	return &PublishHandler{store: s, authMgr: m}
}

// Manifest represents the structure of a neuron.json manifest
type Manifest struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// PublishResponse represents the response structure
type PublishResponse struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Message string `json:"message"`
}

// ServeHTTP handles the POST /v1/publish request
func (h *PublishHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
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

	// Parse multipart form
	err = r.ParseMultipartForm(32 << 20)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error": "failed to parse multipart form: %s"}`, err.Error()), http.StatusBadRequest)
		return
	}

	manifestField, ok := r.MultipartForm.Value["manifest"]
	if !ok || len(manifestField) == 0 {
		http.Error(w, `{"error": "missing manifest field"}`, http.StatusBadRequest)
		return
	}
	manifestJSON := manifestField[0]

	tarballFile, ok := r.MultipartForm.File["tarball"]
	if !ok || len(tarballFile) == 0 {
		http.Error(w, `{"error": "missing tarball field"}`, http.StatusBadRequest)
		return
	}
	file, err := tarballFile[0].Open()
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error": "failed to open tarball: %s"}`, err.Error()), http.StatusBadRequest)
		return
	}
	defer file.Close()
	tarballBytes, err := io.ReadAll(file)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error": "failed to read tarball: %s"}`, err.Error()), http.StatusBadRequest)
		return
	}

	var manifest Manifest
	err = json.Unmarshal([]byte(manifestJSON), &manifest)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error": "invalid manifest JSON: %s"}`, err.Error()), http.StatusBadRequest)
		return
	}
	if manifest.Name == "" || manifest.Version == "" {
		http.Error(w, `{"error": "manifest missing name or version"}`, http.StatusBadRequest)
		return
	}

	// Use the orgID from auth to save the package
	// Signature: Save(orgID, name, version, manifest, tarball)
	err = h.store.Save(orgID, manifest.Name, manifest.Version, []byte(manifestJSON), tarballBytes)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error": "failed to save package: %s"}`, err.Error()), http.StatusInternalServerError)
		return
	}

	response := PublishResponse{
		Name:    manifest.Name,
		Version: manifest.Version,
		Message: "published successfully",
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}
