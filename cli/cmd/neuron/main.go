package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/AlecAivazis/survey/v2"
	"github.com/fatih/color"
	"github.com/ranscky/neuron/internal/config"
	"github.com/ranscky/neuron/pkg/executor"
	"github.com/ranscky/neuron/pkg/installer"
	"github.com/ranscky/neuron/pkg/lockfile"
	"github.com/ranscky/neuron/pkg/manifest"
	"github.com/ranscky/neuron/pkg/mcp"
	"github.com/ranscky/neuron/pkg/registry"
	"github.com/ranscky/neuron/pkg/runtime"
	"github.com/ranscky/neuron/pkg/secrets"
	"github.com/ranscky/neuron/pkg/ui"
	"github.com/ranscky/neuron/pkg/workflow"
	"github.com/spf13/cobra"
)

// Version is the CLI version. Later phases inject this at build time.
var Version = "0.1.0-dev"

var (
	// Initialize registry client with a base URL
	// In a real implementation, this would come from config
	registryClient = registry.NewRegistryClient("https://neuron-production-ae02.up.railway.app")

	// Initialize installer
	installerClient *installer.Installer

	// Initialize lockfile
	lockFile *lockfile.Lockfile

	// Command definitions
	rootCmd = &cobra.Command{
		Use:   "neuron",
		Short: "Neuron is a CLI-based distribution layer for AI tools, agents, and MCP servers",
		Long:  "Neuron handles versioning, dependencies, secrets, and isolated execution for AI tools.",
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

			// Create spinner for fetching
			s := ui.NewSpinner("Fetching " + name + "...")
			ui.StartSpinner(s)

			// Download the package
			s.Suffix = " Downloading..."
			_, err = registryClient.Fetch(name, resolvedVersion)
			if err != nil {
				ui.FailSpinner(s, fmt.Sprintf("Failed to fetch package %s@%s: %v", name, resolvedVersion, err))
				os.Exit(1)
			}

			// Install the package
			s.Suffix = " Installing..."
			err = installerClient.Install(name, resolvedVersion)
			if err != nil {
				ui.FailSpinner(s, fmt.Sprintf("Failed to install package %s@%s: %v", name, resolvedVersion, err))
				os.Exit(1)
			}

			ui.StopSpinner(s, "Installed "+name+"@"+resolvedVersion)
			ui.Step("Run it: neuron run " + name + " '{...}'")

			// If the package ships an MCP server definition, register it with
			// Neuron and push it to every detected client.
			homeDir, err := os.UserHomeDir()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to get home directory: %v\n", err)
				return
			}

			manifestPath := filepath.Join(homeDir, ".neuron", "packages", name, resolvedVersion, "neuron.json")
			pkgManifest, err := manifest.ParseManifest(manifestPath)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to parse manifest for package %s: %v\n", name, err)
				return
			}

			if pkgManifest.MCPServer != nil {
				store, err := mcp.NewStore()
				if err != nil {
					fmt.Fprintf(os.Stderr, "Failed to open Neuron MCP store: %v\n", err)
					return
				}
				spec := mcp.ServerSpec{
					Command: pkgManifest.MCPServer.Command,
					Args:    pkgManifest.MCPServer.Args,
					Env:     pkgManifest.MCPServer.Env,
				}
				if err := store.Add(name, spec); err != nil {
					fmt.Fprintf(os.Stderr, "Failed to register MCP server %s: %v\n", name, err)
					return
				}

				clients, err := mcp.DetectClients()
				if err != nil {
					fmt.Fprintf(os.Stderr, "Failed to detect MCP clients: %v\n", err)
					return
				}
				if len(clients) == 0 {
					fmt.Println("No MCP clients detected. Run `neuron mcp sync` once one is installed.")
					return
				}
				res, err := mcp.Sync(store, clients)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Failed to configure MCP clients: %v\n", err)
					return
				}
				ui.Success(fmt.Sprintf("Registered %s with %d MCP client(s)", name, res.Clients))
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
			ui.Info("Validating neuron.json...")

			// Validate manifest
			_, err := manifest.ParseManifest("neuron.json")
			if err != nil {
				ui.Error(fmt.Sprintf("Failed to validate neuron.json: %v", err))
				os.Exit(1)
			}

			// Create spinner for archiving
			s := ui.NewSpinner("Creating package archive...")
			ui.StartSpinner(s)

			// Publish package
			err = registry.PublishPackage(registryClient)
			if err != nil {
				ui.FailSpinner(s, fmt.Sprintf("Failed to publish package: %v", err))
				os.Exit(1)
			}

			ui.StopSpinner(s, "Successfully published package!")
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

			// Show info message
			ui.Info(fmt.Sprintf("Running %s@%s", packageName, version))

			// Create spinner for venv creation and dep install
			s := ui.NewSpinner("Preparing environment...")
			ui.StartSpinner(s)

			err = rt.Run(entryPoint, runArgs, env)
			if err != nil {
				ui.FailSpinner(s, fmt.Sprintf("Failed to run package %s: %v", packageName, err))
				os.Exit(1)
			}

			ui.StopSpinner(s, "Package executed successfully")
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

			// Create spinner for searching
			s := ui.NewSpinner("Searching...")
			ui.StartSpinner(s)

			// Search the registry
			results, err := registryClient.Search(query)
			if err != nil {
				ui.FailSpinner(s, fmt.Sprintf("Failed to search registry: %v", err))
				os.Exit(1)
			}

			// Stop spinner
			s.Stop()

			// Print results in a table format
			if len(results) == 0 {
				fmt.Println("No packages found.")
				return
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
			fmt.Fprintln(w, "NAME\tVERSION\tDESCRIPTION")
			for _, pkg := range results {
				// Print NAME in cyan, VERSION in yellow, DESCRIPTION in white
				fmt.Fprintf(w, "%s\t%s\t%s\n",
					color.CyanString(pkg.Name),
					color.YellowString(pkg.Version),
					pkg.Description)
			}
			w.Flush()
		},
	}

	// listCmd represents the list command
	listCmd = &cobra.Command{
		Use:   "list",
		Short: "List installed packages",
		Long:  `Call lockfile.List, read each package's manifest, and print a table with Name, Version, and Capability`,
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			// Get list of installed packages
			packages := lockFile.List()
			if len(packages) == 0 {
				fmt.Println("No packages installed.")
				return
			}

			// Get home directory to read manifests
			homeDir, err := os.UserHomeDir()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to get home directory: %v\n", err)
				os.Exit(1)
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
			fmt.Fprintln(w, "PACKAGE\tVERSION\tCAPABILITY")
			for name, version := range packages {
				// Read manifest to get capability
				manifestPath := filepath.Join(homeDir, ".neuron", "packages", name, version, "neuron.json")
				capability := "Unknown"
				if m, err := manifest.ParseManifest(manifestPath); err == nil {
					if m.Capability != nil {
						// For now, just show the output type as the capability summary
						capability = fmt.Sprintf("%s (%s)", m.Capability.Output.Type, m.Capability.Output.Format)
					} else {
						capability = "Generic"
					}
				}

				fmt.Fprintf(w, "%s\t%s\t%s\n", color.CyanString(name), color.YellowString(version), capability)
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
				ui.Error(fmt.Sprintf("Failed to set secret: %v", err))
				os.Exit(1)
			}

			ui.Success("Secret stored")
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
				ui.Error(fmt.Sprintf("Failed to get secret: %v", err))
				os.Exit(1)
			}

			// Print value in white
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
				ui.Error(fmt.Sprintf("Failed to load config: %v", err))
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
				ui.Error(fmt.Sprintf("Invalid config key: %s", key))
				os.Exit(1)
			}

			// Save the updated config
			if err := config.SaveConfig(cfg); err != nil {
				ui.Error(fmt.Sprintf("Failed to save config: %v", err))
				os.Exit(1)
			}

			ui.Success("Config updated")
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
				ui.Error(fmt.Sprintf("Failed to load config: %v", err))
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
				ui.Error(fmt.Sprintf("Invalid config key: %s", key))
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
				ui.Error(fmt.Sprintf("Failed to load config: %v", err))
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

			// Print the configuration with keys in cyan and values in white, mask API keys
			fmt.Printf("%s: %s\n", color.CyanString("Provider"), maskedCfg.Provider)
			fmt.Printf("%s: %s\n", color.CyanString("Ollama Base URL"), maskedCfg.Ollama.BaseURL)
			fmt.Printf("%s: %s\n", color.CyanString("OpenAI API Key"), maskedCfg.OpenAI.APIKey)
			fmt.Printf("%s: %s\n", color.CyanString("OpenAI Model"), maskedCfg.OpenAI.Model)
			fmt.Printf("%s: %s\n", color.CyanString("Anthropic API Key"), maskedCfg.Anthropic.APIKey)
			fmt.Printf("%s: %s\n", color.CyanString("Anthropic Model"), maskedCfg.Anthropic.Model)
			fmt.Printf("%s: %s\n", color.CyanString("Groq API Key"), maskedCfg.Groq.APIKey)
			fmt.Printf("%s: %s\n", color.CyanString("Groq Model"), maskedCfg.Groq.Model)
		},
	}

	// initCmd represents the init command
	initCmd = &cobra.Command{
		Use:   "init",
		Short: "Initialize Neuron configuration",
		Long:  `Initialize Neuron configuration with AI provider settings`,
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			// Print ASCII logo in cyan
			logo := `  ███╗   ██╗███████╗██╗   ██╗██████╗  ██████╗ ███╗   ██╗
	  ████╗  ██║██╔════╝██║   ██║██╔══██╗██╔═══██╗████╗  ██║
	  ██╔██╗ ██║█████╗  ██║   ██║██████╔╝██║   ██║██╔██╗ ██║
	  ██║╚██╗██║██╔══╝  ██║   ██║██╔══██╗██║   ██║██║╚██╗██║
	  ██║ ╚████║███████╗╚██████╔╝██║  ██║╚██████╔╝██║ ╚████║
	  ╚═╝  ╚═══╝╚══════╝ ╚═════╝ ╚═╝  ╚═╝ ╚═════╝ ╚═╝  ╚═══╝`

			fmt.Println(color.CyanString(logo))

			// Print tagline in white
			fmt.Println("The package manager for AI agents and MCP servers.")

			// Print empty line
			fmt.Println()

			// Print setup message
			fmt.Println("Let's get you set up. This takes about 30 seconds.")
			fmt.Println()

			// Provider selection
			providerOptions := []string{
				"Ollama (local, free)",
				"OpenAI",
				"Anthropic (Claude)",
				"Groq",
				"─────────────────────", // separator
				"OpenRouter [coming soon]",
				"Google Gemini [coming soon]",
				"Mistral [coming soon]",
				"Together AI [coming soon]",
			}

			var provider string
			for {
				prompt := &survey.Select{
					Message: "Select your AI provider",
					Options: providerOptions,
					Default: "Ollama (local, free)",
				}

				err := survey.AskOne(prompt, &provider)
				if err != nil {
					ui.Error(fmt.Sprintf("Failed to get provider selection: %v", err))
					os.Exit(1)
				}

				// Check if user selected a coming soon option
				if strings.Contains(provider, "[coming soon]") {
					ui.Warn("That provider is coming soon. Please select an available provider.")
					continue
				}
				break
			}

			// Create config
			cfg := config.DefaultConfig()

			// Provider-specific configuration
			switch provider {
			case "Ollama (local, free)":
				cfg.Provider = "ollama"

				// Get base URL
				baseURL := ""
				prompt := &survey.Input{
					Message: "Enter Ollama base URL",
					Default: "http://localhost:11434",
				}
				err := survey.AskOne(prompt, &baseURL)
				if err != nil {
					ui.Error(fmt.Sprintf("Failed to get base URL: %v", err))
					os.Exit(1)
				}
				cfg.Ollama.BaseURL = baseURL

				// Try to fetch models
				models, err := fetchOllamaModels(baseURL)
				if err != nil {
					ui.Warn("Ollama not running. Enter model name manually.")
					modelName := ""
					prompt := &survey.Input{
						Message: "Model name",
						Default: "qwen3-coder:480b-cloud",
					}
					err := survey.AskOne(prompt, &modelName)
					if err != nil {
						ui.Error(fmt.Sprintf("Failed to get model name: %v", err))
						os.Exit(1)
					}
					// For Ollama, we don't store the model in config, but we could if needed
				} else {
					// Select model from fetched models
					modelSelection := ""
					prompt := &survey.Select{
						Message: "Select default model",
						Options: models,
					}
					err := survey.AskOne(prompt, &modelSelection)
					if err != nil {
						ui.Error(fmt.Sprintf("Failed to get model selection: %v", err))
						os.Exit(1)
					}
					// For Ollama, we don't store the model in config, but we could if needed
				}

			case "OpenAI":
				cfg.Provider = "openai"

				// Get API key
				apiKey := ""
				prompt := &survey.Password{
					Message: "Enter OpenAI API key (sk-...)",
				}
				err := survey.AskOne(prompt, &apiKey)
				if err != nil {
					ui.Error(fmt.Sprintf("Failed to get API key: %v", err))
					os.Exit(1)
				}
				cfg.OpenAI.APIKey = apiKey

				// Get model
				model := ""
				prompt2 := &survey.Input{
					Message: "Model",
					Default: "gpt-4o",
				}
				err = survey.AskOne(prompt2, &model)
				if err != nil {
					ui.Error(fmt.Sprintf("Failed to get model: %v", err))
					os.Exit(1)
				}
				cfg.OpenAI.Model = model

			case "Anthropic (Claude)":
				cfg.Provider = "anthropic"

				// Get API key
				apiKey := ""
				prompt := &survey.Password{
					Message: "Enter Anthropic API key (sk-ant-...)",
				}
				err := survey.AskOne(prompt, &apiKey)
				if err != nil {
					ui.Error(fmt.Sprintf("Failed to get API key: %v", err))
					os.Exit(1)
				}
				cfg.Anthropic.APIKey = apiKey

				// Get model
				model := ""
				prompt2 := &survey.Input{
					Message: "Model",
					Default: "claude-sonnet-4-20250514",
				}
				err = survey.AskOne(prompt2, &model)
				if err != nil {
					ui.Error(fmt.Sprintf("Failed to get model: %v", err))
					os.Exit(1)
				}
				cfg.Anthropic.Model = model

			case "Groq":
				cfg.Provider = "groq"

				// Get API key
				apiKey := ""
				prompt := &survey.Password{
					Message: "Enter Groq API key",
				}
				err := survey.AskOne(prompt, &apiKey)
				if err != nil {
					ui.Error(fmt.Sprintf("Failed to get API key: %v", err))
					os.Exit(1)
				}
				cfg.Groq.APIKey = apiKey

				// Get model
				model := ""
				prompt2 := &survey.Input{
					Message: "Model",
					Default: "llama3-70b-8192",
				}
				err = survey.AskOne(prompt2, &model)
				if err != nil {
					ui.Error(fmt.Sprintf("Failed to get model: %v", err))
					os.Exit(1)
				}
				cfg.Groq.Model = model
			}

			// Save config
			err := config.SaveConfig(cfg)
			if err != nil {
				ui.Error(fmt.Sprintf("Failed to save config: %v", err))
				os.Exit(1)
			}

			ui.Success("Provider configured successfully")
		},
	}
)

	// fetchOllamaModels attempts to fetch models from Ollama API
	func fetchOllamaModels(baseURL string) ([]string, error) {
		// Make HTTP request to Ollama API
		resp, err := http.Get(baseURL + "/api/tags")
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()

		// Check if response is successful
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
		}

		// Read response body
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, err
		}

		// Parse JSON response
		var result map[string]interface{}
		if err := json.Unmarshal(body, &result); err != nil {
			return nil, err
		}

		// Extract models from response
		models, ok := result["models"].([]interface{})
		if !ok {
			return nil, fmt.Errorf("unexpected response format")
		}

		// Convert to string slice
		var modelNames []string
		for _, model := range models {
			if modelMap, ok := model.(map[string]interface{}); ok {
				if name, ok := modelMap["name"].(string); ok {
					modelNames = append(modelNames, name)
				}
			}
		}

		return modelNames, nil
	}

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
		lockFile, err = lockfile.NewLockfile()
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
		rootCmd.AddCommand(initCmd)

		// Add subcommands to secretsCmd
		secretsCmd.AddCommand(secretsSetCmd)
		secretsCmd.AddCommand(secretsGetCmd)

		secretsRmCmd := &cobra.Command{
			Use:   "rm <key>",
			Short: "Delete a secret from the OS keychain",
			Args:  cobra.ExactArgs(1),
			Run: func(cmd *cobra.Command, args []string) {
				store := secrets.NewStore()
				if err := store.Delete(args[0]); err != nil {
					ui.Error(fmt.Sprintf("Failed to delete secret: %v", err))
					os.Exit(1)
				}
				ui.Success("Secret deleted")
			},
		}

		secretsListCmd := &cobra.Command{
			Use:   "list",
			Short: "List secrets required by managed MCP servers",
			Args:  cobra.NoArgs,
			Run: func(cmd *cobra.Command, args []string) {
				mcpStore, err := mcp.NewStore()
				if err != nil {
					ui.Error(fmt.Sprintf("Failed to open MCP store: %v", err))
					os.Exit(1)
				}
				secretStore := secrets.NewStore()
				found := false
				for _, name := range mcpStore.Names() {
					spec := mcpStore.Servers[name]
					for envName, key := range spec.Secrets {
						found = true
						status := color.GreenString("set")
						if _, err := secretStore.Get(key); err != nil {
							status = color.RedString("missing")
						}
						fmt.Printf("%s (%s for %s) %s\n", color.CyanString(key), envName, name, status)
					}
				}
				if !found {
					fmt.Println("No secrets referenced by managed servers.")
				}
			},
		}
		secretsCmd.AddCommand(secretsRmCmd, secretsListCmd)

		// Add subcommands to configCmd
		configCmd.AddCommand(configSetCmd)
		configCmd.AddCommand(configGetCmd)
		configCmd.AddCommand(configShowCmd)

		// mcp — manage MCP servers across every detected client.
		mcpCmd := &cobra.Command{
			Use:   "mcp",
			Short: "Manage MCP servers across every AI client",
			Long:  `Install, sync, inspect and debug MCP servers across every detected AI client.`,
		}

		var (
			addCommand string
			addArgs    []string
			addEnv     []string
			addSecrets []string
			addURL     string
			addType    string
		)

		mcpAddCmd := &cobra.Command{
			Use:   "add <name>",
			Short: "Register a server and sync it to every client",
			Args:  cobra.ExactArgs(1),
			Run: func(cmd *cobra.Command, args []string) {
				name := args[0]

				spec := mcp.ServerSpec{
					Command: addCommand,
					Args:    addArgs,
					URL:     addURL,
					Type:    addType,
				}
				if len(addEnv) > 0 {
					spec.Env = map[string]string{}
				}
				for _, pair := range addEnv {
					k, v, ok := strings.Cut(pair, "=")
					if !ok {
						ui.Error(fmt.Sprintf("invalid --env %q, expected KEY=VALUE", pair))
						os.Exit(1)
					}
					spec.Env[k] = v
				}
				if len(addSecrets) > 0 {
					spec.Secrets = map[string]string{}
				}
				for _, pair := range addSecrets {
					envName, secretKey, ok := strings.Cut(pair, "=")
					if !ok {
						ui.Error(fmt.Sprintf("invalid --secret %q, expected ENV_NAME=SECRET_KEY", pair))
						os.Exit(1)
					}
					spec.Secrets[envName] = secretKey
				}
				if spec.Command == "" && spec.URL == "" {
					ui.Error("provide --command or --url")
					os.Exit(1)
				}

				store, err := mcp.NewStore()
				if err != nil {
					ui.Error(fmt.Sprintf("Failed to open MCP store: %v", err))
					os.Exit(1)
				}
				if err := store.Add(name, spec); err != nil {
					ui.Error(fmt.Sprintf("Failed to save server: %v", err))
					os.Exit(1)
				}

				clients, err := mcp.DetectClients()
				if err != nil {
					ui.Error(fmt.Sprintf("Failed to detect MCP clients: %v", err))
					os.Exit(1)
				}
				res, err := mcp.Sync(store, clients)
				if err != nil {
					ui.Error(fmt.Sprintf("Failed to sync clients: %v", err))
					os.Exit(1)
				}
				ui.Success(fmt.Sprintf("Added %s to %d client(s)", name, res.Clients))
				if spec.UsesSecrets() {
					ui.Step("Set required secrets with: neuron secrets set <KEY> <VALUE>")
				}
			},
		}
		mcpAddCmd.Flags().StringVar(&addCommand, "command", "", "command to launch the server")
		mcpAddCmd.Flags().StringArrayVar(&addArgs, "arg", nil, "argument for the command (repeatable)")
		mcpAddCmd.Flags().StringArrayVar(&addEnv, "env", nil, "non-secret environment variable, KEY=VALUE (repeatable)")
		mcpAddCmd.Flags().StringArrayVar(&addSecrets, "secret", nil, "keychain-backed env var, ENV_NAME=SECRET_KEY (repeatable)")
		mcpAddCmd.Flags().StringVar(&addURL, "url", "", "remote server URL")
		mcpAddCmd.Flags().StringVar(&addType, "type", "", "remote transport type, e.g. http")

		mcpRemoveCmd := &cobra.Command{
			Use:   "remove <name>",
			Short: "Remove a server from the store and every client",
			Args:  cobra.ExactArgs(1),
			Run: func(cmd *cobra.Command, args []string) {
				name := args[0]

				store, err := mcp.NewStore()
				if err != nil {
					ui.Error(fmt.Sprintf("Failed to open MCP store: %v", err))
					os.Exit(1)
				}
				clients, err := mcp.DetectClients()
				if err != nil {
					ui.Error(fmt.Sprintf("Failed to detect MCP clients: %v", err))
					os.Exit(1)
				}
				if err := mcp.Remove(store, clients, name); err != nil {
					ui.Error(fmt.Sprintf("Failed to remove %s: %v", name, err))
					os.Exit(1)
				}
				ui.Success(fmt.Sprintf("Removed %s from the store and %d client(s)", name, len(clients)))
			},
		}

		mcpListCmd := &cobra.Command{
			Use:   "list",
			Short: "List managed servers and detected clients",
			Args:  cobra.NoArgs,
			Run: func(cmd *cobra.Command, args []string) {
				store, err := mcp.NewStore()
				if err != nil {
					ui.Error(fmt.Sprintf("Failed to open MCP store: %v", err))
					os.Exit(1)
				}

				fmt.Printf("%s\n", color.CyanString("Managed servers"))
				names := store.Names()
				if len(names) == 0 {
					fmt.Println("  (none — add one with `neuron mcp add`)")
				}
				for _, name := range names {
					spec := store.Servers[name]
					target := spec.Command
					if spec.URL != "" {
						target = spec.URL
					}
					lock := ""
					if spec.UsesSecrets() {
						lock = " " + color.YellowString("[secrets]")
					}
					fmt.Printf("  %s → %s%s\n", color.CyanString(name), target, lock)
				}

				fmt.Printf("\n%s\n", color.CyanString("Detected clients"))
				clients, err := mcp.DetectClients()
				if err != nil {
					ui.Error(fmt.Sprintf("Failed to detect MCP clients: %v", err))
					os.Exit(1)
				}
				if len(clients) == 0 {
					fmt.Println("  (none detected)")
				}
				for _, client := range clients {
					fmt.Printf("  %s  %s\n", color.CyanString(client.Name), client.ConfigPath)
				}
			},
		}

		mcpSyncCmd := &cobra.Command{
			Use:   "sync",
			Short: "Push every managed server to every detected client",
			Args:  cobra.NoArgs,
			Run: func(cmd *cobra.Command, args []string) {
				store, err := mcp.NewStore()
				if err != nil {
					ui.Error(fmt.Sprintf("Failed to open MCP store: %v", err))
					os.Exit(1)
				}
				clients, err := mcp.DetectClients()
				if err != nil {
					ui.Error(fmt.Sprintf("Failed to detect MCP clients: %v", err))
					os.Exit(1)
				}
				res, err := mcp.Sync(store, clients)
				if err != nil {
					ui.Error(fmt.Sprintf("Sync failed: %v", err))
					os.Exit(1)
				}
				ui.Success(fmt.Sprintf("Synced %d server(s) to %d client(s)", res.Servers, res.Clients))
			},
		}

		mcpDoctorCmd := &cobra.Command{
			Use:   "doctor",
			Short: "Check clients, servers and secrets for problems",
			Args:  cobra.NoArgs,
			Run: func(cmd *cobra.Command, args []string) {
				store, err := mcp.NewStore()
				if err != nil {
					ui.Error(fmt.Sprintf("Failed to open MCP store: %v", err))
					os.Exit(1)
				}
				clients, err := mcp.DetectClients()
				if err != nil {
					ui.Error(fmt.Sprintf("Failed to detect MCP clients: %v", err))
					os.Exit(1)
				}

				secretStore := secrets.NewStore()
				diags := mcp.Doctor(
					store,
					clients,
					func(key string) bool {
						_, err := secretStore.Get(key)
						return err == nil
					},
					exec.LookPath,
				)

				problems := 0
				for _, d := range diags {
					switch d.Level {
					case "error":
						problems++
						ui.Error(fmt.Sprintf("%s: %s", d.Subject, d.Message))
					case "warn":
						ui.Warn(fmt.Sprintf("%s: %s", d.Subject, d.Message))
					default:
						fmt.Printf("%s %s: %s\n", color.GreenString("ok"), d.Subject, d.Message)
					}
				}
				if problems > 0 {
					os.Exit(1)
				}
			},
		}

		// Hidden launcher. Clients invoke this for servers that declare
		// secrets, so that secret values never appear in a client config.
		mcpRunCmd := &cobra.Command{
			Use:    "run <name>",
			Short:  "Launch a managed server with its secrets resolved",
			Args:   cobra.ExactArgs(1),
			Hidden: true,
			Run: func(cmd *cobra.Command, args []string) {
				store, err := mcp.NewStore()
				if err != nil {
					fmt.Fprintf(os.Stderr, "neuron: %v\n", err)
					os.Exit(1)
				}
				secretStore := secrets.NewStore()
				if err := mcp.RunServer(store, args[0], secretStore.Get, os.Stdin, os.Stdout, os.Stderr); err != nil {
					fmt.Fprintf(os.Stderr, "neuron: %v\n", err)
					os.Exit(1)
				}
			},
		}

		// Add mcp commands
		mcpCmd.AddCommand(mcpAddCmd, mcpRemoveCmd, mcpListCmd, mcpSyncCmd, mcpDoctorCmd, mcpRunCmd)
		rootCmd.AddCommand(mcpCmd)

		// runFlowCmd represents the run-flow command
		runFlowCmd := &cobra.Command{
			Use:   "run-flow <workflow_path> <query>",
			Short: "Run a defined workflow",
			Long:  `Parse workflow.json and execute steps in sequence, interpolating inputs and outputs`,
			Args:  cobra.ExactArgs(2),
			Run: func(cmd *cobra.Command, args []string) {
				workflowPath := args[0]
				userQuery := args[1]

				// Parse workflow
				wf, err := workflow.ParseWorkflow(workflowPath)
				if err != nil {
					ui.Error(fmt.Sprintf("Failed to parse workflow: %v", err))
					os.Exit(1)
				}

				// Initialize executor and runner
				secretStore := secrets.NewStore()
				inst, err := installer.NewInstaller()
				if err != nil {
					ui.Error(fmt.Sprintf("Failed to initialize installer: %v", err))
					os.Exit(1)
				}
				// We need the lockfile too
				lf, err := lockfile.NewLockfile()
				if err != nil {
					ui.Error(fmt.Sprintf("Failed to initialize lockfile: %v", err))
					os.Exit(1)
				}

				ex := executor.NewExecutor(inst, lf, secretStore)
				runner := workflow.NewRunner(ex)

				// Run the workflow
				userInputs := map[string]string{
					"user_query": userQuery,
				}

				if err := runner.Execute(wf, userInputs); err != nil {
					ui.Error(fmt.Sprintf("Workflow execution failed: %v", err))
					os.Exit(1)
				}
			},
		}

		// Add run-flow to root
		rootCmd.AddCommand(runFlowCmd)

		// version
		versionCmd := &cobra.Command{
			Use:   "version",
			Short: "Print the Neuron version",
			Args:  cobra.NoArgs,
			Run: func(cmd *cobra.Command, args []string) {
				fmt.Println(Version)
			},
		}
		rootCmd.AddCommand(versionCmd)
		rootCmd.Version = Version
	}

	func main() {
		// Execute the root command
		if err := rootCmd.Execute(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	}
