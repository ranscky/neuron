package lockfile

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLockfileAddGetListRemove exercises the lockfile without touching the
// user's real home directory.
func TestLockfileAddGetListRemove(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "neuron-lockfile-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	lf := &Lockfile{
		path: filepath.Join(tempDir, "lock.json"),
		data: make(map[string]string),
	}

	if err := lf.Add("test-package", "1.0.0"); err != nil {
		t.Fatalf("Failed to add package: %v", err)
	}

	version, err := lf.Get("test-package")
	if err != nil {
		t.Fatalf("Failed to get package: %v", err)
	}
	if version != "1.0.0" {
		t.Errorf("Expected version 1.0.0, got %s", version)
	}

	if packages := lf.List(); len(packages) != 1 {
		t.Errorf("Expected 1 package, got %d", len(packages))
	}

	if err := lf.Remove("test-package"); err != nil {
		t.Fatalf("Failed to remove package: %v", err)
	}
	if _, err := lf.Get("test-package"); err == nil {
		t.Error("Expected error when getting removed package, got nil")
	}
}
