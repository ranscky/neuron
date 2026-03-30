package installer

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/ranscky/neuron/pkg/registry"
	"github.com/ranscky/neuron/pkg/manifest"
)

// MockRegistry is a mock implementation of the registry.Registry interface
type MockRegistry struct {
	fetchFunc func(name, version string) ([]byte, error)
}

func (m *MockRegistry) Search(query string) ([]registry.Package, error) {
	return nil, nil
}

func (m *MockRegistry) Fetch(name, version string) ([]byte, error) {
	if m.fetchFunc != nil {
		return m.fetchFunc(name, version)
	}
	return nil, fmt.Errorf("fetch not implemented")
}

func (m *MockRegistry) GetPackageInfo(name string) (*registry.PackageInfo, error) {
	return nil, nil
}

func (m *MockRegistry) Publish(manifest *manifest.Manifest, tarball []byte) error {
	return nil
}

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

func createTestTarball(name, version string) ([]byte, error) {
	// Create a buffer to write our tarball to
	buf := new(bytes.Buffer)
	
	// Create gzip writer
	gzipWriter := gzip.NewWriter(buf)
	defer gzipWriter.Close()
	
	// Create tar writer
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
	
	// Add a simple main.py file with actual content (not stub)
	mainPyContent := `#!/usr/bin/env python3
print("This is the real installed package content!")
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
	
	return buf.Bytes(), nil
}

func TestInstaller(t *testing.T) {
	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "neuron-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Temporarily change the home directory for testing
	oldHome := os.Getenv("HOME")
	os.Setenv("HOME", tempDir)
	defer os.Setenv("HOME", oldHome)

	// Create a new installer (this will create a lockfile in our temp directory)
	installer, err := NewInstaller()
	if err != nil {
		t.Fatalf("Failed to create installer: %v", err)
	}

	// Create a mock registry that returns a test tarball
	mockRegistry := &MockRegistry{
		fetchFunc: func(name, version string) ([]byte, error) {
			return createTestTarball(name, version)
		},
	}
	
	// Set the mock registry on the installer
	installer.registry = mockRegistry

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
	
	// Verify that the extracted files exist and have the correct content
	homeDir := tempDir // Use tempDir as home dir for testing
	packageDir := filepath.Join(homeDir, ".neuron", "packages", "example-package", "2.0.0")
	
	// Check that main.py exists
	mainPyPath := filepath.Join(packageDir, "main.py")
	mainPyContent, err := os.ReadFile(mainPyPath)
	if err != nil {
		t.Fatalf("Failed to read main.py: %v", err)
	}
	
	// Check that the content is not the stub content
	stubContent := "#!/usr/bin/env python3\nprint('Hello from the installed package!')"
	if string(mainPyContent) == stubContent {
		t.Error("main.py contains stub content, expected real content")
	}
	
	// Check that the content is the expected real content
	expectedContent := "#!/usr/bin/env python3\nprint(\"This is the real installed package content!\")\n"
	if string(mainPyContent) != expectedContent {
		t.Errorf("main.py contains unexpected content. Got: %s, Expected: %s", string(mainPyContent), expectedContent)
	}
}
