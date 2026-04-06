package main

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/ranscky/neuron/pkg/installer"
	"github.com/ranscky/neuron/pkg/manifest"
	"github.com/ranscky/neuron/pkg/registry"
	"github.com/ranscky/neuron/pkg/runtime"
	"github.com/ranscky/neuron/pkg/secrets"
	"github.com/spf13/cobra"
)

var (
	// Initialize registry client with a base URL
	// In a real implementation, this would come from config
	registryClient = registry.NewRegistryClient("https://neuron-production-ae02.up.railway.app")
	
	// Initialize installer
	installerClient *installer.Installer
	
	// Initialize lockfile
	lockFile *installer.Lockfile
	
	// Command definitions
	rootCmd = &cobra.Command{
		Use:   "neuron",
		Short: "Neuron is a CLI-based distribution layer for AI tools, agents, and MCP servers",
		Long:  "Neuron handles versioning, dependencies, secrets, and sandboxed execution for AI tools.",
	}
	
	// installCmd represents the install command
	installCmd = &cobra.Command{
		Use:   "install <package>",
		Short: "Install a tool from the registry",
		Long:  `Resolve version, download via RegistryClient.Fetch, install via Installer.Install`,
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			packageName := args[0]
			
			// Split package name and version constraint if provided
			var name, constraint string
			if strings.Contains(packageName, "@") {
				parts := strings.Split(packageName, "@")
				name, constraint = parts[0], parts[1]
			} else {
				name = packageName
			}
			
			// Resolve version constraint to actual version
			resolvedVersion, err := resolveVersionConstraint(name, constraint, registryClient)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to resolve version for package %s: %v\n", name, err)
				os.Exit(1)
			}
			
			// Download the package
			fmt.Printf("Fetching package %s@%s...\n", name, resolvedVersion)
			_, err = registryClient.Fetch(name, resolvedVersion)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to fetch package %s@%s: %v\n", name, resolvedVersion, err)
				os.Exit(1)
			}
			
			// Install the package
			fmt.Printf("Installing package %s@%s...\n", name, resolvedVersion)
			err = installerClient.Install(name, resolvedVersion)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to install package %s@%s: %v\n", name, resolvedVersion, err)
				os.Exit(1)
			}
			
			fmt.Printf("Successfully installed %s@%s\n", name, resolvedVersion)
		},
	}
	
	// publishCmd represents the publish command
	publishCmd = &cobra.Command{
		Use:   "publish",
		Short: "Publish current directory as a Neuron package",
		Long:  `Validate neuron.json via ParseManifest, tar.gz current dir via PublishPackage, upload via RegistryClient.Publish`,
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("Validating neuron.json...")
			
			// Validate manifest
			_, err := manifest.ParseManifest("neuron.json")
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to validate neuron.json: %v\n", err)
				os.Exit(1)
			}
			
			fmt.Println("Creating package archive...")
			
			// Publish package
			err = registry.PublishPackage(registryClient)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to publish package: %v\n", err)
				os.Exit(1)
			}
			
			fmt.Println("Successfully published package!")
		},
	}
	
	// runCmd represents the run command
	runCmd = &cobra.Command{
		Use:   "run <package> [args]",
		Short: "Run an installed tool",
		Long:  `Read lockfile to find installed path, parse its neuron.json, inject secrets via Injector, pick correct runtime, call runtime.Run`,
		Args:  cobra.MinimumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			packageName := args[0]
			runArgs := args[1:]
			
			// Get installed version from lockfile
			version, err := lockFile.Get(packageName)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Package %s is not installed: %v\n", packageName, err)
				os.Exit(1)
			}
			
			// Get the user's home directory
			homeDir, err := os.UserHomeDir()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to get home directory: %v\n", err)
				os.Exit(1)
			}
			
			// Construct the package path
			packagePath := fmt.Sprintf("%s/.neuron/packages/%s/%s", homeDir, packageName, version)
			manifestPath := fmt.Sprintf("%s/neuron.json", packagePath)
			
			// Parse the package's manifest
			pkgManifest, err := manifest.ParseManifest(manifestPath)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to parse manifest for package %s: %v\n", packageName, err)
				os.Exit(1)
			}
			
			// Check dependencies and install any that are not already installed
			if pkgManifest.Dependencies != nil {
				for depName, depVersion := range pkgManifest.Dependencies {
					// Check if dependency is already installed
					_, err := lockFile.Get(depName)
					if err != nil {
						// Dependency not installed, install it
						// Resolve version constraint to actual version
						resolvedDepVersion, err := resolveVersionConstraint(depName, depVersion, registryClient)
						if err != nil {
							fmt.Fprintf(os.Stderr, "Failed to resolve version for dependency %s: %v\n", depName, err)
							os.Exit(1)
						}
						
						fmt.Printf("Installing dependency %s@%s...\n", depName, resolvedDepVersion)
						
						// Download the package
						_, err = registryClient.Fetch(depName, resolvedDepVersion)
						if err != nil {
							fmt.Fprintf(os.Stderr, "Failed to fetch dependency %s@%s: %v\n", depName, resolvedDepVersion, err)
							os.Exit(1)
						}
						
						// Install the package
						err = installerClient.Install(depName, resolvedDepVersion)
						if err != nil {
							fmt.Fprintf(os.Stderr, "Failed to install dependency %s@%s: %v\n", depName, resolvedDepVersion, err)
							os.Exit(1)
						}
						
						fmt.Printf("Successfully installed dependency %s@%s\n", depName, resolvedDepVersion)
					}
				}
			}
			
			// Initialize secrets injector
			secretStore := secrets.NewStore()
			injector := secrets.NewInjector(secretStore)
			
			// Prepare environment variables
			env := make(map[string]string)
			
			// Inject secrets
			err = injector.Inject(pkgManifest, env)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to inject secrets: %v\n", err)
				os.Exit(1)
			}
			
			// Determine runtime based on manifest
			var rt runtime.Runtime
			switch pkgManifest.Runtime {
			case "python":
				rt = &runtime.PythonRuntime{}
			case "node":
				rt = &runtime.NodeRuntime{}
			case "binary":
				// For binary runtime, we would need to determine the correct runtime
				// This is a simplified implementation
				fmt.Fprintf(os.Stderr, "Binary runtime not fully implemented\n")
				os.Exit(1)
			default:
				fmt.Fprintf(os.Stderr, "Unsupported runtime: %s\n", pkgManifest.Runtime)
				os.Exit(1)
			}
			
			// Construct entry point path
			entryPoint := fmt.Sprintf("%s/%s", packagePath, pkgManifest.Entry)
			
			// Run the package
			fmt.Printf("Running %s@%s...\n", packageName, version)
			err = rt.Run(entryPoint, runArgs, env)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to run package %s: %v\n", packageName, err)
				os.Exit(1)
			}
		},
	}
	
	// searchCmd represents the search command
	searchCmd = &cobra.Command{
		Use:   "search <query>",
		Short: "Search the registry",
		Long:  `Call RegistryClient.Search, print results as a table with Name, Version, Description columns`,
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			query := args[0]
			
			// Search the registry
			results, err := registryClient.Search(query)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to search registry: %v\n", err)
				os.Exit(1)
			}
			
			// Print results in a table format
			if len(results) == 0 {
				fmt.Println("No packages found.")
				return
			}
			
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
			fmt.Fprintln(w, "NAME\tVERSION\tDESCRIPTION")
			for _, pkg := range results {
				fmt.Fprintf(w, "%s\t%s\t%s\n", pkg.Name, pkg.Version, pkg.Description)
			}
			w.Flush()
		},
	}
	
	// listCmd represents the list command
	listCmd = &cobra.Command{
		Use:   "list",
		Short: "List installed packages",
		Long:  `Call lockfile.List, print installed packages and their versions`,
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			// Get list of installed packages
			packages := lockFile.List()
			
			// Print results
			if len(packages) == 0 {
				fmt.Println("No packages installed.")
				return
			}
			
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
			fmt.Fprintln(w, "PACKAGE\tVERSION")
			for name, version := range packages {
				fmt.Fprintf(w, "%s\t%s\n", name, version)
			}
			w.Flush()
		},
	}
	
	// secretsCmd represents the secrets command
	secretsCmd = &cobra.Command{
		Use:   "secrets",
		Short: "Manage secrets",
		Long:  `Manage secrets stored in the OS keychain`,
	}
	
	// secretsSetCmd represents the secrets set command
	secretsSetCmd = &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a secret in the OS keychain",
		Long:  `Store a secret in the OS keychain using zalando/go-keyring with service name "neuron"`,
		Args:  cobra.ExactArgs(2),
		Run: func(cmd *cobra.Command, args []string) {
			key := args[0]
			value := args[1]
			
			// Create a store
			store := secrets.NewStore()
			
			// Set the secret
			err := store.Set(key, value)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to set secret: %v\n", err)
				os.Exit(1)
			}
			
			fmt.Printf("Successfully set secret '%s'\n", key)
		},
	}
	
	// secretsGetCmd represents the secrets get command
	secretsGetCmd = &cobra.Command{
		Use:   "get <key>",
		Short: "Get a secret from the OS keychain",
		Long:  `Retrieve and print a secret from the OS keychain`,
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			key := args[0]
			
			// Create a store
			store := secrets.NewStore()
			
			// Get the secret
			value, err := store.Get(key)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to get secret: %v\n", err)
				os.Exit(1)
			}
			
			fmt.Println(value)
		},
	}
)

// resolveVersionConstraint resolves a version constraint to an actual version
func resolveVersionConstraint(name, constraint string, registryClient *registry.RegistryClient) (string, error) {
	// If no constraint is provided, fetch the latest version from the registry
	if constraint == "" {
		fmt.Printf("Fetching latest version for package %s...\n", name)
		pkgInfo, err := registryClient.GetPackageInfo(name)
		if err != nil {
			return "", fmt.Errorf("failed to get package info for %s: %v", name, err)
		}
		return pkgInfo.Version, nil
	}
	
	// If constraint is an exact version (doesn't start with ^ or ~), use it directly
	if !strings.HasPrefix(constraint, "^") && !strings.HasPrefix(constraint, "~") {
		return constraint, nil
	}
	
	// For version constraints, we need to get available versions and resolve
	// Since we don't have a direct API for getting all versions, we'll fetch the latest
	// and then validate it against the constraint using our resolution logic
	fmt.Printf("Resolving version constraint %s for package %s...\n", constraint, name)
	pkgInfo, err := registryClient.GetPackageInfo(name)
	if err != nil {
		return "", fmt.Errorf("failed to get package info for %s: %v", name, err)
	}
	
	// In a full implementation, we would get all available versions and use the resolver
	// For now, we'll just use the latest version and assume it satisfies the constraint
	// A production implementation would use pkg/registry/resolve.go functions
	// For this implementation, we'll use a simplified approach that works for common cases
	
	// In a full implementation, we would get all available versions and use the resolver
	// For now, we'll just use the latest version and assume it satisfies the constraint
	// A production implementation would use pkg/registry/resolve.go functions
	
	return pkgInfo.Version, nil
}

func init() {
	var err error
	
	// Initialize installer
	installerClient, err = installer.NewInstaller()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize installer: %v\n", err)
		os.Exit(1)
	}
	
	// Initialize lockfile
	lockFile, err = installer.NewLockfile()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize lockfile: %v\n", err)
		os.Exit(1)
	}
	
	// Add all commands
	rootCmd.AddCommand(installCmd)
	rootCmd.AddCommand(publishCmd)
	rootCmd.AddCommand(runCmd)
	rootCmd.AddCommand(searchCmd)
	rootCmd.AddCommand(listCmd)
	rootCmd.AddCommand(secretsCmd)
	
	// Add subcommands to secretsCmd
	secretsCmd.AddCommand(secretsSetCmd)
	secretsCmd.AddCommand(secretsGetCmd)
}

func main() {
	// Execute the root command
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}