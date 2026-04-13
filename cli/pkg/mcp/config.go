package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// MCPClient represents an MCP-compatible AI client
type MCPClient struct {
	Name       string
	ConfigPath string
	Format     string // "cline", "claude_code", "windsurf"
}

// Known client config paths
var clientConfigs = []MCPClient{
	{
		Name:       "Cline",
		ConfigPath: filepath.Join("~", ".config", "Code", "User", "globalStorage", "saoudrizwan.claude-dev", "settings", "cline_mcp_settings.json"),
		Format:     "cline",
	},
	{
		Name:       "Claude Code",
		ConfigPath: filepath.Join("~", ".claude.json"),
		Format:     "claude_code",
	},
	{
		Name:       "Windsurf",
		ConfigPath: filepath.Join("~", ".config", "Windsurf", "User", "settings.json"),
		Format:     "windsurf",
	},
}

// expandHome expands the tilde (~) in a path to the user's home directory
func expandHome(path string) (string, error) {
	if len(path) == 0 || path[0] != '~' {
		return path, nil
	}
	
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	
	return filepath.Join(home, path[1:]), nil
}

// DetectClients checks if each config file exists and returns only the ones found on disk
func DetectClients() ([]MCPClient, error) {
	var detected []MCPClient
	
	for _, client := range clientConfigs {
		expandedPath, err := expandHome(client.ConfigPath)
		if err != nil {
			continue // Skip this client if we can't expand the path
		}
		
		if _, err := os.Stat(expandedPath); err == nil {
			// File exists, create a copy with expanded path
			detectedClient := MCPClient{
				Name:       client.Name,
				ConfigPath: expandedPath,
				Format:     client.Format,
			}
			detected = append(detected, detectedClient)
		}
	}
	
	return detected, nil
}

// InjectMCPServer reads the existing JSON config file and adds the MCP server configuration
func InjectMCPServer(client MCPClient, serverName string, serverConfig map[string]interface{}) error {
	// Read existing config or create new if it doesn't exist
	configData, err := ReadConfigFile(client.ConfigPath)
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}
	
	// Add the server configuration based on client format
	switch client.Format {
	case "cline", "windsurf":
		// Add entry under "mcpServers"
		if configData["mcpServers"] == nil {
			configData["mcpServers"] = make(map[string]interface{})
		}
		mcpServers := configData["mcpServers"].(map[string]interface{})
		mcpServers[serverName] = serverConfig
		
	case "claude_code":
		// Add entry under "projects"[first_project]["mcpServers"] OR at root "mcpServers" if no projects exist
		if projects, ok := configData["projects"].(map[string]interface{}); ok && len(projects) > 0 {
			// Get first project (we'll just take the first key)
			for projectName, projectData := range projects {
				projectMap, isMap := projectData.(map[string]interface{})
				if !isMap {
					continue
				}
				
				if projectMap["mcpServers"] == nil {
					projectMap["mcpServers"] = make(map[string]interface{})
				}
				mcpServers := projectMap["mcpServers"].(map[string]interface{})
				mcpServers[serverName] = serverConfig
				
				// Update the project in the config
				projects[projectName] = projectMap
				break
			}
		} else {
			// No projects exist, add at root level
			if configData["mcpServers"] == nil {
				configData["mcpServers"] = make(map[string]interface{})
			}
			mcpServers := configData["mcpServers"].(map[string]interface{})
			mcpServers[serverName] = serverConfig
		}
	}
	
	// Write back the updated JSON with 2 space indent
	return writeConfigFile(client.ConfigPath, configData)
}

// RemoveMCPServer reads config, removes the named server, and writes back
func RemoveMCPServer(client MCPClient, serverName string) error {
	// Read existing config
	configData, err := ReadConfigFile(client.ConfigPath)
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}
	
	// Remove the server configuration based on client format
	switch client.Format {
	case "cline", "windsurf":
		// Remove entry from "mcpServers"
		if mcpServers, ok := configData["mcpServers"].(map[string]interface{}); ok {
			delete(mcpServers, serverName)
		}
		
	case "claude_code":
		// Remove entry from "projects"[first_project]["mcpServers"] OR at root "mcpServers"
		if projects, ok := configData["projects"].(map[string]interface{}); ok && len(projects) > 0 {
			// Get first project
			for projectName, projectData := range projects {
				projectMap, isMap := projectData.(map[string]interface{})
				if !isMap {
					continue
				}
				
				if mcpServers, ok := projectMap["mcpServers"].(map[string]interface{}); ok {
					delete(mcpServers, serverName)
					// Update the project in the config
					projects[projectName] = projectMap
				}
				break
			}
		} else {
			// No projects exist, remove from root level
			if mcpServers, ok := configData["mcpServers"].(map[string]interface{}); ok {
				delete(mcpServers, serverName)
			}
		}
	}
	
	// Write back the updated JSON
	return writeConfigFile(client.ConfigPath, configData)
}

// ReadConfigFile reads a JSON config file, returning an empty map if the file doesn't exist
func ReadConfigFile(path string) (map[string]interface{}, error) {
	// If file doesn't exist, return empty config
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return make(map[string]interface{}), nil
	}
	
	// Read the file
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	
	// Parse JSON
	var config map[string]interface{}
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, err
	}
	
	// If config is nil, return empty map
	if config == nil {
		config = make(map[string]interface{})
	}
	
	return config, nil
}

// writeConfigFile writes the config data to a JSON file with 2-space indentation
func writeConfigFile(path string, config map[string]interface{}) error {
	// Create directory if it doesn't exist
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	
	// Marshal with 2-space indentation
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	
	// Write to file
	return os.WriteFile(path, data, 0644)
}