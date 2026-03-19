package secrets

import (
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