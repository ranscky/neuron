package installer

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ranscky/neuron/pkg/manifest"
	"github.com/ranscky/neuron/pkg/registry"
)
// Installer handles downloading and installing packages
type Installer struct {
	lockfile *Lockfile
	registry registry.Registry
}

// NewInstaller creates a new installer
func NewInstaller() (*Installer, error) {
	lockfile, err := NewLockfile()
	if err != nil {
		return nil, fmt.Errorf("failed to initialize lockfile: %w", err)
	}
	
	// Initialize registry client
	reg := registry.NewRegistryClient("")
	
	return &Installer{
		lockfile: lockfile,
		registry: reg,
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
	// Fetch the package from the registry
	packageData, err := i.registry.Fetch(name, version)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch package from registry: %w", err)
	}
	
	// Create a reader from the package data
	reader := bytes.NewReader(packageData)
	return reader, nil
}

// extractPackage extracts a package to the target directory
func (i *Installer) extractPackage(reader io.Reader, targetDir string) error {
	// Create gzip reader
	gzipReader, err := gzip.NewReader(reader)
	if err != nil {
		return fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer gzipReader.Close()
	
	// Create tar reader
	tarReader := tar.NewReader(gzipReader)
	
	// Extract files
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read tar header: %w", err)
		}
		
		// Skip empty headers
		if header == nil {
			continue
		}
		
		// Construct the file path
		filePath := filepath.Join(targetDir, header.Name)
		
		// Check for Zip Slip vulnerability
		if !filepath.IsAbs(filePath) && !strings.HasPrefix(filePath, targetDir) {
			return fmt.Errorf("illegal file path: %s", filePath)
		}
		
		// Create directory structure if needed
		if header.Typeflag == tar.TypeDir {
			if err := os.MkdirAll(filePath, os.FileMode(header.Mode)); err != nil {
				return fmt.Errorf("failed to create directory %s: %w", filePath, err)
			}
		} else {
			// Create file
			file, err := os.Create(filePath)
			if err != nil {
				return fmt.Errorf("failed to create file %s: %w", filePath, err)
			}
			
			// Copy file contents
			if _, err := io.Copy(file, tarReader); err != nil {
				file.Close()
				return fmt.Errorf("failed to write file %s: %w", filePath, err)
			}
			
			// Set file permissions
			if err := file.Chmod(os.FileMode(header.Mode)); err != nil {
				file.Close()
				return fmt.Errorf("failed to set permissions for %s: %w", filePath, err)
			}
			
			file.Close()
		}
	}
	
	// Verify neuron.json exists
	neuronJsonPath := filepath.Join(targetDir, "neuron.json")
	if _, err := os.Stat(neuronJsonPath); os.IsNotExist(err) {
		return fmt.Errorf("neuron.json not found in extracted package")
	}

	// Parse the manifest to check runtime
	manifest, err := manifest.ParseManifest(neuronJsonPath)
	if err != nil {
		return fmt.Errorf("failed to parse neuron.json: %w", err)
	}

	// Only check for main.py if runtime is python
	if manifest.Runtime == "python" {
		mainPyPath := filepath.Join(targetDir, "main.py")
		if _, err := os.Stat(mainPyPath); os.IsNotExist(err) {
			return fmt.Errorf("main.py not found in extracted package (required for python runtime)")
		}
	}
	
	return nil
}
