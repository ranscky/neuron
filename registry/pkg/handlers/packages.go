package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/ranscky/neuron-registry/pkg/auth"
	"github.com/ranscky/neuron-registry/pkg/store"
)

// PackagesHandler handles /v1/packages endpoints
type PackagesHandler struct {
	store   store.Store
	authMgr *auth.Manager
}

// NewPackagesHandler creates a new PackagesHandler
func NewPackagesHandler(s store.Store, m *auth.Manager) *PackagesHandler {
	return &PackagesHandler{store: s, authMgr: m}
}

// ServeHTTP handles the /v1/packages requests
func (h *PackagesHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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

	// Strip /v1/packages/ prefix from the path
	path := strings.TrimPrefix(r.URL.Path, "/v1/packages/")
	if path == "" || path == "/" {
		http.Error(w, `{"error": "not found"}`, http.StatusNotFound)
		return
	}
	path = strings.TrimPrefix(path, "/")

	w.Header().Set("Content-Type", "application/json")

	segments := strings.Split(path, "/")
	switch {
	case len(segments) >= 2 && segments[len(segments)-1] == "download":
		if len(segments) < 3 {
			http.Error(w, `{"error": "invalid path"}`, http.StatusBadRequest)
			return
		}
		version := segments[len(segments)-2]
		name := strings.Join(segments[:len(segments)-2], "/")
		h.handleDownloadPackage(w, r, orgID, name, version)
	case len(segments) >= 2 && segments[len(segments)-1] == "versions":
		name := strings.Join(segments[:len(segments)-1], "/")
		h.handleListVersions(w, r, orgID, name)
	case len(segments) >= 2:
		version := segments[len(segments)-1]
		name := strings.Join(segments[:len(segments)-1], "/")
		if strings.Contains(version, ".") || strings.ContainsAny(version, "0123456789") {
			h.handleGetPackageVersion(w, r, orgID, name, version)
		} else {
			h.handleGetPackage(w, r, orgID, path)
		}
	case len(segments) == 1 && segments[0] != "":
		h.handleGetPackage(w, r, orgID, segments[0])
	default:
		http.Error(w, `{"error": "not found"}`, http.StatusNotFound)
	}
}

func (h *PackagesHandler) handleGetPackage(w http.ResponseWriter, r *http.Request, orgID, name string) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error": "method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	version, err := h.store.GetLatest(orgID, name)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error": "package not found: %s"}`, err.Error()), http.StatusNotFound)
		return
	}
	manifestBytes, err := h.store.GetManifest(orgID, name, version)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error": "failed to get manifest: %s"}`, err.Error()), http.StatusInternalServerError)
		return
	}
	var packageInfo store.PackageInfo
	if err := json.Unmarshal(manifestBytes, &packageInfo); err != nil {
		http.Error(w, fmt.Sprintf(`{"error": "failed to parse manifest: %s"}`, err.Error()), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(packageInfo)
}

func (h *PackagesHandler) handleGetPackageVersion(w http.ResponseWriter, r *http.Request, orgID, name, version string) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error": "method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	manifestBytes, err := h.store.GetManifest(orgID, name, version)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error": "package not found: %s"}`, err.Error()), http.StatusNotFound)
		return
	}
	var packageInfo store.PackageInfo
	if err := json.Unmarshal(manifestBytes, &packageInfo); err != nil {
		http.Error(w, fmt.Sprintf(`{"error": "failed to parse manifest: %s"}`, err.Error()), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(packageInfo)
}

func (h *PackagesHandler) handleDownloadPackage(w http.ResponseWriter, r *http.Request, orgID, name, version string) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error": "method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	tarballBytes, err := h.store.GetTarball(orgID, name, version)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error": "package not found: %s"}`, err.Error()), http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/gzip")
	w.Write(tarballBytes)
}

func (h *PackagesHandler) handleListVersions(w http.ResponseWriter, r *http.Request, orgID, name string) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error": "method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	versions, err := h.store.ListVersions(orgID, name)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error": "failed to list versions: %s"}`, err.Error()), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(versions)
}
