package registry

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCreateTarball tests that CreateTarball properly excludes files and includes expected ones
func TestCreateTarball(t *testing.T) {
	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "neuron-publish-test")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Change to temp directory
	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current directory: %v", err)
	}
	
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("Failed to change directory: %v", err)
	}
	defer os.Chdir(originalDir) // Restore original directory

	// Create test files and directories
	filesToCreate := []struct {
		path    string
		content string
	}{
		{"neuron.json", `{"name": "test-package", "version": "1.0.0", "description": "Test package", "entry": "main.py", "runtime": "python"}`},
		{"main.py", "print('Hello, World!')"},
		{"README.md", "# Test Package\nThis is a test package."},
		{"requirements.txt", "requests\nnumpy"},
		{".neuronignore", "*.log\ntemp/\nsecret.txt"},
		{"debug.log", "Debug information"},
		{"secret.txt", "Secret information"},
		{"temp/data.txt", "Temporary data"},
		{"test.pyc", "Python bytecode"},
	}

	// Create directories first
	for _, file := range filesToCreate {
		dir := filepath.Dir(file.path)
		if dir != "." && dir != "/" {
			if err := os.MkdirAll(dir, 0755); err != nil {
				t.Fatalf("Failed to create directory %s: %v", dir, err)
			}
		}
	}

	// Create files
	for _, file := range filesToCreate {
		if err := os.WriteFile(file.path, []byte(file.content), 0644); err != nil {
			t.Fatalf("Failed to create file %s: %v", file.path, err)
		}
	}

	// Create directories that should be excluded
	dirsToCreate := []string{".git", "venv", "__pycache__"}
	for _, dir := range dirsToCreate {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("Failed to create directory %s: %v", dir, err)
		}
		// Add a file inside each directory
		if err := os.WriteFile(filepath.Join(dir, "dummy"), []byte("dummy"), 0644); err != nil {
			t.Fatalf("Failed to create file in %s: %v", dir, err)
		}
	}

	// Call CreateTarball
	tarballBytes, err := CreateTarball()
	if err != nil {
		t.Fatalf("CreateTarball failed: %v", err)
	}

	// Verify the tarball contains expected files and excludes unwanted ones
	expectedFiles := []string{
		"neuron.json",
		"main.py",
		"README.md",
		"requirements.txt",
	}

	unexpectedFiles := []string{
		".git/",
		".git/dummy",
		"venv/",
		"venv/dummy",
		"__pycache__/",
		"__pycache__/dummy",
		"debug.log",
		"secret.txt",
		"temp/",
		"temp/data.txt",
		"test.pyc",
	}

	// Decompress and read the tarball
	gzReader, err := gzip.NewReader(bytes.NewReader(tarballBytes))
	if err != nil {
		t.Fatalf("Failed to create gzip reader: %v", err)
	}
	defer gzReader.Close()

	tarReader := tar.NewReader(gzReader)
	
	// Track found files
	foundFiles := make(map[string]bool)
	for _, expected := range expectedFiles {
		foundFiles[expected] = false
	}

	// Read through the tarball entries
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Failed to read tar header: %v", err)
		}

		// Check if this is an expected file
		for _, expected := range expectedFiles {
			if header.Name == expected {
				foundFiles[expected] = true
				break
			}
		}

		// Check if this is an unexpected file
		for _, unexpected := range unexpectedFiles {
			if strings.HasPrefix(header.Name, unexpected) || header.Name == unexpected {
				t.Errorf("Unexpected file found in tarball: %s", header.Name)
				break
			}
		}
	}

	// Verify all expected files were found
	for file, found := range foundFiles {
		if !found {
			t.Errorf("Expected file not found in tarball: %s", file)
		}
	}
}