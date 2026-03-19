package installer

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)
// Installer handles downloading and installing packages
type Installer struct {
	lockfile *Lockfile
}

// NewInstaller creates a new installer
func NewInstaller() (*Installer, error) {
	lockfile, err := NewLockfile()
	if err != nil {
		return nil, fmt.Errorf("failed to initialize lockfile: %w", err)
	}
	
	return &Installer{
		lockfile: lockfile,
	}, nil
}

// Install downloads and installs a package from the registry
func (i *Installer) Install(name, version string) error {
	// Download the package
	packageData, err := i.downloadPackage(name, version)
	if err != nil {
		return fmt.Errorf("failed to download package %s@%s: %w", name, version, err)
	}
	
	// Get the user's home directory
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}
	
	// Create the target directory
	targetDir := filepath.Join(homeDir, ".neuron", "packages", name, version)
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return fmt.Errorf("failed to create target directory: %w", err)
	}
	
	// Extract the package
	if err := i.extractPackage(packageData, targetDir); err != nil {
		return fmt.Errorf("failed to extract package: %w", err)
	}
	
	// Record the installation in the lockfile
	if err := i.lockfile.Add(name, version); err != nil {
		return fmt.Errorf("failed to record installation in lockfile: %w", err)
	}
	
	return nil
}

// downloadPackage downloads a package from the registry
func (i *Installer) downloadPackage(name, version string) (io.Reader, error) {
	// For this implementation, we'll simulate downloading from a registry
	// In a real implementation, this would connect to an actual registry
	
	// Create a temporary file to simulate downloaded package data
	tmpFile, err := os.CreateTemp("", "package-*.tar.gz")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()
	
	// Write some mock data to simulate a package
	mockData := fmt.Sprintf("This is a mock package: %s@%s\n", name, version)
	if _, err := tmpFile.WriteString(mockData); err != nil {
		return nil, fmt.Errorf("failed to write mock data: %w", err)
	}
	
	// Reset file pointer to beginning
	if _, err := tmpFile.Seek(0, 0); err != nil {
		return nil, fmt.Errorf("failed to reset file pointer: %w", err)
	}
	
	// Re-open the file for reading
	return os.Open(tmpFile.Name())
}

// extractPackage extracts a package to the target directory
func (i *Installer) extractPackage(reader io.Reader, targetDir string) error {
	// For this implementation, we'll simulate extraction
	// In a real implementation, this would handle tar.gz extraction
	
	// Create a simple file to represent the extracted package
	manifestPath := filepath.Join(targetDir, "neuron.json")
	manifestContent := fmt.Sprintf(`{
  "name": "%s",
  "version": "1.0.0",
  "description": "A sample package",
  "entry": "main.py",
  "runtime": "python"
}`, filepath.Base(targetDir))
	
	if err := os.WriteFile(manifestPath, []byte(manifestContent), 0644); err != nil {
		return fmt.Errorf("failed to create mock manifest: %w", err)
	}
	
	return nil
}