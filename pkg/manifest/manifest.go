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

// CapabilityInput represents an input parameter for a capability
type CapabilityInput struct {
	Name     string      `json:"name"`
	Type     string      `json:"type"`
	Mime     []string    `json:"mime,omitempty"`
	Required bool        `json:"required"`
	Default  interface{} `json:"default,omitempty"`
}

// CapabilityOutput represents the output specification for a capability
type CapabilityOutput struct {
	Type   string `json:"type"`
	Format string `json:"format"`
}

// Capability represents a tool's capability including inputs, outputs, and possible errors
type Capability struct {
	Input  []CapabilityInput `json:"input,omitempty"`
	Output CapabilityOutput  `json:"output"`
	Errors []string          `json:"errors,omitempty"`
}

// Performance represents performance metrics for a tool
type Performance struct {
	AvgLatencyMs     int     `json:"avg_latency_ms"`
	P99LatencyMs     int     `json:"p99_latency_ms"`
	SuccessRate      float64 `json:"success_rate"`
	CostPerCallUsd   float64 `json:"cost_per_call_usd"`
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
	Capability   *Capability       `json:"capability,omitempty"`
	Performance  *Performance      `json:"performance,omitempty"`
}