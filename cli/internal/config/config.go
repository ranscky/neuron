package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// OllamaConfig represents Ollama provider configuration
type OllamaConfig struct {
	BaseURL string `json:"base_url"`
}

// OpenAIConfig represents OpenAI provider configuration
type OpenAIConfig struct {
	APIKey string `json:"api_key"`
	Model  string `json:"model"`
}

// AnthropicConfig represents Anthropic provider configuration
type AnthropicConfig struct {
	APIKey string `json:"api_key"`
	Model  string `json:"model"`
}

// GroqConfig represents Groq provider configuration
type GroqConfig struct {
	APIKey string `json:"api_key"`
	Model  string `json:"model"`
}

// Config represents the Neuron CLI configuration
type Config struct {
	Provider  string         `json:"provider"`
	Ollama    OllamaConfig   `json:"ollama"`
	OpenAI    OpenAIConfig   `json:"openai"`
	Anthropic AnthropicConfig `json:"anthropic"`
	Groq      GroqConfig     `json:"groq"`
}

// LoadConfig reads configuration from ~/.neuron/config.json
func LoadConfig() (*Config, error) {
	configPath, err := getConfigPath()
	if err != nil {
		return nil, fmt.Errorf("failed to get config path: %w", err)
	}

	// Check if config file exists
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		// Return default config if file doesn't exist
		return DefaultConfig(), nil
	}

	// Read config file
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// Parse JSON
	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	return &config, nil
}

// SaveConfig writes configuration to ~/.neuron/config.json
func SaveConfig(c *Config) error {
	configPath, err := getConfigPath()
	if err != nil {
		return fmt.Errorf("failed to get config path: %w", err)
	}

	// Create directory if it doesn't exist
	dir := filepath.Dir(configPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	// Convert to JSON
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	// Write file
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// DefaultConfig returns the default configuration
func DefaultConfig() *Config {
	return &Config{
		Provider: "ollama",
		Ollama: OllamaConfig{
			BaseURL: "http://localhost:11434",
		},
		OpenAI: OpenAIConfig{
			APIKey: "",
			Model:  "gpt-4o",
		},
		Anthropic: AnthropicConfig{
			APIKey: "",
			Model:  "claude-sonnet-4-20250514",
		},
		Groq: GroqConfig{
			APIKey: "",
			Model:  "llama3-70b-8192",
		},
	}
}

// getConfigPath returns the path to the config file
func getConfigPath() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}
	return filepath.Join(homeDir, ".neuron", "config.json"), nil
}