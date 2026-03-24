package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLogger(t *testing.T) {
	// Create a simple handler
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	// Wrap with logger middleware
 loggedHandler := Logger(handler)

	// Create a test request
	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()

	// Execute the request
	loggedHandler.ServeHTTP(rec, req)

	// Check the response
	if rec.Code != http.StatusOK {
		t.Errorf("Expected status code %d, got %d", http.StatusOK, rec.Code)
	}
}

func TestCORS(t *testing.T) {
	// Create a simple handler
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	// Wrap with CORS middleware
	corsHandler := CORS(handler)

	// Test normal request
	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	corsHandler.ServeHTTP(rec, req)

	// Check CORS headers
	if origin := rec.Header().Get("Access-Control-Allow-Origin"); origin != "*" {
		t.Errorf("Expected Access-Control-Allow-Origin *, got %s", origin)
	}

	// Test OPTIONS request
	req = httptest.NewRequest("OPTIONS", "/", nil)
	rec = httptest.NewRecorder()
	corsHandler.ServeHTTP(rec, req)

	// Check that OPTIONS returns 200
	if rec.Code != http.StatusOK {
		t.Errorf("Expected status code %d for OPTIONS, got %d", http.StatusOK, rec.Code)
	}
}

func TestRecovery(t *testing.T) {
	// Create a handler that panics
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("test panic")
	})

	// Wrap with recovery middleware
	recoveryHandler := Recovery(handler)

	// Create a test request
	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()

	// Execute the request - should not panic
	recoveryHandler.ServeHTTP(rec, req)

	// Check the response
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("Expected status code %d, got %d", http.StatusInternalServerError, rec.Code)
	}
}