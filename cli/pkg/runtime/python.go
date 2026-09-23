package runtime

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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

// SetupPackageVenv creates a virtual environment for a package and installs its requirements.
func (p *PythonRuntime) SetupPackageVenv(name, version string) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	venvName := strings.ReplaceAll(name, "/", "_")
	venvPath := filepath.Join(homeDir, ".neuron", "venv", venvName)
	if err := os.MkdirAll(venvPath, 0755); err != nil {
		return fmt.Errorf("failed to create venv directory: %w", err)
	}

	venvPythonPath := filepath.Join(venvPath, "bin", "python3")
	if _, err := os.Stat(venvPythonPath); os.IsNotExist(err) {
		fmt.Printf("Creating virtual environment for %s...\n", name)
		venvCmd := exec.Command("python3", "-m", "venv", venvPath)
		if output, err := venvCmd.CombinedOutput(); err != nil {
			return fmt.Errorf("failed to create virtual environment: %w\nOutput: %s", err, output)
		}
	}

	packagePath := filepath.Join(homeDir, ".neuron", "packages", name, version)
	requirementsPath := filepath.Join(packagePath, "requirements.txt")
	if _, err := os.Stat(requirementsPath); err == nil {
		fmt.Printf("Installing dependencies for %s@%s...\n", name, version)
		pipCmd := exec.Command(filepath.Join(venvPath, "bin", "pip"), "install", "-r", requirementsPath, "-q", "--no-cache-dir")
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
