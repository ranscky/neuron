package runtime

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ranscky/neuron/internal/config"
)

// isValidSemver checks if a string is a valid semantic version (X.Y.Z format)
func isValidSemver(version string) bool {
	// Regular expression for semantic versioning (X.Y.Z)
	semverRegex := regexp.MustCompile(`^\d+\.\d+\.\d+$`)
	return semverRegex.MatchString(version)
}

// PythonRuntime handles Python runtime execution
type PythonRuntime struct {
	// Python runtime executor
}

// NewPythonRuntime creates a new PythonRuntime instance
func NewPythonRuntime() *PythonRuntime {
	return &PythonRuntime{}
}

// setupPackageVenv creates a virtual environment for a package and installs its requirements.
func (p *PythonRuntime) setupPackageVenv(name, version string) error {
	// Get user's home directory
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}
	
	// Create venv at ~/.neuron/venv/<name>/ (preserving scoped package paths)
	// Replace slashes in scoped package names with underscores
	venvName := strings.ReplaceAll(name, "/", "_")
	venvPath := filepath.Join(homeDir, ".neuron", "venv", venvName)
	if err := os.MkdirAll(venvPath, 0755); err != nil {
		return fmt.Errorf("failed to create venv directory: %w", err)
	}
	
	// Check if venv already exists
	venvPythonPath := filepath.Join(venvPath, "bin", "python3")
	if _, err := os.Stat(venvPythonPath); os.IsNotExist(err) {
		// Create virtual environment
		fmt.Printf("Creating virtual environment for %s...\n", name)
		venvCmd := exec.Command("python3", "-m", "venv", venvPath)
		if output, err := venvCmd.CombinedOutput(); err != nil {
			return fmt.Errorf("failed to create virtual environment: %w\nOutput: %s", err, output)
		}
	}
	
	// Install ~/.neuron/packages/<name>/<version>/requirements.txt if it exists
	packagePath := filepath.Join(homeDir, ".neuron", "packages", name, version)
	requirementsPath := filepath.Join(packagePath, "requirements.txt")
	if _, err := os.Stat(requirementsPath); err == nil {
		fmt.Printf("Installing dependencies for %s@%s...\n", name, version)
		// Use the venv's own pip with the specified flags
		pipCmd := exec.Command(filepath.Join(venvPath, "bin", "pip"), "install", "-r", requirementsPath, "-q", "--no-cache-dir")
		
		// Capture stderr separately to provide better error messages
		var stderr bytes.Buffer
		pipCmd.Stderr = &stderr
		
		// Run the command and check for errors
		if err := pipCmd.Run(); err != nil {
			// Return the error with the full stderr output
			return fmt.Errorf("failed to install dependencies: %w\nStderr: %s", err, stderr.String())
		}
	}
	
	return nil
}

// Run executes the Python entry file with the given arguments and environment variables
func (p *PythonRuntime) Run(entry string, args []string, env map[string]string) error {
	// 1. Derive the package directory from the entry path
	packageDir := filepath.Dir(entry)
	
	// Extract package name and version from path
	// Expected path format: ~/.neuron/packages/<name>/<version>/...
	// For scoped packages: ~/.neuron/packages/tools/web-search/1.0.2/main.py
	// packageName should be "tools/web-search" and packageVersion should be "1.0.2"
	pathParts := strings.Split(packageDir, string(filepath.Separator))
	var packageName, packageVersion string
	
	// Find the .neuron/packages part in the path
	for i, part := range pathParts {
		if part == ".neuron" && i+1 < len(pathParts) && pathParts[i+1] == "packages" {
			// Start looking for the version after .neuron/packages
			// Everything before the version is the package name
			// The segment that matches semver pattern is the version
			
			// Collect potential package name parts
			var nameParts []string
			
			// Process segments after .neuron/packages
			for j := i + 2; j < len(pathParts); j++ {
				segment := pathParts[j]
				// Check if this segment is a valid semantic version
				if isValidSemver(segment) {
					// This segment is a valid version
					packageVersion = segment
					packageName = strings.Join(nameParts, "/")
					break
				} else {
					// This segment is part of the package name
					nameParts = append(nameParts, segment)
				}
			}
			break
		}
	}
	
	// Fallback to original logic if we couldn't parse using the new method
	if packageName == "" || packageVersion == "" {
		// Find the .neuron/packages part in the path
		for i, part := range pathParts {
			if part == ".neuron" && i+1 < len(pathParts) && pathParts[i+1] == "packages" && i+3 < len(pathParts) {
				packageName = pathParts[i+2]
				packageVersion = pathParts[i+3]
				break
			}
		}
		
		// Final fallback: try to extract from the full path
		// Look for pattern like .../packages/<name>/<version>/
		if packageName == "" || packageVersion == "" {
			parts := strings.Split(packageDir, string(filepath.Separator))
			if len(parts) >= 2 {
				packageVersion = parts[len(parts)-1]
				packageName = parts[len(parts)-2]
			}
		}
	}
	
	// Setup virtual environment for the package
	if err := p.setupPackageVenv(packageName, packageVersion); err != nil {
		return fmt.Errorf("failed to setup virtual environment: %w", err)
	}
	
	// Get the Python interpreter path using the shared utility function
	pythonPath, err := GetPackageVenvPython(packageName, packageVersion)
	if err != nil {
		return fmt.Errorf("failed to get Python venv path: %w", err)
	}
	
	// Execute the entry file using the virtual environment's Python
	cmdArgs := append([]string{entry}, args...)
	cmd := exec.Command(pythonPath, cmdArgs...)
	
	// 6. Load Neuron config to determine active provider
	cfg, err := config.LoadConfig()
	if err != nil {
		return fmt.Errorf("failed to load Neuron config: %w", err)
	}
	
	// 7. Pass env vars from the env map to the subprocess
	envVars := os.Environ() // Start with current environment
	
	// Add environment variables from the passed env map
	if env != nil {
		for key, value := range env {
			envVars = append(envVars, fmt.Sprintf("%s=%s", key, value))
		}
	}
	
	// Inject provider-specific environment variables
	envVars = append(envVars, fmt.Sprintf("NEURON_PROVIDER=%s", cfg.Provider))
	
	switch cfg.Provider {
	case "ollama":
		envVars = append(envVars, fmt.Sprintf("NEURON_OLLAMA_BASE_URL=%s", cfg.Ollama.BaseURL))
	case "openai":
		envVars = append(envVars, fmt.Sprintf("NEURON_OPENAI_API_KEY=%s", cfg.OpenAI.APIKey))
		envVars = append(envVars, fmt.Sprintf("NEURON_MODEL=%s", cfg.OpenAI.Model))
	case "anthropic":
		envVars = append(envVars, fmt.Sprintf("NEURON_ANTHROPIC_API_KEY=%s", cfg.Anthropic.APIKey))
		envVars = append(envVars, fmt.Sprintf("NEURON_MODEL=%s", cfg.Anthropic.Model))
	case "groq":
		envVars = append(envVars, fmt.Sprintf("NEURON_GROQ_API_KEY=%s", cfg.Groq.APIKey))
		envVars = append(envVars, fmt.Sprintf("NEURON_MODEL=%s", cfg.Groq.Model))
	}
	
	cmd.Env = envVars
	
	// 7. Handle stdin - pipe JSON args to subprocess stdin
	if len(args) > 0 {
		// Join all args into a single JSON string and write to stdin
		stdin, err := cmd.StdinPipe()
		if err != nil {
			return fmt.Errorf("failed to create stdin pipe: %w", err)
		}
		
		// Create buffers to capture stdout and stderr
		stdoutBuf := &bytes.Buffer{}
		stderrBuf := &bytes.Buffer{}
		cmd.Stdout = stdoutBuf
		cmd.Stderr = stderrBuf
		
		// Start the command
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("failed to start command: %w", err)
		}
		
		// Write all args as JSON to stdin and close it
		jsonArgs := strings.Join(args, " ")
		if _, err := stdin.Write([]byte(jsonArgs)); err != nil {
			return fmt.Errorf("failed to write to stdin: %w", err)
		}
		if err := stdin.Close(); err != nil {
			return fmt.Errorf("failed to close stdin: %w", err)
		}
		
		// Wait for the command to finish
		err = cmd.Wait()
		
		// Print stdout output with pretty-printing if it's JSON
		if stdoutBuf.Len() > 0 {
			output := stdoutBuf.String()
			
			// Try to parse as JSON and pretty-print if it is
			var jsonData interface{}
			if json.Unmarshal([]byte(output), &jsonData) == nil {
				// It's valid JSON, pretty-print it
				prettyJSON, err := json.MarshalIndent(jsonData, "", "  ")
				if err == nil {
					fmt.Println(string(prettyJSON))
				} else {
					// If pretty-printing fails, print as-is
					fmt.Print(output)
				}
			} else {
				// Not JSON, print as-is
				fmt.Print(output)
			}
		}
		
		// Handle stderr and exit code
		if err != nil {
			if stderrBuf.Len() > 0 {
				fmt.Fprintf(os.Stderr, "Error: %s\n", stderrBuf.String())
			}
			return fmt.Errorf("failed to execute Python script: %w", err)
		}
	} else {
		// If args is empty, leave stdin connected to os.Stdin so the user can type input
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		
		// Run the command
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to execute Python script: %w", err)
		}
	}
	
	return nil
}

// Name returns the name of the runtime
func (p *PythonRuntime) Name() string {
	return "python"
}
