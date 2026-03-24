package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/ranscky/neuron-registry/pkg/store"
)

// SearchHandler handles GET /v1/search
type SearchHandler struct {
	store store.Store
}

// NewSearchHandler creates a new SearchHandler
func NewSearchHandler(s store.Store) *SearchHandler {
	return &SearchHandler{store: s}
}

// SearchResponse represents the response structure for search
type SearchResponse struct {
	Results []store.PackageInfo `json:"results"`
}

// ServeHTTP handles the GET /v1/search request
func (h *SearchHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error": "method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	// Read q query param
	query := r.URL.Query().Get("q")
	if query == "" {
		http.Error(w, `{"error": "query parameter 'q' is required"}`, http.StatusBadRequest)
		return
	}

	// Call store.Search(q)
	results, err := h.store.Search(query)
	if err != nil {
		http.Error(w, `{"error": "search failed"}`, http.StatusInternalServerError)
		return
	}

	// Return JSON with "results" array of PackageInfo
	response := SearchResponse{
		Results: results,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}