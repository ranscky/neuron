package runtime

import (
	"fmt"
	"os/exec"
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
	// Prepare the command
	cmdArgs := append([]string{entry}, args...)
	cmd := exec.Command("python3", cmdArgs...)
	
	// Set environment variables
	if env != nil {
		envVars := []string{}
		for key, value := range env {
			envVars = append(envVars, fmt.Sprintf("%s=%s", key, value))
		}
		cmd.Env = append(cmd.Env, envVars...)
	}
	
	// Run the command
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to execute Python script: %w\nOutput: %s", err, output)
	}
	
	return nil
}

// Name returns the name of the runtime
func (p *PythonRuntime) Name() string {
	return "python"
}