package runtime

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
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
	
	// Handle stdin - pipe JSON args to subprocess stdin
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
		
		// Print stdout output
		if stdoutBuf.Len() > 0 {
			fmt.Print(stdoutBuf.String())
		}
		
		// Handle stderr and exit code
		if err != nil {
			if stderrBuf.Len() > 0 {
				fmt.Fprintf(os.Stderr, "Error: %s\n", stderrBuf.String())
			}
			return fmt.Errorf("failed to execute Node script: %w", err)
		}
	} else {
		// If args is empty, connect stdout/stderr and leave stdin connected to os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Stdin = os.Stdin
		
		// Run the command
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to execute Node script: %w", err)
		}
	}
	
	return nil
}

// Name returns the name of the runtime
func (n *NodeRuntime) Name() string {
	return "node"
}