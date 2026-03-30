package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Index implements an in-memory search index
type Index struct {
	packages map[string]PackageInfo
}

// NewIndex creates a new Index instance and loads data from disk
func NewIndex() (*Index, error) {
	index := &Index{
		packages: make(map[string]PackageInfo),
	}
	
	// Try to load existing index
	if err := index.Load(); err != nil {
		// If loading fails, it's not fatal - we can start with an empty index
		fmt.Printf("Warning: failed to load index: %v\n", err)
	}
	
	return index, nil
}

// Load reads the index from data/index.json
func (idx *Index) Load() error {
	path := filepath.Join("data", "index.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// Index file doesn't exist yet, which is fine
			return nil
		}
		return fmt.Errorf("failed to read index from %s: %w", path, err)
	}
	
	var packages []PackageInfo
	if err := json.Unmarshal(data, &packages); err != nil {
		return fmt.Errorf("failed to parse index: %w", err)
	}
	
	// Convert slice to map for faster lookups
	for _, pkg := range packages {
		key := fmt.Sprintf("%s@%s", pkg.Name, pkg.Version)
		idx.packages[key] = pkg
	}
	
	return nil
}

// Save writes the index to data/index.json
func (idx *Index) Save() error {
	// Convert map to slice for JSON serialization
	packages := make([]PackageInfo, 0, len(idx.packages))
	for _, pkg := range idx.packages {
		packages = append(packages, pkg)
	}
	
	// Marshal to JSON
	data, err := json.MarshalIndent(packages, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal index: %w", err)
	}
	
	// Ensure directory exists
	dir := filepath.Dir(filepath.Join("data", "index.json"))
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}
	
	// Write to file
	path := filepath.Join("data", "index.json")
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write index to %s: %w", path, err)
	}
	
	return nil
}

// AddPackage adds a package to the index
func (idx *Index) AddPackage(pkg PackageInfo) error {
	key := fmt.Sprintf("%s@%s", pkg.Name, pkg.Version)
	idx.packages[key] = pkg
	return nil
}

// Search performs a case-insensitive string contains check on name and description
func (idx *Index) Search(query string) []PackageInfo {
	query = strings.ToLower(query)
	results := []PackageInfo{}
	
	for _, pkg := range idx.packages {
		name := strings.ToLower(pkg.Name)
		description := strings.ToLower(pkg.Description)
		
		if strings.Contains(name, query) || strings.Contains(description, query) {
			results = append(results, pkg)
		}
	}
	
	return results
}

// compareSemVer compares two semantic versions
// Returns: -1 if a < b, 0 if a == b, 1 if a > b
func compareSemVer(a, b string) int {
	aParts := strings.Split(a, ".")
	bParts := strings.Split(b, ".")
	
	// Compare each part (major, minor, patch)
	for i := 0; i < 3; i++ {
		// If one version has fewer parts, treat missing parts as 0
		var aVal, bVal int
		if i < len(aParts) {
			aVal, _ = strconv.Atoi(aParts[i])
		}
		if i < len(bParts) {
			bVal, _ = strconv.Atoi(bParts[i])
		}
		
		if aVal < bVal {
			return -1
		} else if aVal > bVal {
			return 1
		}
	}
	
	return 0
}

// GetLatest returns the highest semver version for a package
func (idx *Index) GetLatest(name string) (string, error) {
	versions := []string{}
	
	for key, pkg := range idx.packages {
		if pkg.Name == name {
			// Extract version from key (format: name@version)
			parts := strings.Split(key, "@")
			if len(parts) == 2 {
				versions = append(versions, parts[1])
			}
		}
	}
	
	if len(versions) == 0 {
		return "", fmt.Errorf("no versions found for package %s", name)
	}
	
	// If only one version exists, return it
	if len(versions) == 1 {
		return versions[0], nil
	}
	
	// Find the highest semver version
	highest := versions[0]
	for i := 1; i < len(versions); i++ {
		if compareSemVer(versions[i], highest) > 0 {
			highest = versions[i]
		}
	}
	
	return highest, nil
}
