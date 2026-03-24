package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/ranscky/neuron-registry/pkg/store"
)

// PublishHandler handles POST /v1/publish
type PublishHandler struct {
	store store.Store
}

// NewPublishHandler creates a new PublishHandler
func NewPublishHandler(s store.Store) *PublishHandler {
	return &PublishHandler{store: s}
}

// Manifest represents the structure of a neuron.json manifest
type Manifest struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	// Other fields are not required for validation
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

	// Parse multipart form with max memory of 32MB
	err := r.ParseMultipartForm(32 << 20)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error": "failed to parse multipart form: %s"}`, err.Error()), http.StatusBadRequest)
		return
	}

	// Get manifest field
	manifestField, ok := r.MultipartForm.Value["manifest"]
	if !ok || len(manifestField) == 0 {
		http.Error(w, `{"error": "missing manifest field"}`, http.StatusBadRequest)
		return
	}
	manifestJSON := manifestField[0]

	// Get tarball field
	tarballFile, ok := r.MultipartForm.File["tarball"]
	if !ok || len(tarballFile) == 0 {
		http.Error(w, `{"error": "missing tarball field"}`, http.StatusBadRequest)
		return
	}
	
	// Open the tarball file
	file, err := tarballFile[0].Open()
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error": "failed to open tarball: %s"}`, err.Error()), http.StatusBadRequest)
		return
	}
	defer file.Close()

	// Read tarball bytes
	tarballBytes, err := io.ReadAll(file)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error": "failed to read tarball: %s"}`, err.Error()), http.StatusBadRequest)
		return
	}

	// Validate manifest JSON has name and version fields
	var manifest Manifest
	err = json.Unmarshal([]byte(manifestJSON), &manifest)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error": "invalid manifest JSON: %s"}`, err.Error()), http.StatusBadRequest)
		return
	}

	if manifest.Name == "" {
		http.Error(w, `{"error": "manifest missing name field"}`, http.StatusBadRequest)
		return
	}

	if manifest.Version == "" {
		http.Error(w, `{"error": "manifest missing version field"}`, http.StatusBadRequest)
		return
	}

	// Call store.Save with the manifest and tarball bytes
	err = h.store.Save(manifest.Name, manifest.Version, []byte(manifestJSON), tarballBytes)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error": "failed to save package: %s"}`, err.Error()), http.StatusInternalServerError)
		return
	}

	// Return JSON response with name, version, and success message
	response := PublishResponse{
		Name:    manifest.Name,
		Version: manifest.Version,
		Message: "published successfully",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}