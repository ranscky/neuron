package installer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLockfile(t *testing.T) {
	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "neuron-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create a lockfile with a custom path
	lockfilePath := filepath.Join(tempDir, "lock.json")
	lf := &Lockfile{
		path: lockfilePath,
		data: make(map[string]string),
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
	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "neuron-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create a custom lockfile for testing
	lockfilePath := filepath.Join(tempDir, "lock.json")
	lockfile := &Lockfile{
		path: lockfilePath,
		data: make(map[string]string),
	}

	// Create a new installer with the custom lockfile
	installer := &Installer{
		lockfile: lockfile,
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
