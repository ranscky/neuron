package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// ServerSpec is Neuron's canonical definition of one MCP server. It is the
// single source of truth that gets materialised into every client config.
type ServerSpec struct {
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	URL     string            `json:"url,omitempty"`
	Type    string            `json:"type,omitempty"`
	// Secrets maps an environment variable name to a secret key in the OS
	// keychain. Secret *values* are never written to a client config. Servers
	// that declare secrets are exposed to clients through `neuron mcp run`,
	// which resolves them at launch.
	Secrets map[string]string `json:"secrets,omitempty"`
}

// UsesSecrets reports whether the server needs credentials at launch.
func (s ServerSpec) UsesSecrets() bool {
	return len(s.Secrets) > 0
}

// Store is Neuron's own registry of managed servers, persisted to disk. It is
// the desired state that Sync reconciles every client against.
type Store struct {
	path    string
	Servers map[string]ServerSpec
}

// NewStore opens the store at ~/.neuron/mcp/servers.json.
func NewStore() (*Store, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("cannot determine home directory: %w", err)
	}
	return newStoreAt(filepath.Join(home, ".neuron", "mcp", "servers.json"))
}

func newStoreAt(path string) (*Store, error) {
	s := &Store{path: path, Servers: map[string]ServerSpec{}}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, err
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return s, nil
	}
	if err := json.Unmarshal(data, &s.Servers); err != nil {
		return nil, fmt.Errorf("invalid %s: %w", path, err)
	}
	if s.Servers == nil {
		s.Servers = map[string]ServerSpec{}
	}
	return s, nil
}

// Save persists the store. It is written 0600 because it holds the names of
// the secrets a server needs, even though it never holds their values.
func (s *Store) Save() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s.Servers, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, append(data, '\n'), 0o600)
}

// Add inserts or replaces a server and persists the store.
func (s *Store) Add(name string, spec ServerSpec) error {
	s.Servers[name] = spec
	return s.Save()
}

// Remove deletes a server and persists the store.
func (s *Store) Remove(name string) error {
	delete(s.Servers, name)
	return s.Save()
}

// Get returns a server by name.
func (s *Store) Get(name string) (ServerSpec, error) {
	spec, ok := s.Servers[name]
	if !ok {
		return ServerSpec{}, fmt.Errorf("server %q is not managed by Neuron", name)
	}
	return spec, nil
}

// Names returns the managed server names in sorted order.
func (s *Store) Names() []string {
	names := make([]string, 0, len(s.Servers))
	for name := range s.Servers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
