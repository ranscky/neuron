package runtime

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// PythonRuntime handles Python runtime execution
type PythonRuntime struct {
	// Python runtime executor
}

// NewPythonRuntime creates a new PythonRuntime instance
func NewPythonRuntime() *PythonRuntime {
	return &PythonRuntime{}
}

// Run executes the Python entry file with the given arguments and environment variables
func (p *PythonRuntime) Run(entry string, args []string, env map[string]string) error {
	// 1. Derive the package directory from the entry path
	packageDir := filepath.Dir(entry)
	
	// Extract package name and version from path
	// Expected path format: ~/.neuron/packages/<name>/<version>/...
	pathParts := strings.Split(packageDir, string(filepath.Separator))
	var packageName, packageVersion string
	
	// Find the .neuron/packages part in the path
	for i, part := range pathParts {
		if part == ".neuron" && i+1 < len(pathParts) && pathParts[i+1] == "packages" && i+3 < len(pathParts) {
			packageName = pathParts[i+2]
			packageVersion = pathParts[i+3]
			break
		}
	}
	
	if packageName == "" || packageVersion == "" {
		// Fallback: try to extract from the full path
		// Look for pattern like .../packages/<name>/<version>/
		parts := strings.Split(packageDir, string(filepath.Separator))
		if len(parts) >= 2 {
			packageVersion = parts[len(parts)-1]
			packageName = parts[len(parts)-2]
		}
	}
	
	// Get user's home directory
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}
	
	// 2. Check if virtual environment exists
	venvPath := filepath.Join(homeDir, ".neuron", "venv", packageName, packageVersion)
	
	// 3. If not, create virtual environment
	if _, err := os.Stat(venvPath); os.IsNotExist(err) {
		fmt.Printf("Creating virtual environment for %s@%s...\n", packageName, packageVersion)
		venvCmd := exec.Command("python3", "-m", "venv", venvPath)
		if output, err := venvCmd.CombinedOutput(); err != nil {
			return fmt.Errorf("failed to create virtual environment: %w\nOutput: %s", err, output)
		}
	}
	
	// 4. Check if requirements.txt exists and install dependencies
	requirementsPath := filepath.Join(packageDir, "requirements.txt")
	if _, err := os.Stat(requirementsPath); err == nil {
		fmt.Printf("Installing dependencies for %s@%s...\n", packageName, packageVersion)
		pipCmd := exec.Command(filepath.Join(venvPath, "bin", "pip"), "install", "-r", requirementsPath, "-q")
		if output, err := pipCmd.CombinedOutput(); err != nil {
			return fmt.Errorf("failed to install dependencies: %w\nOutput: %s", err, output)
		}
	}
	
	// 5. Execute the entry file using the virtual environment's Python
	cmdArgs := append([]string{entry}, args...)
	pythonPath := filepath.Join(venvPath, "bin", "python3")
	cmd := exec.Command(pythonPath, cmdArgs...)
	
	// 6. Pass env vars from the env map to the subprocess
	if env != nil {
		envVars := os.Environ() // Start with current environment
		for key, value := range env {
			envVars = append(envVars, fmt.Sprintf("%s=%s", key, value))
		}
		cmd.Env = envVars
	}
	
	// 7. Handle stdin based on whether args are provided
	if len(args) > 0 {
		// If args is not empty, write args[0] as bytes to the subprocess's stdin pipe
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
		
		// Write args[0] to stdin and close it
		if _, err := stdin.Write([]byte(args[0])); err != nil {
			return fmt.Errorf("failed to write to stdin: %w", err)
		}
		if err := stdin.Close(); err != nil {
			return fmt.Errorf("failed to close stdin: %w", err)
		}
		
		// Wait for the command to finish
		err = cmd.Wait()
		
		// Print stdout output
		if stdoutBuf.Len() > 0 {
			fmt.Print(stdoutBuf.String())
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
