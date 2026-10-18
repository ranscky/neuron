package runtime

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// setTestHome isolates the home directory for a test. Both variables are set:
// os.UserHomeDir reads HOME on Unix but USERPROFILE on Windows, and setting only
// HOME silently leaves Windows tests writing to the real profile.
func setTestHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return home
}

func TestVenvPathsPerOS(t *testing.T) {
	cases := []struct {
		goos   string
		bin    string
		python string
	}{
		{"windows", "Scripts", filepath.Join("Scripts", "python.exe")},
		{"linux", "bin", filepath.Join("bin", "python3")},
		{"darwin", "bin", filepath.Join("bin", "python3")},
	}

	for _, tc := range cases {
		root := filepath.Join("home", ".neuron", "venv", "pkg")
		if got := venvBin(root, tc.goos); got != filepath.Join(root, tc.bin) {
			t.Errorf("%s: bin dir = %q, want %q", tc.goos, got, filepath.Join(root, tc.bin))
		}
		if got := venvPython(root, tc.goos); got != filepath.Join(root, tc.python) {
			t.Errorf("%s: python = %q, want %q", tc.goos, got, filepath.Join(root, tc.python))
		}
	}
}

func TestPythonCandidatesPreferTheWorkingInterpreter(t *testing.T) {
	windows := pythonCandidates("windows", "")
	if len(windows) == 0 || windows[0][0] != "python" {
		t.Errorf("Windows should try `python` first, got %v", windows)
	}

	posix := pythonCandidates("linux", "")
	if len(posix) == 0 || posix[0][0] != "python3" {
		t.Errorf("POSIX should try `python3` first, got %v", posix)
	}

	override := pythonCandidates("windows", "C:/Python315/python.exe")
	if len(override) != 1 || override[0][0] != "C:/Python315/python.exe" {
		t.Errorf("NEURON_PYTHON should be the only candidate, got %v", override)
	}
}

func TestVenvLayoutsCoverScopedAndLegacyNames(t *testing.T) {
	layouts := venvLayouts(t.TempDir(), "tools/web-search", "1.2.3")
	if len(layouts) != 3 {
		t.Fatalf("expected three layouts, got %v", layouts)
	}
	if !strings.HasSuffix(layouts[0], "tools_web-search") {
		t.Errorf("the scoped layout should use the underscored name, got %q", layouts[0])
	}
	if !strings.HasSuffix(layouts[2], filepath.Join("tools", "web-search", "1.2.3")) {
		t.Errorf("the versioned layout is missing, got %q", layouts[2])
	}
}

// interpreterAvailable reports whether any candidate can be executed, so the
// integration tests can skip on a machine without Python.
func interpreterAvailable() bool {
	for _, candidate := range pythonCandidates(runtime.GOOS, os.Getenv("NEURON_PYTHON")) {
		if _, err := exec.LookPath(candidate[0]); err == nil {
			return true
		}
	}
	return false
}

// The regression test for the venv bug: creating an environment must produce an
// interpreter at the path the rest of Neuron looks for, on this platform.
func TestSetupPackageVenvCreatesAWorkingEnvironment(t *testing.T) {
	if !interpreterAvailable() {
		t.Skip("no Python interpreter available")
	}
	home := setTestHome(t)

	// The package directory exists but has no requirements.txt: the venv needs no
	// pip, which is the path that used to fail through ensurepip.
	pkgDir := filepath.Join(home, ".neuron", "packages", "example-package", "1.0.0")
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := NewPythonRuntime().SetupPackageVenv("example-package", "1.0.0"); err != nil {
		t.Fatalf("creating the environment failed: %v", err)
	}

	pythonPath, err := GetPackageVenvPython("example-package", "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(pythonPath); err != nil {
		t.Fatalf("GetPackageVenvPython returned %q, which does not exist: %v", pythonPath, err)
	}

	out, err := exec.Command(pythonPath, "-c", "print(6*7)").Output()
	if err != nil {
		t.Fatalf("the venv interpreter does not run: %v", err)
	}
	if strings.TrimSpace(string(out)) != "42" {
		t.Errorf("venv interpreter printed %q", out)
	}

	// Idempotent: a second call must reuse the environment rather than fail.
	if err := NewPythonRuntime().SetupPackageVenv("example-package", "1.0.0"); err != nil {
		t.Fatalf("a second setup should be a no-op: %v", err)
	}
}

// A half-built environment is rebuilt rather than reused. This is the exact
// shape of the Windows failure ("Unable to copy … venvlauncher.exe"): a
// directory exists but the interpreter inside cannot start.
func TestSetupPackageVenvRebuildsABrokenEnvironment(t *testing.T) {
	if !interpreterAvailable() {
		t.Skip("no Python interpreter available")
	}
	home := setTestHome(t)

	if err := os.MkdirAll(filepath.Join(home, ".neuron", "packages", "broken", "1.0.0"), 0o755); err != nil {
		t.Fatal(err)
	}

	venvPath := venvPathFor(home, "broken")
	if err := os.MkdirAll(venvBin(venvPath, runtime.GOOS), 0o755); err != nil {
		t.Fatal(err)
	}
	// A file that exists but is not a working interpreter.
	if err := os.WriteFile(venvPython(venvPath, runtime.GOOS), []byte("not a real python"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := NewPythonRuntime().SetupPackageVenv("broken", "1.0.0"); err != nil {
		t.Fatalf("a broken environment should be rebuilt, got: %v", err)
	}
	if !venvRuns(venvPython(venvPath, runtime.GOOS)) {
		t.Fatal("the rebuilt environment still has no working interpreter")
	}
}
