package mcp

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// Format describes how a client stores MCP servers inside its JSON config.
type Format string

const (
	// FormatMCPServers is the common shape: {"mcpServers": {"<name>": {...}}}.
	FormatMCPServers Format = "mcpServers"
	// FormatVSCodeServers uses {"servers": {...}} instead of mcpServers.
	FormatVSCodeServers Format = "vscodeServers"
	// FormatZedContext uses {"context_servers": {...}} with a slightly
	// different per-server shape.
	FormatZedContext Format = "zedContext"
)

// Client is a detected MCP-capable application together with the config file
// Neuron should write to.
type Client struct {
	ID         string
	Name       string
	Format     Format
	ConfigPath string
}

// ClientDef describes a known client and how to locate its config file.
type ClientDef struct {
	ID     string
	Name   string
	Format Format
	// resolve returns candidate config paths in priority order. The first
	// existing file — or file inside an existing directory — wins.
	resolve func(home, goos string) []string
}

// KnownClients returns every client Neuron knows how to configure.
func KnownClients() []ClientDef {
	return []ClientDef{
		{
			ID: "claude-code", Name: "Claude Code", Format: FormatMCPServers,
			resolve: func(home, goos string) []string {
				return []string{filepath.Join(home, ".claude.json")}
			},
		},
		{
			ID: "claude-desktop", Name: "Claude Desktop", Format: FormatMCPServers,
			resolve: func(home, goos string) []string {
				switch goos {
				case "darwin":
					return []string{filepath.Join(home, "Library", "Application Support", "Claude", "claude_desktop_config.json")}
				case "windows":
					if appData := os.Getenv("APPDATA"); appData != "" {
						return []string{filepath.Join(appData, "Claude", "claude_desktop_config.json")}
					}
					return nil
				default:
					return []string{filepath.Join(home, ".config", "Claude", "claude_desktop_config.json")}
				}
			},
		},
		{
			ID: "cursor", Name: "Cursor", Format: FormatMCPServers,
			resolve: func(home, goos string) []string {
				return []string{filepath.Join(home, ".cursor", "mcp.json")}
			},
		},
		{
			ID: "cline", Name: "Cline", Format: FormatMCPServers,
			resolve: func(home, goos string) []string {
				switch goos {
				case "darwin":
					return []string{filepath.Join(home, "Library", "Application Support", "Code", "User", "globalStorage", "saoudrizwan.claude-dev", "settings", "cline_mcp_settings.json")}
				case "windows":
					if appData := os.Getenv("APPDATA"); appData != "" {
						return []string{filepath.Join(appData, "Code", "User", "globalStorage", "saoudrizwan.claude-dev", "settings", "cline_mcp_settings.json")}
					}
					return nil
				default:
					return []string{filepath.Join(home, ".config", "Code", "User", "globalStorage", "saoudrizwan.claude-dev", "settings", "cline_mcp_settings.json")}
				}
			},
		},
		{
			ID: "windsurf", Name: "Windsurf", Format: FormatMCPServers,
			resolve: func(home, goos string) []string {
				return []string{filepath.Join(home, ".codeium", "windsurf", "mcp_config.json")}
			},
		},
		{
			ID: "vscode", Name: "VS Code", Format: FormatVSCodeServers,
			resolve: func(home, goos string) []string {
				switch goos {
				case "darwin":
					return []string{filepath.Join(home, "Library", "Application Support", "Code", "User", "mcp.json")}
				case "windows":
					if appData := os.Getenv("APPDATA"); appData != "" {
						return []string{filepath.Join(appData, "Code", "User", "mcp.json")}
					}
					return nil
				default:
					return []string{filepath.Join(home, ".config", "Code", "User", "mcp.json")}
				}
			},
		},
		{
			ID: "zed", Name: "Zed", Format: FormatZedContext,
			resolve: func(home, goos string) []string {
				return []string{filepath.Join(home, ".config", "zed", "settings.json")}
			},
		},
	}
}

// DetectClients returns the clients present on this machine.
func DetectClients() ([]Client, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("cannot determine home directory: %w", err)
	}
	return detectClients(KnownClients(), home, runtime.GOOS, fileExists), nil
}

// detectClients is the testable core of detection.
func detectClients(defs []ClientDef, home, goos string, exists func(string) bool) []Client {
	var out []Client
	for _, def := range defs {
		for _, path := range def.resolve(home, goos) {
			if exists(path) || exists(filepath.Dir(path)) {
				out = append(out, Client{
					ID:         def.ID,
					Name:       def.Name,
					Format:     def.Format,
					ConfigPath: path,
				})
				break
			}
		}
	}
	return out
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
