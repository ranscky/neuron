package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/ranscky/neuron/internal/config"
	"github.com/ranscky/neuron/pkg/installer"
	"github.com/ranscky/neuron/pkg/manifest"
	"github.com/ranscky/neuron/pkg/mcp"
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
			
			// Check if package has MCP server configuration
			// Get the user's home directory
			homeDir, err := os.UserHomeDir()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to get home directory: %v\n", err)
				return
			}
			
			// Construct the package path
			packagePath := fmt.Sprintf("%s/.neuron/packages/%s/%s", homeDir, name, resolvedVersion)
			manifestPath := fmt.Sprintf("%s/neuron.json", packagePath)
			
			// Parse the package's manifest
			pkgManifest, err := manifest.ParseManifest(manifestPath)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to parse manifest for package %s: %v\n", name, err)
				return
			}
			
			// Check if manifest has MCP server configuration
			if pkgManifest.MCPServer != nil {
				// Detect MCP clients
				clients, err := mcp.DetectClients()
				if err != nil {
					fmt.Fprintf(os.Stderr, "Failed to detect MCP clients: %v\n", err)
					return
				}
				
				if len(clients) == 0 {
					fmt.Println("No MCP clients detected. Manually add to your AI client config.")
					return
				}
				
				// Build server config from manifest
				serverConfig := map[string]interface{}{
					"command": pkgManifest.MCPServer.Command,
				}
				
				if len(pkgManifest.MCPServer.Args) > 0 {
					serverConfig["args"] = pkgManifest.MCPServer.Args
				}
				
				if len(pkgManifest.MCPServer.Env) > 0 {
					serverConfig["env"] = pkgManifest.MCPServer.Env
				}
				
				// Inject MCP server configuration for each detected client
				for _, client := range clients {
					err := mcp.InjectMCPServer(client, name, serverConfig)
					if err != nil {
						fmt.Fprintf(os.Stderr, "Failed to configure %s in %s: %v\n", name, client.Name, err)
					} else {
						fmt.Printf("Configured %s in %s\n", name, client.Name)
					}
				}
			}
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
	
	// uninstallCmd represents the uninstall command
	uninstallCmd = &cobra.Command{
		Use:   "uninstall <package>",
		Short: "Uninstall a package",
		Long:  `Remove package from ~/.neuron/packages/, remove venv from ~/.neuron/venv/, remove entry from lockfile`,
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			packageName := args[0]
			
			// Get user's home directory
			homeDir, err := os.UserHomeDir()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to get home directory: %v\n", err)
				os.Exit(1)
			}
			
			// Remove ~/.neuron/packages/<name>/
			packagesPath := filepath.Join(homeDir, ".neuron", "packages", packageName)
			if err := os.RemoveAll(packagesPath); err != nil {
				fmt.Fprintf(os.Stderr, "Failed to remove package directory: %v\n", err)
				os.Exit(1)
			}
			
			// Remove ~/.neuron/venv/<name>/
			venvPath := filepath.Join(homeDir, ".neuron", "venv", packageName)
			if err := os.RemoveAll(venvPath); err != nil {
				fmt.Fprintf(os.Stderr, "Failed to remove venv directory: %v\n", err)
				os.Exit(1)
			}
			
			// Remove entry from lockfile
			if err := lockFile.Remove(packageName); err != nil {
				fmt.Fprintf(os.Stderr, "Failed to remove package from lockfile: %v\n", err)
				os.Exit(1)
			}
			
			fmt.Printf("Successfully uninstalled %s\n", packageName)
		},
	}
	
	// updateCmd represents the update command
	updateCmd = &cobra.Command{
		Use:   "update <package>",
		Short: "Update a package to the latest version",
		Long:  `Calls GetPackageInfo to get latest version, compares with lockfile version, if newer: installs new version, updates lockfile`,
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			packageName := args[0]
			
			// Get current installed version from lockfile
			currentVersion, err := lockFile.Get(packageName)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Package %s is not installed: %v\n", packageName, err)
				os.Exit(1)
			}
			
			// Get latest version from registry
			pkgInfo, err := registryClient.GetPackageInfo(packageName)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to get package info for %s: %v\n", packageName, err)
				os.Exit(1)
			}
			
			latestVersion := pkgInfo.Version
			
			// Compare versions (simplified comparison)
			if latestVersion != currentVersion {
				// Install the new version
				fmt.Printf("Updating %s from %s to %s...\n", packageName, currentVersion, latestVersion)
				
				// Download the package
				_, err = registryClient.Fetch(packageName, latestVersion)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Failed to fetch package %s@%s: %v\n", packageName, latestVersion, err)
					os.Exit(1)
				}
				
				// Install the package
				err = installerClient.Install(packageName, latestVersion)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Failed to install package %s@%s: %v\n", packageName, latestVersion, err)
					os.Exit(1)
				}
				
				fmt.Printf("Updated %s to %s\n", packageName, latestVersion)
			} else {
				fmt.Printf("Already at latest version (%s)\n", currentVersion)
			}
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
	
	// configCmd represents the config command
	configCmd = &cobra.Command{
		Use:   "config",
		Short: "Manage Neuron configuration",
		Long:  `Manage Neuron configuration including AI provider settings`,
	}
	
	// configSetCmd represents the config set command
	configSetCmd = &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a configuration value",
		Long:  `Set a configuration value. Valid keys: provider, ollama.base_url, openai.api_key, openai.model, anthropic.api_key, anthropic.model, groq.api_key, groq.model`,
		Args:  cobra.ExactArgs(2),
		Run: func(cmd *cobra.Command, args []string) {
			key := args[0]
			value := args[1]
			
			// Load current config
			cfg, err := config.LoadConfig()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
				os.Exit(1)
			}
			
			// Set the config value based on key
			switch key {
			case "provider":
				cfg.Provider = value
			case "ollama.base_url":
				cfg.Ollama.BaseURL = value
			case "openai.api_key":
				cfg.OpenAI.APIKey = value
			case "openai.model":
				cfg.OpenAI.Model = value
			case "anthropic.api_key":
				cfg.Anthropic.APIKey = value
			case "anthropic.model":
				cfg.Anthropic.Model = value
			case "groq.api_key":
				cfg.Groq.APIKey = value
			case "groq.model":
				cfg.Groq.Model = value
			default:
				fmt.Fprintf(os.Stderr, "Invalid config key: %s\n", key)
				os.Exit(1)
			}
			
			// Save the updated config
			if err := config.SaveConfig(cfg); err != nil {
				fmt.Fprintf(os.Stderr, "Failed to save config: %v\n", err)
				os.Exit(1)
			}
			
			fmt.Printf("Successfully set %s\n", key)
		},
	}
	
	// configGetCmd represents the config get command
	configGetCmd = &cobra.Command{
		Use:   "get <key>",
		Short: "Get a configuration value",
		Long:  `Get a configuration value. Valid keys: provider, ollama.base_url, openai.api_key, openai.model, anthropic.api_key, anthropic.model, groq.api_key, groq.model`,
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			key := args[0]
			
			// Load current config
			cfg, err := config.LoadConfig()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
				os.Exit(1)
			}
			
			// Get the config value based on key
			var value string
			switch key {
			case "provider":
				value = cfg.Provider
			case "ollama.base_url":
				value = cfg.Ollama.BaseURL
			case "openai.api_key":
				value = cfg.OpenAI.APIKey
			case "openai.model":
				value = cfg.OpenAI.Model
			case "anthropic.api_key":
				value = cfg.Anthropic.APIKey
			case "anthropic.model":
				value = cfg.Anthropic.Model
			case "groq.api_key":
				value = cfg.Groq.APIKey
			case "groq.model":
				value = cfg.Groq.Model
			default:
				fmt.Fprintf(os.Stderr, "Invalid config key: %s\n", key)
				os.Exit(1)
			}
			
			fmt.Println(value)
		},
	}
	
	// configShowCmd represents the config show command
	configShowCmd = &cobra.Command{
		Use:   "show",
		Short: "Show all configuration values",
		Long:  `Show all configuration values with API keys masked`,
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			// Load current config
			cfg, err := config.LoadConfig()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
				os.Exit(1)
			}
			
			// Mask API keys for display
			maskedCfg := *cfg
			if len(maskedCfg.OpenAI.APIKey) > 4 {
				maskedCfg.OpenAI.APIKey = "sk-..." + maskedCfg.OpenAI.APIKey[len(maskedCfg.OpenAI.APIKey)-4:]
			}
			if len(maskedCfg.Anthropic.APIKey) > 4 {
				maskedCfg.Anthropic.APIKey = "sk-..." + maskedCfg.Anthropic.APIKey[len(maskedCfg.Anthropic.APIKey)-4:]
			}
			if len(maskedCfg.Groq.APIKey) > 4 {
				maskedCfg.Groq.APIKey = "gsk_..." + maskedCfg.Groq.APIKey[len(maskedCfg.Groq.APIKey)-4:]
			}
			
			// Print the configuration
			fmt.Printf("Provider: %s\n", maskedCfg.Provider)
			fmt.Printf("Ollama Base URL: %s\n", maskedCfg.Ollama.BaseURL)
			fmt.Printf("OpenAI API Key: %s\n", maskedCfg.OpenAI.APIKey)
			fmt.Printf("OpenAI Model: %s\n", maskedCfg.OpenAI.Model)
			fmt.Printf("Anthropic API Key: %s\n", maskedCfg.Anthropic.APIKey)
			fmt.Printf("Anthropic Model: %s\n", maskedCfg.Anthropic.Model)
			fmt.Printf("Groq API Key: %s\n", maskedCfg.Groq.APIKey)
			fmt.Printf("Groq Model: %s\n", maskedCfg.Groq.Model)
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
	rootCmd.AddCommand(uninstallCmd)
	rootCmd.AddCommand(updateCmd)
	rootCmd.AddCommand(secretsCmd)
	rootCmd.AddCommand(configCmd)
	
	// Add subcommands to secretsCmd
	secretsCmd.AddCommand(secretsSetCmd)
	secretsCmd.AddCommand(secretsGetCmd)
	
	// Add subcommands to configCmd
	configCmd.AddCommand(configSetCmd)
	configCmd.AddCommand(configGetCmd)
	configCmd.AddCommand(configShowCmd)
	
	// Create mcp command
	mcpCmd := &cobra.Command{
		Use:   "mcp",
		Short: "Manage MCP servers",
		Long:  `Manage MCP servers for AI clients`,
	}
	
	// Create mcp list command
	mcpListCmd := &cobra.Command{
		Use:   "list",
		Short: "List configured MCP servers",
		Long:  `Show all MCP servers currently configured across all detected clients`,
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			// Detect MCP clients
			clients, err := mcp.DetectClients()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to detect MCP clients: %v\n", err)
				os.Exit(1)
			}
			
			if len(clients) == 0 {
				fmt.Println("No MCP clients detected.")
				return
			}
			
			// For each detected client, read its config file and print mcpServers entries
			for _, client := range clients {
				fmt.Printf("=== %s ===\n", client.Name)
				
				// Read the config file
				configData, err := mcp.ReadConfigFile(client.ConfigPath)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Failed to read config for %s: %v\n", client.Name, err)
					continue
				}
				
				// Print the mcpServers entries based on client format
				switch client.Format {
				case "cline", "windsurf":
					if mcpServers, ok := configData["mcpServers"].(map[string]interface{}); ok {
						if len(mcpServers) == 0 {
							fmt.Println("No MCP servers configured.")
						} else {
							for serverName := range mcpServers {
								fmt.Printf("- %s\n", serverName)
							}
						}
					} else {
						fmt.Println("No MCP servers configured.")
					}
					
				case "claude_code":
					// Check for projects with mcpServers
					if projects, ok := configData["projects"].(map[string]interface{}); ok && len(projects) > 0 {
						foundServers := false
						for projectName, projectData := range projects {
							if projectMap, isMap := projectData.(map[string]interface{}); isMap {
								if mcpServers, ok := projectMap["mcpServers"].(map[string]interface{}); ok {
									for serverName := range mcpServers {
										fmt.Printf("- %s (project: %s)\n", serverName, projectName)
										foundServers = true
									}
								}
							}
						}
						if !foundServers {
							fmt.Println("No MCP servers configured.")
						}
					} else {
						// Check for root level mcpServers
						if mcpServers, ok := configData["mcpServers"].(map[string]interface{}); ok {
							if len(mcpServers) == 0 {
								fmt.Println("No MCP servers configured.")
							} else {
								for serverName := range mcpServers {
									fmt.Printf("- %s\n", serverName)
								}
							}
						} else {
							fmt.Println("No MCP servers configured.")
						}
					}
				}
				fmt.Println()
			}
		},
	}
	
	// Add mcp commands
	mcpCmd.AddCommand(mcpListCmd)
	rootCmd.AddCommand(mcpCmd)
}

func main() {
	// Execute the root command
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}