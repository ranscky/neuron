package registry

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/ranscky/neuron/pkg/manifest"
)

// CreateTarball creates a tar.gz archive of the current directory
// excluding specified paths and patterns from .neuronignore
func CreateTarball() ([]byte, error) {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	// Read .neuronignore patterns if file exists
	ignorePatterns, err := readIgnoreFile(".neuronignore")
	if err != nil {
		return nil, fmt.Errorf("error reading .neuronignore: %v", err)
	}

	// Add default ignore patterns
	defaultIgnorePatterns := []string{
		".git/",
		"venv/",
		"__pycache__/",
		"*.pyc",
	}

	// Combine default and custom ignore patterns
	allIgnorePatterns := append(defaultIgnorePatterns, ignorePatterns...)

	err = filepath.Walk(".", func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip the root directory itself
		if path == "." {
			return nil
		}

		// Check if path should be ignored
		if shouldIgnorePath(path, info, allIgnorePatterns) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		header, err := tar.FileInfoHeader(info, path)
		if err != nil {
			return err
		}

		// Use relative paths in the tarball
		header.Name = filepath.ToSlash(path)

		if err := tw.WriteHeader(header); err != nil {
			return err
		}

		if !info.IsDir() {
			file, err := os.Open(path)
			if err != nil {
				return err
			}
			defer file.Close()

			if _, err := io.Copy(tw, file); err != nil {
				return err
			}
		}
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("error walking directory: %v", err)
	}

	if err := tw.Close(); err != nil {
		return nil, fmt.Errorf("error closing tar writer: %v", err)
	}

	if err := gw.Close(); err != nil {
		return nil, fmt.Errorf("error closing gzip writer: %v", err)
	}

	return buf.Bytes(), nil
}

// readIgnoreFile reads ignore patterns from a file
func readIgnoreFile(filename string) ([]string, error) {
	var patterns []string

	// Check if file exists
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		// File doesn't exist, return empty slice
		return patterns, nil
	} else if err != nil {
		// Other error occurred
		return nil, err
	}

	// Read the file
	content, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	// Split by lines and add non-empty patterns
	lines := strings.Split(string(content), "\n")
	for _, line := range lines {
		// Trim whitespace and skip empty lines or comments
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			patterns = append(patterns, line)
		}
	}

	return patterns, nil
}

// shouldIgnorePath checks if a path should be ignored based on patterns
func shouldIgnorePath(path string, info fs.FileInfo, ignorePatterns []string) bool {
	// Convert path to forward slashes for consistency
	path = filepath.ToSlash(path)

	for _, pattern := range ignorePatterns {
		// Handle directory patterns (ending with /)
		if strings.HasSuffix(pattern, "/") {
			// For directory patterns, check if path starts with the pattern
			// or if the path is within that directory
			if strings.HasPrefix(path, pattern) || strings.HasPrefix(path+"/", pattern) {
				return true
			}
		} else if strings.Contains(pattern, "*") {
			// Handle wildcard patterns
			if matched, _ := filepath.Match(pattern, filepath.Base(path)); matched {
				return true
			}
			// Also check the full path
			if matched, _ := filepath.Match(pattern, path); matched {
				return true
			}
		} else {
			// Handle exact matches and prefix matches
			if path == pattern || strings.HasPrefix(path, pattern) {
				return true
			}
		}
	}

	return false
}

// ValidateManifest checks that neuron.json exists and is valid
func ValidateManifest() (*manifest.Manifest, error) {
	// Try to parse the manifest
	// This will also validate required fields
	m, err := manifest.ParseManifest("neuron.json")
	if err != nil {
		return nil, fmt.Errorf("error parsing neuron.json: %v", err)
	}

	return m, nil
}

// PublishPackage publishes a package to the registry
func PublishPackage(registry Registry) error {
	// Validate the manifest first
	manifest, err := ValidateManifest()
	if err != nil {
		return err
	}

	// Create the tarball
	tarball, err := CreateTarball()
	if err != nil {
		return fmt.Errorf("error creating tarball: %v", err)
	}

	// Publish to registry
	if err := registry.Publish(manifest, tarball); err != nil {
		return fmt.Errorf("error publishing to registry: %v", err)
	}

	return nil
}