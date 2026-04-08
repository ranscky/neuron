package runtime

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// GetPackageVenvPython returns the path to the Python interpreter for a package's virtual environment.
// It checks multiple possible locations and falls back to the system default.
func GetPackageVenvPython(name, version string) (string, error) {
	// Get user's home directory
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}

	// For scoped packages, replace slashes with underscores to match the venv directory structure
	venvName := strings.ReplaceAll(name, "/", "_")

	// Check ~/.neuron/venv/<venvName>/bin/python3 first (new structure for scoped packages)
	venvPath1 := filepath.Join(homeDir, ".neuron", "venv", venvName, "bin", "python3")
	if _, err := os.Stat(venvPath1); err == nil {
		return venvPath1, nil
	}

	// Check ~/.neuron/venv/<name>/bin/python3 (old structure for backward compatibility)
	venvPath2 := filepath.Join(homeDir, ".neuron", "venv", name, "bin", "python3")
	if _, err := os.Stat(venvPath2); err == nil {
		return venvPath2, nil
	}

	// Fall back to ~/.neuron/venv/<name>/<version>/bin/python3
	venvPath3 := filepath.Join(homeDir, ".neuron", "venv", name, version, "bin", "python3")
	if _, err := os.Stat(venvPath3); err == nil {
		return venvPath3, nil
	}

	// Fall back to "python3" system default
	return "python3", nil
}
