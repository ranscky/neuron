package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ranscky/neuron-registry/pkg/store"
)

func TestSearchHandler(t *testing.T) {
	// Create a test store
	testStore, err := store.NewFileStore()
	if err != nil {
		t.Fatalf("Failed to create test store: %v", err)
	}

	// Create a search handler
	searchHandler := NewSearchHandler(testStore)

	// Test case 1: GET /v1/search with empty query
	t.Run("SearchWithEmptyQuery", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/v1/search", nil)
		rr := httptest.NewRecorder()
		searchHandler.ServeHTTP(rr, req)

		// We expect a 200 OK
		if rr.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", rr.Code)
		}

		// Parse the response
		var response SearchResponse
		if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
			t.Errorf("Failed to parse response: %v", err)
		}

		// With an empty index, we expect an empty results array
		if len(response.Results) != 0 {
			t.Errorf("Expected empty results array, got %d items", len(response.Results))
		}
	})

	// Test case 2: GET /v1/search with specific query
	t.Run("SearchWithSpecificQuery", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/v1/search?q=test", nil)
		rr := httptest.NewRecorder()
		searchHandler.ServeHTTP(rr, req)

		// We expect a 200 OK
		if rr.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", rr.Code)
		}

		// Parse the response
		var response SearchResponse
		if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
			t.Errorf("Failed to parse response: %v", err)
		}

		// With an empty index, we expect an empty results array
		if len(response.Results) != 0 {
			t.Errorf("Expected empty results array, got %d items", len(response.Results))
		}
	})
}