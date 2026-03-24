package manifest

import (
	"encoding/json"
	"fmt"
	"os"
)

// ParseManifest reads a neuron.json file from the given path and returns a populated Manifest struct
func ParseManifest(path string) (*Manifest, error) {
	// Read the file
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read manifest file: %w", err)
	}

	// Parse the JSON into a Manifest struct
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("failed to parse manifest JSON: %w", err)
	}

	// Validate required fields
	if err := validateRequiredFields(&manifest); err != nil {
		return nil, err
	}

	return &manifest, nil
}

// validateRequiredFields checks that all required fields are present
func validateRequiredFields(manifest *Manifest) error {
	if manifest.Name == "" {
		return fmt.Errorf("missing required field: name")
	}
	if manifest.Version == "" {
		return fmt.Errorf("missing required field: version")
	}
	if manifest.Description == "" {
		return fmt.Errorf("missing required field: description")
	}
	if manifest.Entry == "" {
		return fmt.Errorf("missing required field: entry")
	}
	if manifest.Runtime == "" {
		return fmt.Errorf("missing required field: runtime")
	}
	return nil
}