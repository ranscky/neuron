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
	registryClient = registry.NewRegistryClient("https://registry.neuron.ai")
	
	// Initialize installer
	installerClient *installer.Installer
	
	// Initialize lockfile
	lockFile *installer.Lockfile
)

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
}

func main() {
	var rootCmd = &cobra.Command{
		Use:   "neuron",
		Short: "Neuron is a CLI-based distribution layer for AI tools, agents, and MCP servers",
		Long:  "Neuron handles versioning, dependencies, secrets, and sandboxed execution for AI tools.",
	}
	
	// Add all commands
	rootCmd.AddCommand(installCmd)
	rootCmd.AddCommand(publishCmd)
	rootCmd.AddCommand(runCmd)
	rootCmd.AddCommand(searchCmd)
	rootCmd.AddCommand(listCmd)
	
	// Execute the root command
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// installCmd represents the install command
var installCmd = &cobra.Command{
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
		
		// For now, we'll use a placeholder for version resolution
		// In a real implementation, we would resolve the version constraint
		version := "1.0.0"
		if constraint != "" {
			version = constraint
		}
		
		// Download the package
		fmt.Printf("Fetching package %s@%s...\n", name, version)
		_, err := registryClient.Fetch(name, version)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to fetch package %s@%s: %v\n", name, version, err)
			os.Exit(1)
		}
		
		// Install the package
		fmt.Printf("Installing package %s@%s...\n", name, version)
		err = installerClient.Install(name, version)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to install package %s@%s: %v\n", name, version, err)
			os.Exit(1)
		}
		
		fmt.Printf("Successfully installed %s@%s\n", name, version)
	},
}

// publishCmd represents the publish command
var publishCmd = &cobra.Command{
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
var runCmd = &cobra.Command{
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
var searchCmd = &cobra.Command{
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
var listCmd = &cobra.Command{
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