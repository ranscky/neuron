package runtime

import (
	"fmt"
	"os/exec"
)

// NodeRuntime handles Node runtime execution
type NodeRuntime struct {
	// Node runtime executor
}

// NewNodeRuntime creates a new NodeRuntime instance
func NewNodeRuntime() *NodeRuntime {
	return &NodeRuntime{}
}

// Run executes the Node entry file with the given arguments and environment variables
func (n *NodeRuntime) Run(entry string, args []string, env map[string]string) error {
	// Prepare the command
	cmdArgs := append([]string{entry}, args...)
	cmd := exec.Command("node", cmdArgs...)
	
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
		return fmt.Errorf("failed to execute Node script: %w\nOutput: %s", err, output)
	}
	
	return nil
}

// Name returns the name of the runtime
func (n *NodeRuntime) Name() string {
	return "node"
}