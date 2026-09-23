package mcp

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"

	"github.com/ranscky/neuron/internal/proxy"
)

// RunServer launches a managed server with its secrets resolved from the
// keychain, proxying its JSON-RPC traffic so every call can be recorded.
//
// This is the single seam between a client and a real MCP server. It is what
// makes both "secrets never touch a client config" and the observability
// dashboard true. A nil Recorder disables recording; traffic is still proxied.
func RunServer(
	store *Store,
	name string,
	secretGet func(key string) (string, error),
	rec proxy.Recorder,
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

	serverIn, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	serverOut, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = stderr

	if err := cmd.Start(); err != nil {
		return err
	}

	pumpErr := proxy.Pump(name, stdin, stdout, serverIn, serverOut, rec)

	// The session is over. Make sure the server is not left waiting on stdin,
	// then reap it.
	_ = serverIn.Close()
	waitErr := cmd.Wait()

	if pumpErr != nil {
		return pumpErr
	}

	// MCP clients terminate their servers routinely, so a non-zero exit is not
	// worth surfacing. The server's stderr has already been forwarded.
	var exitErr *exec.ExitError
	if errors.As(waitErr, &exitErr) {
		return nil
	}
	return waitErr
}
