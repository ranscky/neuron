package manifest

// Model represents a model capability requirement
type Model struct {
	Capability string `json:"capability"`
	Context    int    `json:"context"`
}

// MCPConfig represents MCP server configuration
type MCPConfig struct {
	Compatible bool   `json:"compatible,omitempty"`
	Server     string `json:"server,omitempty"`
}

// Manifest represents the neuron.json structure
type Manifest struct {
	Name         string            `json:"name"`
	Version      string            `json:"version"`
	Description  string            `json:"description"`
	Entry        string            `json:"entry"`
	Runtime      string            `json:"runtime"`
	Permissions  []string          `json:"permissions,omitempty"`
	Models       []Model           `json:"models,omitempty"`
	Dependencies map[string]string `json:"dependencies,omitempty"`
	MCP          MCPConfig         `json:"mcp,omitempty"`
}