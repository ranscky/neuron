package runtime

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/ranscky/neuron/internal/config"
	"github.com/ranscky/neuron/pkg/lockfile"
	"github.com/ranscky/neuron/pkg/manifest"
)

// isValidSemver checks if a string is a valid semantic version (X.Y.Z format)
func isValidSemver(version string) bool {
	semverRegex := regexp.MustCompile(`^\d+\.\d+\.\d+$`)
	return semverRegex.MatchString(version)
}

// PythonRuntime handles Python runtime execution
type PythonRuntime struct{}

// NewPythonRuntime creates a new PythonRuntime instance
func NewPythonRuntime() *PythonRuntime {
	return &PythonRuntime{}
}

func (p *PythonRuntime) Name() string {
	return "python"
}

// pythonCandidates lists the interpreters to try, in order, as command prefixes
// (a name plus any fixed arguments). NEURON_PYTHON overrides the search.
//
// Windows prefers `python`: `python3` frequently resolves to the Microsoft Store
// alias, whose venv support is unreliable, and `py -3` is the launcher fallback.
func pythonCandidates(goos, override string) [][]string {
	if override != "" {
		return [][]string{{override}}
	}
	if goos == "windows" {
		return [][]string{{"python"}, {"py", "-3"}, {"python3"}}
	}
	return [][]string{{"python3"}, {"python"}}
}

// createVenv builds a virtual environment, trying each candidate interpreter in
// turn.
//
// --clear is deliberate: a failed earlier attempt leaves a half-built venv
// behind, and plain `python -m venv` would reuse it and fail forever (on Windows
// the symptom is "Unable to copy … venvlauncher.exe"). --without-pip is used
// when nothing needs installing, which also avoids a broken ensurepip failing an
// otherwise fine environment.
func createVenv(venvPath string, needPip bool) error {
	var attempts []string
	for _, candidate := range pythonCandidates(runtime.GOOS, os.Getenv("NEURON_PYTHON")) {
		args := append([]string{}, candidate[1:]...)
		args = append(args, "-m", "venv", "--clear")
		if !needPip {
			args = append(args, "--without-pip")
		}
		args = append(args, venvPath)

		cmd := exec.Command(candidate[0], args...)
		output, err := cmd.CombinedOutput()
		if err != nil {
			attempts = append(attempts, fmt.Sprintf("%s: %v: %s",
				strings.Join(candidate, " "), err, firstLine(string(output))))
			continue
		}
		if _, statErr := os.Stat(venvPython(venvPath, runtime.GOOS)); statErr == nil {
			return nil
		}
		attempts = append(attempts, fmt.Sprintf("%s: reported success but produced no interpreter",
			strings.Join(candidate, " ")))
	}

	return fmt.Errorf("could not create a virtual environment at %s "+
		"(set NEURON_PYTHON to choose an interpreter):\n  %s",
		venvPath, strings.Join(attempts, "\n  "))
}

// venvHasPip reports whether the venv's interpreter can run pip.
func venvHasPip(pythonPath string) bool {
	return exec.Command(pythonPath, "-m", "pip", "--version").Run() == nil
}

// venvRuns reports whether the interpreter inside a venv actually works. A
// half-built environment can still contain a python.exe that cannot start, so
// existence is not enough.
func venvRuns(pythonPath string) bool {
	return exec.Command(pythonPath, "--version").Run() == nil
}

// firstLine keeps an interpreter's complaint readable in an error message.
func firstLine(output string) string {
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return "no output"
}

// SetupPackageVenv creates a virtual environment for a package and installs its requirements.
func (p *PythonRuntime) SetupPackageVenv(name, version string) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	venvPath := venvPathFor(homeDir, name)
	packagePath := filepath.Join(homeDir, ".neuron", "packages", name, version)
	requirementsPath := filepath.Join(packagePath, "requirements.txt")
	needPip := false
	if _, err := os.Stat(requirementsPath); err == nil {
		needPip = true
	}

	pythonPath := venvPython(venvPath, runtime.GOOS)
	_, statErr := os.Stat(pythonPath)
	healthy := statErr == nil && venvRuns(pythonPath) && (!needPip || venvHasPip(pythonPath))
	if !healthy {
		fmt.Printf("Creating virtual environment for %s...\n", name)
		if err := createVenv(venvPath, needPip); err != nil {
			return err
		}
		if _, err := os.Stat(pythonPath); err != nil {
			return fmt.Errorf("virtual environment at %s has no interpreter", venvPath)
		}
	}

	if needPip {
		fmt.Printf("Installing dependencies for %s@%s...\n", name, version)
		// `python -m pip` rather than the pip script: the script's name and
		// location differ between Windows and POSIX, the module call does not.
		pipCmd := exec.Command(pythonPath, "-m", "pip", "install", "-r", requirementsPath, "-q", "--no-cache-dir")
		var stderr bytes.Buffer
		pipCmd.Stderr = &stderr
		if err := pipCmd.Run(); err != nil {
			return fmt.Errorf("failed to install dependencies: %w\nStderr: %s", err, stderr.String())
		}
	}

	return nil
}

// Run executes the Python entry file with the given arguments and environment variables
func (p *PythonRuntime) Run(entry string, args []string, env map[string]string) error {
	output, err := p.RunWithOutput(entry, args, env)
	if err != nil {
		return err
	}
	fmt.Print(output)
	return nil
}

// RunWithOutput executes the Python entry file and returns the stdout output as a string.
func (p *PythonRuntime) RunWithOutput(entry string, args []string, env map[string]string) (string, error) {
	packageDir := filepath.Dir(entry)
	pathParts := strings.Split(packageDir, string(filepath.Separator))
	var packageName, packageVersion string

	for i, part := range pathParts {
		if part == ".neuron" && i+1 < len(pathParts) && pathParts[i+1] == "packages" {
			var nameParts []string
			for j := i + 2; j < len(pathParts); j++ {
				segment := pathParts[j]
				if isValidSemver(segment) {
					packageVersion = segment
					packageName = strings.Join(nameParts, "/")
					break
				} else {
					nameParts = append(nameParts, segment)
				}
			}
			break
		}
	}

	if packageName == "" || packageVersion == "" {
		return "", fmt.Errorf("could not parse package name and version from path: %s", entry)
	}

	if err := p.SetupPackageVenv(packageName, packageVersion); err != nil {
		return "", fmt.Errorf("failed to setup virtual environment: %w", err)
	}

	manifestPath := filepath.Join(packageDir, "neuron.json")
	allowedEnv := make(map[string]bool)
	if _, err := os.Stat(manifestPath); err == nil {
		pkgManifest, err := manifest.ParseManifest(manifestPath)
		if err != nil {
			return "", fmt.Errorf("failed to parse neuron.json: %w", err)
		}

		for _, perm := range pkgManifest.Permissions {
			if strings.HasPrefix(perm, "env:") {
				allowedEnv[strings.TrimPrefix(perm, "env:")] = true
			}
		}

		if pkgManifest.Dependencies != nil && len(pkgManifest.Dependencies) > 0 {
			lockfile, err := lockfile.NewLockfile()
			if err != nil {
				return "", fmt.Errorf("failed to initialize lockfile: %w", err)
			}
			for depName := range pkgManifest.Dependencies {
				depVersion, err := lockfile.Get(depName)
				if err == nil {
					if err := p.SetupPackageVenv(depName, depVersion); err != nil {
						fmt.Printf("Warning: failed to setup virtual environment for dependency %s: %v\n", depName, err)
					}
				}
			}
		}
	}

	pythonPath, err := GetPackageVenvPython(packageName, packageVersion)
	if err != nil {
		return "", fmt.Errorf("failed to get Python venv path: %w", err)
	}

	cmdArgs := append([]string{entry}, args...)
	cmd := exec.Command(pythonPath, cmdArgs...)

	cfg, err := config.LoadConfig()
	if err != nil {
		return "", fmt.Errorf("failed to load Neuron config: %w", err)
	}

	envVars := os.Environ()
	if env != nil {
		for key, value := range env {
			envVars = append(envVars, fmt.Sprintf("%s=%s", key, value))
		}
	}

	envVars = append(envVars, fmt.Sprintf("NEURON_PROVIDER=%s", cfg.Provider))

	// Non-secret routing config is safe to expose. Provider credentials are only
	// handed to packages that explicitly declare the matching env: permission,
	// so one package can never read another's keys.
	setSecret := func(key, value string) {
		if allowedEnv[key] {
			envVars = append(envVars, fmt.Sprintf("%s=%s", key, value))
		}
	}

	switch cfg.Provider {
	case "ollama":
		envVars = append(envVars, fmt.Sprintf("NEURON_OLLAMA_BASE_URL=%s", cfg.Ollama.BaseURL))
	case "openai":
		setSecret("NEURON_OPENAI_API_KEY", cfg.OpenAI.APIKey)
		envVars = append(envVars, fmt.Sprintf("NEURON_MODEL=%s", cfg.OpenAI.Model))
	case "anthropic":
		setSecret("NEURON_ANTHROPIC_API_KEY", cfg.Anthropic.APIKey)
		envVars = append(envVars, fmt.Sprintf("NEURON_MODEL=%s", cfg.Anthropic.Model))
	case "groq":
		setSecret("NEURON_GROQ_API_KEY", cfg.Groq.APIKey)
		envVars = append(envVars, fmt.Sprintf("NEURON_MODEL=%s", cfg.Groq.Model))
	}

	cmd.Env = envVars

	if len(args) > 0 {
		stdin, err := cmd.StdinPipe()
		if err != nil {
			return "", fmt.Errorf("failed to create stdin pipe: %w", err)
		}

		var stdoutBuf bytes.Buffer
		var stderrBuf bytes.Buffer
		cmd.Stdout = &stdoutBuf
		cmd.Stderr = &stderrBuf

		if err := cmd.Start(); err != nil {
			return "", fmt.Errorf("failed to start command: %w", err)
		}

		jsonArgs := strings.Join(args, " ")
		if _, err := stdin.Write([]byte(jsonArgs)); err != nil {
			return "", fmt.Errorf("failed to write to stdin: %w", err)
		}
		if err := stdin.Close(); err != nil {
			return "", fmt.Errorf("failed to close stdin: %w", err)
		}

		err = cmd.Wait()
		if err != nil {
			if stderrBuf.Len() > 0 {
				return "", fmt.Errorf("stderr: %s, err: %w", stderrBuf.String(), err)
			}
			return "", fmt.Errorf("execution failed: %w", err)
		}

		return stdoutBuf.String(), nil
	} else {
		var stdoutBuf bytes.Buffer
		var stderrBuf bytes.Buffer
		cmd.Stdout = &stdoutBuf
		cmd.Stderr = &stderrBuf

		if err := cmd.Run(); err != nil {
			return "", fmt.Errorf("stderr: %s, err: %w", stderrBuf.String(), err)
		}

		return stdoutBuf.String(), nil
	}
}
