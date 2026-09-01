package store

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Index implements a per-org in-memory search index.
//
// Storage format changed: previously serialized as a JSON array of PackageInfo.
// Now serialized as a JSON object keyed by "name@version" for O(1) lookup.
// The old format is auto-migrated on load.
type Index struct {
	packages map[string]PackageInfo
}

// NewIndex creates an empty Index.
func NewIndex() *Index {
	return &Index{
		packages: make(map[string]PackageInfo),
	}
}

// LoadOrMigrate reads the index from disk, handling both legacy array format
// and current map format.
func (idx *Index) LoadOrMigrate(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read index: %w", err)
	}

	// Try map format first (current).
	var asMap map[string]PackageInfo
	if err := json.Unmarshal(data, &asMap); err == nil {
		// Validate it actually looks like a map (has at least one @-key if non-empty).
		if len(asMap) == 0 || hasAtKey(asMap) {
			idx.packages = asMap
			return nil
		}
	}

	// Fall back to legacy array format.
	var asArray []PackageInfo
	if err := json.Unmarshal(data, &asArray); err == nil {
		for _, pkg := range asArray {
			key := fmt.Sprintf("%s@%s", pkg.Name, pkg.Version)
			idx.packages[key] = pkg
		}
		return nil
	}

	return fmt.Errorf("index file at %s is neither map nor array format", path)
}

// Save returns the serialized index bytes. The caller writes them to disk.
// Returning bytes (instead of writing directly) keeps the Index package free
// of filesystem concerns — tests can assert on the JSON shape without temp files.
func (idx *Index) Save() ([]byte, error) {
	data, err := json.MarshalIndent(idx.packages, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal index: %w", err)
	}
	return data, nil
}

// AddPackage adds a package to the index.
func (idx *Index) AddPackage(pkg PackageInfo) error {
	key := fmt.Sprintf("%s@%s", pkg.Name, pkg.Version)
	idx.packages[key] = pkg
	return nil
}

// Search performs a case-insensitive substring match on name and description.
// Returns only the latest version of each matched package.
func (idx *Index) Search(query string) []PackageInfo {
	query = strings.ToLower(query)

	packagesByName := make(map[string][]PackageInfo)
	for _, pkg := range idx.packages {
		name := strings.ToLower(pkg.Name)
		description := strings.ToLower(pkg.Description)
		if query == "" || strings.Contains(name, query) || strings.Contains(description, query) {
			packagesByName[pkg.Name] = append(packagesByName[pkg.Name], pkg)
		}
	}

	results := []PackageInfo{}
	for _, packages := range packagesByName {
		if len(packages) == 0 {
			continue
		}
		if len(packages) == 1 {
			results = append(results, packages[0])
			continue
		}
		latest := packages[0]
		for i := 1; i < len(packages); i++ {
			if compareSemVer(packages[i].Version, latest.Version) > 0 {
				latest = packages[i]
			}
		}
		results = append(results, latest)
	}
	return results
}

// GetLatest returns the highest semver version for a package.
func (idx *Index) GetLatest(name string) (string, error) {
	versions := []string{}
	for key, pkg := range idx.packages {
		if pkg.Name == name {
			parts := strings.Split(key, "@")
			if len(parts) == 2 {
				versions = append(versions, parts[1])
			}
		}
	}
	if len(versions) == 0 {
		return "", fmt.Errorf("no versions found for package %s", name)
	}
	if len(versions) == 1 {
		return versions[0], nil
	}
	highest := versions[0]
	for i := 1; i < len(versions); i++ {
		if compareSemVer(versions[i], highest) > 0 {
			highest = versions[i]
		}
	}
	return highest, nil
}

// compareSemVer compares two semver strings.
// Returns -1 if a < b, 0 if equal, 1 if a > b.
func compareSemVer(a, b string) int {
	aParts := strings.Split(a, ".")
	bParts := strings.Split(b, ".")

	for i := 0; i < 3; i++ {
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

// hasAtKey returns true if any key in the map contains "@".
// Used to distinguish a real map-format index from a JSON-encoded array
// that happened to parse as a map (shouldn't happen, but defensive).
func hasAtKey(m map[string]PackageInfo) bool {
	for k := range m {
		if strings.Contains(k, "@") {
			return true
		}
	}
	return len(m) == 0 // empty is fine
}