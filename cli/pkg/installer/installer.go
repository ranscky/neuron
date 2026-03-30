package installer

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
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
	// In a real implementation, this would connect to an actual registry
	// For now, we'll create a mock tar.gz file for testing purposes
	
	// Create a temporary file to simulate downloaded package data
	tmpFile, err := os.CreateTemp("", "package-*.tar.gz")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp file: %w", err)
	}
	defer tmpFile.Close()
	
	// Create a gzip writer
	gzipWriter := gzip.NewWriter(tmpFile)
	defer gzipWriter.Close()
	
	// Create a tar writer
	tarWriter := tar.NewWriter(gzipWriter)
	defer tarWriter.Close()
	
	// Create a simple neuron.json file
	manifestContent := fmt.Sprintf(`{
  "name": "%s",
  "version": "%s",
  "description": "A sample package",
  "entry": "main.py",
  "runtime": "python"
}`, name, version)
	
	// Add neuron.json to the tar
	hdr := &tar.Header{
		Name: "neuron.json",
		Mode: 0644,
		Size: int64(len(manifestContent)),
	}
	
	if err := tarWriter.WriteHeader(hdr); err != nil {
		return nil, fmt.Errorf("failed to write tar header: %w", err)
	}
	
	if _, err := tarWriter.Write([]byte(manifestContent)); err != nil {
		return nil, fmt.Errorf("failed to write manifest to tar: %w", err)
	}
	
	// Add a simple main.py file
	mainPyContent := `#!/usr/bin/env python3
print("Hello from the installed package!")
`
	
	hdr = &tar.Header{
		Name: "main.py",
		Mode: 0755,
		Size: int64(len(mainPyContent)),
	}
	
	if err := tarWriter.WriteHeader(hdr); err != nil {
		return nil, fmt.Errorf("failed to write tar header for main.py: %w", err)
	}
	
	if _, err := tarWriter.Write([]byte(mainPyContent)); err != nil {
		return nil, fmt.Errorf("failed to write main.py to tar: %w", err)
	}
	
	// Close writers to flush data
	tarWriter.Close()
	gzipWriter.Close()
	
	// Reset file pointer to beginning
	if _, err := tmpFile.Seek(0, 0); err != nil {
		return nil, fmt.Errorf("failed to reset file pointer: %w", err)
	}
	
	// Re-open the file for reading
	return os.Open(tmpFile.Name())
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
	
	// Verify main.py exists
	mainPyPath := filepath.Join(targetDir, "main.py")
	if _, err := os.Stat(mainPyPath); os.IsNotExist(err) {
		return fmt.Errorf("main.py not found in extracted package")
	}
	
	return nil
}
