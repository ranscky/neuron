package installer

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Lockfile handles neuron.lock generation and reading
type Lockfile struct {
	path string
	data map[string]string
}

// NewLockfile creates a new lockfile handler
func NewLockfile() (*Lockfile, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}
	
	lockfilePath := filepath.Join(homeDir, ".neuron", "lock.json")
	
	// Create the .neuron directory if it doesn't exist
	neuronDir := filepath.Dir(lockfilePath)
	if err := os.MkdirAll(neuronDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create neuron directory: %w", err)
	}
	
	lf := &Lockfile{
		path: lockfilePath,
		data: make(map[string]string),
	}
	
	// Try to load existing lockfile
	if err := lf.load(); err != nil {
		// If the file doesn't exist, that's fine, we'll create it later
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("failed to load lockfile: %w", err)
		}
	}
	
	return lf, nil
}

// load reads the lockfile from disk
func (lf *Lockfile) load() error {
	data, err := os.ReadFile(lf.path)
	if err != nil {
		return err
	}
	
	if err := json.Unmarshal(data, &lf.data); err != nil {
		return fmt.Errorf("failed to parse lockfile: %w", err)
	}
	
	return nil
}

// save writes the lockfile to disk
func (lf *Lockfile) save() error {
	data, err := json.MarshalIndent(lf.data, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal lockfile data: %w", err)
	}
	
	if err := os.WriteFile(lf.path, data, 0644); err != nil {
		return fmt.Errorf("failed to write lockfile: %w", err)
	}
	
	return nil
}

// Add adds a package to the lockfile
func (lf *Lockfile) Add(name, version string) error {
	lf.data[name] = version
	return lf.save()
}

// Remove removes a package from the lockfile
func (lf *Lockfile) Remove(name string) error {
	delete(lf.data, name)
	return lf.save()
}

// Get retrieves the version of an installed package
func (lf *Lockfile) Get(name string) (string, error) {
	version, exists := lf.data[name]
	if !exists {
		return "", fmt.Errorf("package %s not found in lockfile", name)
	}
	return version, nil
}

// List returns all installed packages
func (lf *Lockfile) List() map[string]string {
	// Return a copy to prevent external modification
	result := make(map[string]string)
	for k, v := range lf.data {
		result[k] = v
	}
	return result
}