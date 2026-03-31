package store

import (
	"testing"
	"time"
)

func TestSearchReturnsLatestVersionOnly(t *testing.T) {
	// Create a new index
	index := &Index{
		packages: make(map[string]PackageInfo),
	}

	// Add multiple versions of the same package
	pkgV1 := PackageInfo{
		Name:        "test-package",
		Version:     "1.0.0",
		Description: "Test package v1",
		Runtime:     "python",
		Permissions: []string{"http"},
		PublishedAt: time.Now(),
	}

	pkgV2 := PackageInfo{
		Name:        "test-package",
		Version:     "2.0.0",
		Description: "Test package v2",
		Runtime:     "python",
		Permissions: []string{"http", "fs"},
		PublishedAt: time.Now(),
	}

	pkgV3 := PackageInfo{
		Name:        "test-package",
		Version:     "1.5.0",
		Description: "Test package v1.5",
		Runtime:     "python",
		Permissions: []string{"http"},
		PublishedAt: time.Now(),
	}

	// Add a different package
	otherPkg := PackageInfo{
		Name:        "other-package",
		Version:     "1.0.0",
		Description: "Other package",
		Runtime:     "node",
		Permissions: []string{"http"},
		PublishedAt: time.Now(),
	}

	// Add packages to index
	key1 := pkgV1.Name + "@" + pkgV1.Version
	key2 := pkgV2.Name + "@" + pkgV2.Version
	key3 := pkgV3.Name + "@" + pkgV3.Version
	key4 := otherPkg.Name + "@" + otherPkg.Version

	index.packages[key1] = pkgV1
	index.packages[key2] = pkgV2
	index.packages[key3] = pkgV3
	index.packages[key4] = otherPkg

	// Test search with empty query (should return all packages, but only latest version of each)
	results := index.Search("")
	
	// Should return 2 packages (test-package and other-package)
	if len(results) != 2 {
		t.Errorf("Expected 2 packages, got %d", len(results))
	}

	// Find test-package in results
	var testPkgResult *PackageInfo
	var otherPkgResult *PackageInfo
	
	for _, pkg := range results {
		if pkg.Name == "test-package" {
			testPkgResult = &pkg
		} else if pkg.Name == "other-package" {
			otherPkgResult = &pkg
		}
	}

	// Should have found test-package
	if testPkgResult == nil {
		t.Error("Expected to find test-package in results")
	}

	// Should have found other-package
	if otherPkgResult == nil {
		t.Error("Expected to find other-package in results")
	}

	// For test-package, should have version 2.0.0 (latest)
	if testPkgResult != nil && testPkgResult.Version != "2.0.0" {
		t.Errorf("Expected test-package version 2.0.0, got %s", testPkgResult.Version)
	}

	// For other-package, should have version 1.0.0
	if otherPkgResult != nil && otherPkgResult.Version != "1.0.0" {
		t.Errorf("Expected other-package version 1.0.0, got %s", otherPkgResult.Version)
	}

	// Test search with specific query
	results = index.Search("test")
	
	// Should return 1 package (test-package only)
	if len(results) != 1 {
		t.Errorf("Expected 1 package for query 'test', got %d", len(results))
	}

	// The result should be test-package version 2.0.0
	if len(results) > 0 && results[0].Name == "test-package" && results[0].Version != "2.0.0" {
		t.Errorf("Expected test-package version 2.0.0 for query 'test', got %s", results[0].Version)
	}
}

func TestCompareSemVer(t *testing.T) {
	// Test basic version comparisons
	tests := []struct {
		a, b     string
		expected int
	}{
		{"1.0.0", "1.0.0", 0},  // equal
		{"1.0.1", "1.0.0", 1},  // a > b
		{"1.0.0", "1.0.1", -1}, // a < b
		{"2.0.0", "1.0.0", 1},  // major version difference
		{"1.2.0", "1.1.0", 1},  // minor version difference
		{"1.0.2", "1.0.1", 1},  // patch version difference
		{"1.0", "1.0.0", 0},    // different format, same version
		{"2", "1.0.0", 1},      // different format
	}

	for _, test := range tests {
		result := compareSemVer(test.a, test.b)
		if result != test.expected {
			t.Errorf("compareSemVer(%s, %s): expected %d, got %d", test.a, test.b, test.expected, result)
		}
	}
}