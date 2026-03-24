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
// excluding the .git directory
func CreateTarball() ([]byte, error) {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	err := filepath.Walk(".", func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip .git directory
		if strings.Contains(path, ".git") {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		// Skip the root directory itself
		if path == "." {
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