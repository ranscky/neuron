package executor

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/ranscky/neuron/pkg/installer"
	"github.com/ranscky/neuron/pkg/lockfile"
	"github.com/ranscky/neuron/pkg/manifest"
	"github.com/ranscky/neuron/pkg/runtime"
	"github.com/ranscky/neuron/pkg/secrets"
)

type Executor struct {
	installer   *installer.Installer
	lockfile    *lockfile.Lockfile
	secretStore *secrets.Store
}

func NewExecutor(inst *installer.Installer, lf *lockfile.Lockfile, ss *secrets.Store) *Executor {
	return &Executor{
		installer:   inst,
		lockfile:    lf,
		secretStore: ss,
	}
}

func (e *Executor) Execute(packageName string, runArgs []string) (string, error) {
	// Get installed version from lockfile
	version, err := e.lockfile.Get(packageName)
	if err != nil {
		return "", fmt.Errorf("package %s is not installed: %w", packageName, err)
	}

	// Get the user's home directory
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}

	// Construct the package path
	packagePath := filepath.Join(homeDir, ".neuron", "packages", packageName, version)
	manifestPath := filepath.Join(packagePath, "neuron.json")

	// Parse the package's manifest
	pkgManifest, err := manifest.ParseManifest(manifestPath)
	if err != nil {
		return "", fmt.Errorf("failed to parse manifest for package %s: %w", packageName, err)
	}

	// Check dependencies and install any that are not already installed
	if pkgManifest.Dependencies != nil {
		for depName := range pkgManifest.Dependencies {
			_, err := e.lockfile.Get(depName)
			if err != nil {
				// Resolve version constraint to actual version
				// For now, we just use the latest version from registry (simplified)
				// This is a gap we'll fill in the laer phases

				// Since we don't have the registry client here, we'll just report the error
				// or we could pass the registry client to Executor
				return "", fmt.Errorf("dependency %s is not installed. Please install it first: neuron install %s", depName, depName)
			}
		}
	}

	// Initialize secrets injector
	injector := secrets.NewInjector(e.secretStore)
	env := make(map[string]string)
	if err := injector.Inject(pkgManifest, env); err != nil {
		return "", fmt.Errorf("failed to inject secrets: %w", err)
	}

	// Determine runtime based on manifest
	var rt runtime.Runtime
	switch pkgManifest.Runtime {
	case "python":
		rt = runtime.NewPythonRuntime()
	case "node":
		rt = &runtime.NodeRuntime{} // Assume this is implemented or being implemented
	default:
		return "", fmt.Errorf("unsupported runtime: %s", pkgManifest.Runtime)
	}

	// Construct entry point path
	entryPoint := filepath.Join(packagePath, pkgManifest.Entry)

	// Execute
	output, err := rt.RunWithOutput(entryPoint, runArgs, env)
	if err != nil {
		return "", fmt.Errorf("execution failed: %w", err)
	}

	return output, nil
}
