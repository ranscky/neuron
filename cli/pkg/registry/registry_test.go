package registry

import (
	"os"
	"testing"
)

// TestNewRegistryClientWithEnv tests that the registry client reads from environment variables
func TestNewRegistryClientWithEnv(t *testing.T) {
	// Save original environment variable
	originalURL := os.Getenv("NEURON_REGISTRY_URL")
	defer func() {
		os.Setenv("NEURON_REGISTRY_URL", originalURL)
	}()

	// Test with custom URL
	os.Setenv("NEURON_REGISTRY_URL", "https://custom-registry.example.com")
	client := NewRegistryClient("")
	if client.baseURL != "https://custom-registry.example.com" {
		t.Errorf("Expected baseURL to be https://custom-registry.example.com, got %s", client.baseURL)
	}

	// Test with empty environment variable (should use default)
	os.Unsetenv("NEURON_REGISTRY_URL")
	client = NewRegistryClient("")
	if client.baseURL != "https://neuron-production-ae02.up.railway.app" {
		t.Errorf("Expected baseURL to be https://neuron-production-ae02.up.railway.app, got %s", client.baseURL)
	}

	// Test with explicit baseURL parameter (should override environment)
	client = NewRegistryClient("https://explicit-url.example.com")
	if client.baseURL != "https://explicit-url.example.com" {
		t.Errorf("Expected baseURL to be https://explicit-url.example.com, got %s", client.baseURL)
	}
}