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
	output, err := n.RunWithOutput(entry, args, env)
	if err != nil {
		return err
	}
	fmt.Print(output)
	return nil
}

// RunWithOutput executes the Node entry file and returns the stdout output as a string.
func (n *NodeRuntime) RunWithOutput(entry string, args []string, env map[string]string) (string, error) {
	cmdArgs := append([]string{entry}, args...)
	cmd := exec.Command("node", cmdArgs...)

	if env != nil {
		envVars := os.Environ()
		for key, value := range env {
			envVars = append(envVars, fmt.Sprintf("%s=%s", key, value))
		}
		cmd.Env = envVars
	}

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

// Name returns the name of the runtime
func (n *NodeRuntime) Name() string {
	return "node"
}