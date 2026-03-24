package secrets

import (
	"fmt"
	"strings"

	"github.com/ranscky/neuron/pkg/manifest"
)

// Injector handles injecting secrets into runtime env
type Injector struct {
	store *Store
}

// NewInjector creates a new Injector instance
func NewInjector(store *Store) *Injector {
	return &Injector{
		store: store,
	}
}

// Inject takes a Manifest and a runtime environment map, looks up each permission 
// that starts with "env:" in the keyring, and injects the value into the environment map
func (i *Injector) Inject(manifest *manifest.Manifest, env map[string]string) error {
	for _, permission := range manifest.Permissions {
		if strings.HasPrefix(permission, "env:") {
			// Extract the environment variable name (e.g., "env:OPENAI_KEY" -> "OPENAI_KEY")
			envVarName := strings.TrimPrefix(permission, "env:")
			
			// Get the secret value from the keyring
			value, err := i.store.Get(permission)
			if err != nil {
				return fmt.Errorf("failed to get secret for permission %s: %w", permission, err)
			}
			
			// Inject the value into the environment map
			env[envVarName] = value
		}
	}
	
	return nil
}