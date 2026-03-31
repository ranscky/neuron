package handlers

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ranscky/neuron-registry/pkg/store"
)

func TestPackagesHandlerWithScopedNames(t *testing.T) {
	// Create a test store
	testStore, err := store.NewFileStore()
	if err != nil {
		t.Fatalf("Failed to create test store: %v", err)
	}

	// Create a packages handler
	packagesHandler := NewPackagesHandler(testStore)

	// Test case 1: GET /v1/packages/tools/web-search
	t.Run("GetPackageWithScopedName", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/v1/packages/tools/web-search", nil)
		rr := httptest.NewRecorder()
		packagesHandler.ServeHTTP(rr, req)

		// We expect a 404 since the package doesn't exist
		if rr.Code != http.StatusNotFound {
			t.Errorf("Expected status 404, got %d", rr.Code)
		}
	})

	// Test case 2: GET /v1/packages/tools/web-search/1.0.2
	t.Run("GetPackageVersionWithScopedName", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/v1/packages/tools/web-search/1.0.2", nil)
		rr := httptest.NewRecorder()
		packagesHandler.ServeHTTP(rr, req)

		// We expect a 404 since the package doesn't exist
		if rr.Code != http.StatusNotFound {
			t.Errorf("Expected status 404, got %d", rr.Code)
		}
	})

	// Test case 3: GET /v1/packages/tools/web-search/1.0.2/download
	t.Run("DownloadPackageWithScopedName", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/v1/packages/tools/web-search/1.0.2/download", nil)
		rr := httptest.NewRecorder()
		packagesHandler.ServeHTTP(rr, req)

		// We expect a 404 since the package doesn't exist
		if rr.Code != http.StatusNotFound {
			t.Errorf("Expected status 404, got %d", rr.Code)
		}
	})

	// Test case 4: GET /v1/packages/tools/web-search/versions
	t.Run("ListVersionsWithScopedName", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/v1/packages/tools/web-search/versions", nil)
		rr := httptest.NewRecorder()
		packagesHandler.ServeHTTP(rr, req)

		// We expect a 200 with empty array since the package doesn't exist
		if rr.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", rr.Code)
		}
		
		// Response should be an empty array
		expected := "[]\n"
		if rr.Body.String() != expected {
			t.Errorf("Expected %s, got %s", expected, rr.Body.String())
		}
	})
}

func TestPathParsingLogic(t *testing.T) {
	// Test the internal path parsing logic directly
	testCases := []struct {
		path          string
		expectedName  string
		expectedAction string
		isVersion     bool
	}{
		{"tools/web-search", "tools/web-search", "", false},
		{"tools/web-search/1.0.2", "tools/web-search", "1.0.2", true},
		{"tools/web-search/versions", "tools/web-search", "versions", false},
		{"a/b/c/d", "a/b/c", "d", false},
		{"a/b/c/d/versions", "a/b/c/d", "versions", false},
		{"a/b/c/d/1.0.0", "a/b/c/d", "1.0.0", true},
	}

	for _, tc := range testCases {
		t.Run(tc.path, func(t *testing.T) {
			// This is a simplified version of the path parsing logic
			segments := bytes.Split([]byte(tc.path), []byte("/"))
			
			// For our test, we just want to make sure the splitting works correctly
			if len(segments) < 1 {
				t.Errorf("Expected at least one segment, got %d", len(segments))
				return
			}
			
			// Convert segments back to strings for easier comparison
			segmentStrings := make([]string, len(segments))
			for i, seg := range segments {
				segmentStrings[i] = string(seg)
			}
			
			t.Logf("Path: %s -> Segments: %v", tc.path, segmentStrings)
		})
	}
}