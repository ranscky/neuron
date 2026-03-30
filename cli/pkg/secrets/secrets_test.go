package secrets

import (
	"os"
	"testing"
	
	"github.com/ranscky/neuron/pkg/manifest"
)

func TestSecretsIntegration(t *testing.T) {
	// Create a store
	store := NewStore()
	
	// Create an injector
	injector := NewInjector(store)
	
	// Set a test secret
	err := store.Set("env:TEST_KEY", "test-value")
	if err != nil {
		t.Fatalf("Failed to set secret: %v", err)
	}
	
	// Create a test manifest with permissions
	testManifest := &manifest.Manifest{
		Permissions: []string{"env:TEST_KEY", "http", "filesystem"},
	}
	
	// Create a runtime environment map
	env := make(map[string]string)
	
	// Inject secrets
	err = injector.Inject(testManifest, env)
	if err != nil {
		t.Fatalf("Failed to inject secrets: %v", err)
	}
	
	// Check that the secret was injected
	if val, ok := env["TEST_KEY"]; !ok {
		t.Error("Expected TEST_KEY to be injected into environment")
	} else if val != "test-value" {
		t.Errorf("Expected TEST_KEY to be 'test-value', got '%s'", val)
	}
}

func TestSecretsFallbackToEnv(t *testing.T) {
	// Create a store
	store := NewStore()
	
	// Create an injector
	injector := NewInjector(store)
	
	// Set an environment variable for testing
	os.Setenv("FALLBACK_TEST_KEY", "fallback-value")
	defer os.Unsetenv("FALLBACK_TEST_KEY")
	
	// Create a test manifest with permissions that don't exist in keyring
	testManifest := &manifest.Manifest{
		Permissions: []string{"env:FALLBACK_TEST_KEY", "http", "filesystem"},
	}
	
	// Create a runtime environment map
	env := make(map[string]string)
	
	// Inject secrets - this should fall back to environment variable
	err := injector.Inject(testManifest, env)
	if err != nil {
		t.Fatalf("Failed to inject secrets: %v", err)
	}
	
	// Check that the secret was injected from environment variable
	if val, ok := env["FALLBACK_TEST_KEY"]; !ok {
		t.Error("Expected FALLBACK_TEST_KEY to be injected into environment from env var")
	} else if val != "fallback-value" {
		t.Errorf("Expected FALLBACK_TEST_KEY to be 'fallback-value', got '%s'", val)
	}
}

func TestSecretsFallbackFailure(t *testing.T) {
	// Create a store
	store := NewStore()
	
	// Create an injector
	injector := NewInjector(store)
	
	// Make sure the environment variable is not set
	os.Unsetenv("MISSING_KEY")
	
	// Create a test manifest with permissions that don't exist in keyring or env
	testManifest := &manifest.Manifest{
		Permissions: []string{"env:MISSING_KEY", "http", "filesystem"},
	}
	
	// Create a runtime environment map
	env := make(map[string]string)
	
	// Inject secrets - this should fail because neither keyring nor env var exist
	err := injector.Inject(testManifest, env)
	if err == nil {
		t.Fatal("Expected error when secret is not found in keyring or env var, but got none")
	}
}
