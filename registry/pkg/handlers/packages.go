package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/ranscky/neuron-registry/pkg/store"
)

// PackagesHandler handles /v1/packages endpoints
type PackagesHandler struct {
	store store.Store
}

// NewPackagesHandler creates a new PackagesHandler
func NewPackagesHandler(s store.Store) *PackagesHandler {
	return &PackagesHandler{store: s}
}

// ServeHTTP handles the /v1/packages requests
func (h *PackagesHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Extract path segments after /v1/packages/
	path := strings.TrimPrefix(r.URL.Path, "/v1/packages/")
	segments := strings.Split(path, "/")

	w.Header().Set("Content-Type", "application/json")

	switch {
	case len(segments) == 1 && segments[0] != "":
		// GET /v1/packages/:name
		h.handleGetPackage(w, r, segments[0])
	case len(segments) == 2 && segments[0] != "" && segments[1] != "":
		if segments[1] == "versions" {
			// GET /v1/packages/:name/versions
			h.handleListVersions(w, r, segments[0])
		} else {
			// GET /v1/packages/:name/:version
			h.handleGetPackageVersion(w, r, segments[0], segments[1])
		}
	case len(segments) == 3 && segments[0] != "" && segments[1] != "" && segments[2] == "download":
		// GET /v1/packages/:name/:version/download
		h.handleDownloadPackage(w, r, segments[0], segments[1])
	default:
		http.Error(w, `{"error": "not found"}`, http.StatusNotFound)
	}
}

// handleGetPackage handles GET /v1/packages/:name
func (h *PackagesHandler) handleGetPackage(w http.ResponseWriter, r *http.Request, name string) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error": "method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	// Call store.GetLatest
	version, err := h.store.GetLatest(name)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error": "package not found: %s"}`, err.Error()), http.StatusNotFound)
		return
	}

	// Call store.GetManifest
	manifestBytes, err := h.store.GetManifest(name, version)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error": "failed to get manifest: %s"}`, err.Error()), http.StatusInternalServerError)
		return
	}

	// Parse PackageInfo from manifest bytes
	var packageInfo store.PackageInfo
	err = json.Unmarshal(manifestBytes, &packageInfo)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error": "failed to parse manifest: %s"}`, err.Error()), http.StatusInternalServerError)
		return
	}

	// Return parsed PackageInfo as JSON
	json.NewEncoder(w).Encode(packageInfo)
}

// handleGetPackageVersion handles GET /v1/packages/:name/:version
func (h *PackagesHandler) handleGetPackageVersion(w http.ResponseWriter, r *http.Request, name, version string) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error": "method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	// Call store.GetManifest
	manifestBytes, err := h.store.GetManifest(name, version)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error": "package not found: %s"}`, err.Error()), http.StatusNotFound)
		return
	}

	// Parse PackageInfo from manifest bytes
	var packageInfo store.PackageInfo
	err = json.Unmarshal(manifestBytes, &packageInfo)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error": "failed to parse manifest: %s"}`, err.Error()), http.StatusInternalServerError)
		return
	}

	// Return PackageInfo as JSON
	json.NewEncoder(w).Encode(packageInfo)
}

// handleDownloadPackage handles GET /v1/packages/:name/:version/download
func (h *PackagesHandler) handleDownloadPackage(w http.ResponseWriter, r *http.Request, name, version string) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error": "method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	// Call store.GetTarball
	tarballBytes, err := h.store.GetTarball(name, version)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error": "package not found: %s"}`, err.Error()), http.StatusNotFound)
		return
	}

	// Return raw bytes with Content-Type: application/gzip
	w.Header().Set("Content-Type", "application/gzip")
	w.Write(tarballBytes)
}

// handleListVersions handles GET /v1/packages/:name/versions
func (h *PackagesHandler) handleListVersions(w http.ResponseWriter, r *http.Request, name string) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error": "method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	// Call store.ListVersions
	versions, err := h.store.ListVersions(name)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error": "failed to list versions: %s"}`, err.Error()), http.StatusInternalServerError)
		return
	}

	// Return JSON array of version strings
	json.NewEncoder(w).Encode(versions)
}