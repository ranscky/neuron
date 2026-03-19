package installer

import (
	"testing"
)

func TestLockfile(t *testing.T) {
	// Create a new lockfile
	lf, err := NewLockfile()
	if err != nil {
		t.Fatalf("Failed to create lockfile: %v", err)
	}

	// Add a package
	err = lf.Add("test-package", "1.0.0")
	if err != nil {
		t.Fatalf("Failed to add package: %v", err)
	}

	// Get the package version
	version, err := lf.Get("test-package")
	if err != nil {
		t.Fatalf("Failed to get package: %v", err)
	}

	if version != "1.0.0" {
		t.Errorf("Expected version 1.0.0, got %s", version)
	}

	// List packages
	packages := lf.List()
	if len(packages) != 1 {
		t.Errorf("Expected 1 package, got %d", len(packages))
	}

	// Remove the package
	err = lf.Remove("test-package")
	if err != nil {
		t.Fatalf("Failed to remove package: %v", err)
	}

	// Verify removal
	_, err = lf.Get("test-package")
	if err == nil {
		t.Error("Expected error when getting removed package, got nil")
	}
}

func TestInstaller(t *testing.T) {
	// Create a new installer
	installer, err := NewInstaller()
	if err != nil {
		t.Fatalf("Failed to create installer: %v", err)
	}

	// Install a package (this will use the mock implementation)
	err = installer.Install("example-package", "2.0.0")
	if err != nil {
		t.Fatalf("Failed to install package: %v", err)
	}

	// Verify the package was recorded in the lockfile
	version, err := installer.lockfile.Get("example-package")
	if err != nil {
		t.Fatalf("Failed to get package from lockfile: %v", err)
	}

	if version != "2.0.0" {
		t.Errorf("Expected version 2.0.0 in lockfile, got %s", version)
	}
}