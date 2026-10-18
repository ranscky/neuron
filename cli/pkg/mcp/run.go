package mcp

import (
	"fmt"
	"io"
	"os"
	"os/exec"

	"github.com/ranscky/neuron/internal/proxy"
)

// Options tunes one supervised session.
type Options struct {
	// Restart controls supervision of the server process. The zero value means
	// no restarts; use DefaultRestartPolicy for the normal behaviour.
	Restart RestartPolicy
	// Guard may refuse calls before they reach the server (see internal/policy).
	// A nil guard allows everything.
	Guard proxy.Guard
}

// RunServer launches a managed server with its secrets resolved from the
// keychain, proxying its JSON-RPC traffic so every call can be recorded.
//
// This is the single seam between a client and a real MCP server. It is what
// makes both "secrets never touch a client config" and the observability
// dashboard true. A nil Recorder disables recording; traffic is still proxied.
//
// The process is supervised: if it exits while the client is still connected it
// is restarted with exponential backoff (see DefaultRestartPolicy), and requests
// the dead process never answered are answered with an error rather than left
// hanging.
func RunServer(
	store *Store,
	name string,
	secretGet func(key string) (string, error),
	rec proxy.Recorder,
	stdin io.Reader,
	stdout, stderr io.Writer,
) error {
	return RunServerWithOptions(store, name, secretGet, rec, stdin, stdout, stderr,
		Options{Restart: DefaultRestartPolicy})
}

// RunServerWithPolicy is RunServer with an explicit supervision policy.
func RunServerWithPolicy(
	store *Store,
	name string,
	secretGet func(key string) (string, error),
	rec proxy.Recorder,
	stdin io.Reader,
	stdout, stderr io.Writer,
	policy RestartPolicy,
) error {
	return RunServerWithOptions(store, name, secretGet, rec, stdin, stdout, stderr,
		Options{Restart: policy})
}

// RunServerWithOptions is RunServer with full control over the session.
func RunServerWithOptions(
	store *Store,
	name string,
	secretGet func(key string) (string, error),
	rec proxy.Recorder,
	stdin io.Reader,
	stdout, stderr io.Writer,
	opts Options,
) error {
	spec, err := store.Get(name)
	if err != nil {
		return err
	}

	sess := proxy.NewSession(name, stdout, rec)
	sess.SetGuard(opts.Guard)

	// A server registered with a URL is reached over HTTP instead of spawned; the
	// session, policy and recording are identical either way.
	if spec.Command == "" {
		if spec.URL == "" {
			return fmt.Errorf("server %q has neither a command nor a url", name)
		}
		return runRemote(spec, sess, proxy.NewReaderSource(stdin))
	}

	env, err := serverEnv(spec, secretGet)
	if err != nil {
		return err
	}

	start := func() (*generation, error) {
		return startProcess(spec, env, stderr)
	}
	return supervise(name, sess, proxy.NewReaderSource(stdin), stderr, start, opts.Restart)
}

// serverEnv builds the child's environment: ours, plus declared env and the
// secrets resolved from the OS keychain.
//
// Secrets are resolved once per session rather than per restart, so an
// unavailable keychain cannot turn a transient server crash into a dead session.
func serverEnv(spec ServerSpec, secretGet func(key string) (string, error)) ([]string, error) {
	env := os.Environ()

	for _, key := range sortedKeys(spec.Env) {
		env = append(env, key+"="+spec.Env[key])
	}
	for _, envName := range sortedKeys(spec.Secrets) {
		value, err := secretGet(spec.Secrets[envName])
		if err != nil {
			return nil, fmt.Errorf("failed to resolve secret %q for %s: %w", spec.Secrets[envName], envName, err)
		}
		env = append(env, envName+"="+value)
	}
	return env, nil
}

// startProcess launches one generation of the server.
func startProcess(spec ServerSpec, env []string, stderr io.Writer) (*generation, error) {
	cmd := exec.Command(spec.Command, spec.Args...)
	cmd.Env = env
	cmd.Stderr = stderr

	serverIn, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	serverOut, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	return &generation{in: serverIn, out: serverOut, wait: cmd.Wait}, nil
}
