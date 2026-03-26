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
	// Strip /v1/packages/ prefix from the path
	path := strings.TrimPrefix(r.URL.Path, "/v1/packages/")
	
	// Handle empty path
	if path == "" || path == "/" {
		http.Error(w, `{"error": "not found"}`, http.StatusNotFound)
		return
	}
	
	// Remove leading slash if present
	path = strings.TrimPrefix(path, "/")
	
	w.Header().Set("Content-Type", "application/json")

	// Parse the path to extract name and version/action
	// For paths with slashes in package names, we need to manually parse
	// We'll split by "/" and analyze the last segments to determine the action
	
	segments := strings.Split(path, "/")
	
	// Handle different path patterns
	switch {
	case len(segments) >= 2 && segments[len(segments)-1] == "download":
		// GET /v1/packages/:name/:version/download
		// Name is everything except the last two segments
		if len(segments) < 3 {
			http.Error(w, `{"error": "invalid path"}`, http.StatusBadRequest)
			return
		}
		version := segments[len(segments)-2]
		name := strings.Join(segments[:len(segments)-2], "/")
		h.handleDownloadPackage(w, r, name, version)
		
	case len(segments) >= 2 && segments[len(segments)-1] == "versions":
		// GET /v1/packages/:name/versions
		// Name is everything except the last segment
		name := strings.Join(segments[:len(segments)-1], "/")
		h.handleListVersions(w, r, name)
		
	case len(segments) >= 2:
		// GET /v1/packages/:name/:version
		// Check if the last segment is a version or if it's just a name with slashes
		version := segments[len(segments)-1]
		name := strings.Join(segments[:len(segments)-1], "/")
		
		// If the last segment looks like a version (contains dots or numbers), treat as version
		// Otherwise, treat as just a name request
		if strings.Contains(version, ".") || strings.ContainsAny(version, "0123456789") {
			h.handleGetPackageVersion(w, r, name, version)
		} else {
			// This is just a name with slashes
			h.handleGetPackage(w, r, path)
		}
		
	case len(segments) == 1 && segments[0] != "":
		// GET /v1/packages/:name (simple name without slashes)
		h.handleGetPackage(w, r, segments[0])
		
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
