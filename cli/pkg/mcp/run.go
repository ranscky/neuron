package mcp

import (
	"fmt"
	"io"
	"os"
	"os/exec"
)

// RunServer launches a managed server with its secrets resolved from the
// keychain. This is what a client invokes when a server declares credentials:
// the client config only ever contains `neuron mcp run <name>`.
func RunServer(
	store *Store,
	name string,
	secretGet func(key string) (string, error),
	stdin io.Reader,
	stdout, stderr io.Writer,
) error {
	spec, err := store.Get(name)
	if err != nil {
		return err
	}
	if spec.Command == "" {
		return fmt.Errorf("server %q has no command to run", name)
	}

	cmd := exec.Command(spec.Command, spec.Args...)
	cmd.Env = os.Environ()

	for _, key := range sortedKeys(spec.Env) {
		cmd.Env = append(cmd.Env, key+"="+spec.Env[key])
	}
	for _, envName := range sortedKeys(spec.Secrets) {
		value, err := secretGet(spec.Secrets[envName])
		if err != nil {
			return fmt.Errorf("failed to resolve secret %q for %s: %w", spec.Secrets[envName], envName, err)
		}
		cmd.Env = append(cmd.Env, envName+"="+value)
	}

	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}
