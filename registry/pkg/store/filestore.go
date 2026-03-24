package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// FileStore implements the Store interface using the local filesystem
type FileStore struct {
	index *Index
}

// NewFileStore creates a new FileStore instance
func NewFileStore() (*FileStore, error) {
	index, err := NewIndex()
	if err != nil {
		return nil, fmt.Errorf("failed to create index: %w", err)
	}
	
	return &FileStore{
		index: index,
	}, nil
}

// Save stores tarballs and manifests to the filesystem and updates the index
func (fs *FileStore) Save(name, version string, manifest []byte, tarball []byte) error {
	// Create directory structure
	dir := filepath.Join("data", "packages", name)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	// Save tarball
	tarballPath := filepath.Join(dir, version+".tar.gz")
	if err := os.WriteFile(tarballPath, tarball, 0644); err != nil {
		return fmt.Errorf("failed to write tarball to %s: %w", tarballPath, err)
	}

	// Save manifest
	manifestPath := filepath.Join(dir, version+".json")
	if err := os.WriteFile(manifestPath, manifest, 0644); err != nil {
		return fmt.Errorf("failed to write manifest to %s: %w", manifestPath, err)
	}

	// Parse manifest to get package info
	var pkgInfo PackageInfo
	if err := json.Unmarshal(manifest, &pkgInfo); err != nil {
		return fmt.Errorf("failed to parse manifest: %w", err)
	}

	// Update index
	if err := fs.index.AddPackage(pkgInfo); err != nil {
		return fmt.Errorf("failed to add package to index: %w", err)
	}

	// Persist index
	if err := fs.index.Save(); err != nil {
		return fmt.Errorf("failed to save index: %w", err)
	}

	return nil
}

// GetManifest retrieves a manifest from the filesystem
func (fs *FileStore) GetManifest(name, version string) ([]byte, error) {
	path := filepath.Join("data", "packages", name, version+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read manifest from %s: %w", path, err)
	}
	return data, nil
}

// GetTarball retrieves a tarball from the filesystem
func (fs *FileStore) GetTarball(name, version string) ([]byte, error) {
	path := filepath.Join("data", "packages", name, version+".tar.gz")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read tarball from %s: %w", path, err)
	}
	return data, nil
}

// ListVersions lists all versions of a package
func (fs *FileStore) ListVersions(name string) ([]string, error) {
	dir := filepath.Join("data", "packages", name)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, fmt.Errorf("failed to read directory %s: %w", dir, err)
	}

	versions := []string{}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if strings.HasSuffix(entry.Name(), ".json") {
			version := strings.TrimSuffix(entry.Name(), ".json")
			versions = append(versions, version)
		}
	}

	return versions, nil
}

// Search delegates to the index
func (fs *FileStore) Search(query string) ([]PackageInfo, error) {
	return fs.index.Search(query), nil
}

// GetLatest delegates to the index
func (fs *FileStore) GetLatest(name string) (string, error) {
	return fs.index.GetLatest(name)
}