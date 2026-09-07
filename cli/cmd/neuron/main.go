package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/fatih/color"
	"github.com/ranscky/neuron/internal/config"
	"github.com/ranscky/neuron/pkg/installer"
	"github.com/ranscky/neuron/pkg/manifest"
	"github.com/ranscky/neuron/pkg/registry"
	"github.com/ranscky/neuron/pkg/runtime"
	"github.com/ranscky/neuron/pkg/secrets"
	"github.com/ranscky/neuron/pkg/ui"
	"github.com/spf13/cobra"
)

var (
	registryClient  *registry.RegistryClient
	installerClient *installer.Installer
	lockFile        *installer.Lockfile
	rootCmd         = &cobra.Command{
		Use:   "neuron",
		Short: "Neuron is a CLI-based distribution layer for AI tools, agents, and MCP servers",
		Long:  "Neuron handles versioning, dependencies, secrets, and sandboxed execution for AI tools.",
	}
)

var installCmd = &cobra.Command{
	Use:   "install <package>",
	Short: "Install a tool from the registry",
	Long:  `Resolve version, download via RegistryClient.Fetch, install via Installer.Install`,
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		packageName := args[0]
		var name, constraint string
		if strings.Contains(packageName, "@") {
			parts := strings.Split(packageName, "@")
			name, constraint = parts[0], parts[1]
		} else {
			name = packageName
		}
		resolvedVersion, err := resolveVersionConstraint(registryClient, name, constraint)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to resolve version for package %s: %v\\n", name, err)
			os.Exit(1)
		}
		s := ui.NewSpinner("Fetching " + name + "...")
		ui.StartSpinner(s)
		s.Suffix = " Downloading..."
		_, err = registryClient.Fetch(name, resolvedVersion)
		if err != nil {
			ui.FailSpinner(s, fmt.Sprintf("Failed to fetch package %s@%s: %v", name, resolvedVersion, err))
			os.Exit(1)
		}
		s.Suffix = " Installing..."
		err = installerClient.Install(name, resolvedVersion)
		if err != nil {
			ui.FailSpinner(s, fmt.Sprintf("Failed to install package %s@%s: %v", name, resolvedVersion, err))
			os.Exit(1)
		}
		ui.StopSpinner(s, "Installed "+name+"@"+resolvedVersion)
	},
}

var publishCmd = &cobra.Command{
	Use:   "publish",
	Short: "Publish current directory as a Neuron package",
	Long:  `Validate neuron.json via ParseManifest, tar.gz current dir via PublishPackage, upload via RegistryClient.Publish`,
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		ui.Info("Validating neuron.json...")
		_, err := manifest.ParseManifest("neuron.json")
		if err != nil {
			ui.Error(fmt.Sprintf("Failed to validate neuron.json: %v", err))
			os.Exit(1)
		}
		s := ui.NewSpinner("Creating package archive...")
		ui.StartSpinner(s)
		err = registry.PublishPackage(registryClient)
		if err != nil {
			ui.FailSpinner(s, fmt.Sprintf("Failed to publish package: %v", err))
			os.Exit(1)
		}
		ui.StopSpinner(s, "Successfully published package!")
	},
}

var runCmd = &cobra.Command{
	Use:   "run <package> [args]",
	Short: "Run an installed tool",
	Long:  `Read lockfile to find installed path, parse its neuron.json, inject secrets via Injector, pick correct runtime, call runtime.Run`,
	Args:  cobra.MinimumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		packageName := args[0]
		runArgs := args[1:]
		version, err := lockFile.Get(packageName)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Package %s is not installed: %v\\n", packageName, err)
			os.Exit(1)
		}
		homeDir, _ := os.UserHomeDir()
		packagePath := fmt.Sprintf("%s/.neuron/packages/%s/%s", homeDir, packageName, version)
		manifestPath := fmt.Sprintf("%s/neuron.json", packagePath)
		pkgManifest, err := manifest.ParseManifest(manifestPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to parse manifest for package %s: %v\\n", packageName, err)
			os.Exit(1)
		}
		if pkgManifest.Dependencies != nil {
			for depName, depVersion := range pkgManifest.Dependencies {
				_, err := lockFile.Get(depName)
				if err != nil {
					resolvedDepVersion, err := resolveVersionConstraint(registryClient, depName, depVersion)
					if err != nil {
						fmt.Fprintf(os.Stderr, "Failed to resolve version for dependency %s: %v\\n", depName, err)
						os.Exit(1)
					}
					fmt.Printf("Installing dependency %s@%s...\\n", depName, resolvedDepVersion)
					_, err = registryClient.Fetch(depName, resolvedDepVersion)
					if err != nil {
						fmt.Fprintf(os.Stderr, "Failed to fetch dependency %s@%s: %v\\n", depName, resolvedDepVersion, err)
						os.Exit(1)
					}
					err = installerClient.Install(depName, resolvedDepVersion)
					if err != nil {
						fmt.Fprintf(os.Stderr, "Failed to install dependency %s@%s: %v\\n", depName, resolvedDepVersion, err)
						os.Exit(1)
					}
					fmt.Printf("Successfully installed dependency %s@%s\\n", depName, resolvedDepVersion)
				}
			}
		}
		secretStore := secrets.NewStore()
		injector := secrets.NewInjector(secretStore)
		env := make(map[string]string)
		err = injector.Inject(pkgManifest, env)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to inject secrets: %v\\n", err)
			os.Exit(1)
		}
		var rt runtime.Runtime
		switch pkgManifest.Runtime {
		case "python":
			rt = &runtime.PythonRuntime{}
		case "node":
			rt = &runtime.NodeRuntime{}
		case "binary":
			fmt.Fprintf(os.Stderr, "Binary runtime not fully implemented\\n")
			os.Exit(1)
		default:
			fmt.Fprintf(os.Stderr, "Unsupported runtime: %s\\n", pkgManifest.Runtime)
			os.Exit(1)
		}
		entryPoint := fmt.Sprintf("%s/%s", packagePath, pkgManifest.Entry)
		ui.Info(fmt.Sprintf("Running %s@%s", packageName, version))
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

var searchCmd = &cobra.Command{
	Use:   "search <query>",
	Short: "Search the registry",
	Long:  `Call RegistryClient.Search, print results as a table with Name, Version, Description columns`,
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		query := args[0]
		s := ui.NewSpinner("Searching...")
		ui.StartSpinner(s)
		results, err := registryClient.Search("public", query)
		if err != nil {
			ui.FailSpinner(s, fmt.Sprintf("Failed to search registry: %v", err))
			os.Exit(1)
		}
		s.Stop()
		if len(results) == 0 {
			fmt.Println("No packages found.")
			return
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
		fmt.Fprintln(w, "NAME\\tVERSION\\tDESCRIPTION")
		for _, pkg := range results {
			fmt.Fprintf(w, "%s\\t%s\\t%s\\n", color.CyanString(pkg.Name), color.YellowString(pkg.Version), pkg.Description)
		}
		w.Flush()
	},
}

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List installed packages",
	Long:  `Call lockfile.List, print installed packages and their versions`,
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		packages := lockFile.List()
		if len(packages) == 0 {
			fmt.Println("No packages installed.")
			return
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
		fmt.Fprintln(w, "PACKAGE\\tVERSION")
		for name, version := range packages {
			fmt.Fprintf(w, "%s\\t%s\\n", name, version)
		}
		w.Flush()
	},
}

var uninstallCmd = &cobra.Command{
	Use:   "uninstall <package>",
	Short: "Uninstall a package",
	Long:  `Remove package from ~/.neuron/packages/, remove venv from ~/.neuron/venv/, and remove entry from lockfile`,
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		packageName := args[0]
		homeDir, _ := os.UserHomeDir()
		packagesPath := filepath.Join(homeDir, ".neuron", "packages", packageName)
		if err := os.RemoveAll(packagesPath); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to remove package directory: %v\\n", err)
			os.Exit(1)
		}
		venvPath := filepath.Join(homeDir, ".neuron", "venv", packageName)
		if err := os.RemoveAll(venvPath); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to remove venv directory: %v\\n", err)
			os.Exit(1)
		}
		if err := lockFile.Remove(packageName); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to remove package from lockfile: %v\\n", err)
			os.Exit(1)
		}
		fmt.Printf("Successfully uninstalled %s\\n", packageName)
	},
}

var updateCmd = &cobra.Command{
	Use:   "update <package>",
	Short: "Update a package to the latest version",
	Long:  `Calls GetPackageInfo to get latest version, compares with lockfile version, if newer: installs new version, updates lockfile`,
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		packageName := args[0]
		currentVersion, err := lockFile.Get(packageName)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Package %s is not installed: %v\\n", packageName, err)
			os.Exit(1)
		}
		latestVersion, err := registryClient.GetLatest("public", packageName)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to get package info for %s: %v\\n", packageName, err)
			os.Exit(1)
		}
		if latestVersion != currentVersion {
			fmt.Printf("Updating %s from %s to %s...\\n", packageName, currentVersion, latestVersion)
			_, err = registryClient.Fetch(packageName, latestVersion)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to fetch package %s@%s: %v\\n", packageName, latestVersion, err)
				os.Exit(1)
			}
			err = installerClient.Install(packageName, latestVersion)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to install package %s@%s: %v\\n", packageName, latestVersion, err)
			}
			fmt.Printf("Updated %s to %s\\n", packageName, latestVersion)
		} else {
			fmt.Printf("Already at latest version (%s)\\n", currentVersion)
		}
	},
}

var loginCmd = &cobra.Command{
	Use:   "login <token>",
	Short: "Authenticate with the Neuron Registry",
	Long:  `Saves the registry API token to the local config file (~/.neuron/config.json)`,
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		token := args[0]
		cfg, err := config.LoadConfig()
		if err != nil {
			ui.Error(fmt.Sprintf("Failed to load config: %v", err))
			os.Exit(1)
		}
		cfg.RegistryToken = token
		if err := config.SaveConfig(cfg); err != nil {
			ui.Error(fmt.Sprintf("Failed to save config: %v", err))
			os.Exit(1)
		}
		ui.Success("Authenticated with Neuron Registry")
	},
}

func initialize() {
	cfg, err := config.LoadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\\n", err)
		os.Exit(1)
	}

	registryClient = registry.NewRegistryClient(
		"https://neuron-production-ae02.up.railway.app",
		cfg.RegistryToken,
		cfg.DefaultOrgID,
	)
	installerClient, _ = installer.NewInstaller()
	lockFile, _ = installer.NewLockfile()
}

func resolveVersionConstraint(client *registry.RegistryClient, name, constraint string) (string, error) {
	if constraint == "" {
		return client.GetLatest("public", name)
	}
	return constraint, nil
}

func main() {
	initialize()
	rootCmd.AddCommand(installCmd)
	rootCmd.AddCommand(publishCmd)
	rootCmd.AddCommand(runCmd)
	rootCmd.AddCommand(searchCmd)
	rootCmd.AddCommand(listCmd)
	rootCmd.AddCommand(uninstallCmd)
	rootCmd.AddCommand(updateCmd)
	rootCmd.AddCommand(loginCmd)
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to execute command: %v\\n", err)
		os.Exit(1)
	}
}
