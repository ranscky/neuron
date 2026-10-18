package runtime

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// A virtual environment lays its executables out differently per platform:
// Scripts\python.exe on Windows, bin/python3 everywhere else. Everything that
// looks for the interpreter goes through these helpers, so the two layouts never
// have to be spelled out again.

// venvBin is the directory a virtual environment keeps its executables in.
func venvBin(venvPath, goos string) string {
	if goos == "windows" {
		return filepath.Join(venvPath, "Scripts")
	}
	return filepath.Join(venvPath, "bin")
}

// venvPython is the interpreter path inside a virtual environment.
func venvPython(venvPath, goos string) string {
	if goos == "windows" {
		return filepath.Join(venvPath, "Scripts", "python.exe")
	}
	return filepath.Join(venvPath, "bin", "python3")
}

// venvPythonNames lists interpreter file names to look for. More than one so a
// venv created by a different layout (or an older Neuron) is still found.
func venvPythonNames(goos string) []string {
	if goos == "windows" {
		return []string{"python.exe", "python3.exe", "python"}
	}
	return []string{"python3", "python"}
}

// venvLayouts lists where a package's venv may live, newest layout first: the
// underscored name for scoped packages, then the flat and versioned older ones.
func venvLayouts(homeDir, name, version string) []string {
	scoped := strings.ReplaceAll(name, "/", "_")
	layouts := []string{
		filepath.Join(homeDir, ".neuron", "venv", scoped),
		filepath.Join(homeDir, ".neuron", "venv", name),
	}
	if version != "" {
		layouts = append(layouts, filepath.Join(homeDir, ".neuron", "venv", name, version))
	}
	return layouts
}

// findVenvPython locates the interpreter inside any of a package's venvs.
func findVenvPython(homeDir, name, version, goos string) (string, bool) {
	for _, layout := range venvLayouts(homeDir, name, version) {
		for _, candidate := range venvPythonNames(goos) {
			path := filepath.Join(venvBin(layout, goos), candidate)
			if _, err := os.Stat(path); err == nil {
				return path, true
			}
		}
	}
	return "", false
}

// venvPathFor is the venv directory a package installs into.
func venvPathFor(homeDir, name string) string {
	return filepath.Join(homeDir, ".neuron", "venv", strings.ReplaceAll(name, "/", "_"))
}

// GetPackageVenvPython returns the path to the Python interpreter for a package's virtual environment.
// It checks the known locations and falls back to the system interpreter.
func GetPackageVenvPython(name, version string) (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}

	if python, ok := findVenvPython(homeDir, name, version, runtime.GOOS); ok {
		return python, nil
	}

	// No environment yet: the caller is about to create one, or wants the system
	// interpreter. Naming it consistently matters on Windows, where `python3`
	// often resolves to the Microsoft Store alias.
	return systemPython(), nil
}

// systemPython is the interpreter to use when no venv exists.
func systemPython() string {
	if runtime.GOOS == "windows" {
		return "python"
	}
	return "python3"
}
