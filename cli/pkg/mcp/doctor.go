package mcp

import (
	"fmt"
	"sort"
)

// Diagnostic is a single finding from Doctor.
type Diagnostic struct {
	Level   string // "ok", "warn", "error"
	Subject string
	Message string
}

// Doctor checks managed servers and detected clients without changing
// anything. lookups are injected so the logic stays testable.
func Doctor(
	store *Store,
	clients []Client,
	secretExists func(key string) bool,
	lookPath func(file string) (string, error),
) []Diagnostic {
	var diags []Diagnostic

	if len(clients) == 0 {
		diags = append(diags, Diagnostic{Level: "warn", Subject: "clients", Message: "no MCP clients detected"})
	}
	for _, c := range clients {
		if _, err := ReadConfig(c.ConfigPath); err != nil {
			diags = append(diags, Diagnostic{Level: "error", Subject: c.Name, Message: err.Error()})
			continue
		}
		diags = append(diags, Diagnostic{Level: "ok", Subject: c.Name, Message: c.ConfigPath})
	}

	if len(store.Servers) == 0 {
		diags = append(diags, Diagnostic{Level: "warn", Subject: "servers", Message: "no servers managed by Neuron yet"})
	}

	for _, name := range store.Names() {
		spec := store.Servers[name]

		for _, envName := range sortedKeys(spec.Secrets) {
			secretKey := spec.Secrets[envName]
			if !secretExists(secretKey) {
				diags = append(diags, Diagnostic{
					Level:   "error",
					Subject: name,
					Message: fmt.Sprintf("secret %q (for %s) is not set", secretKey, envName),
				})
			}
		}

		// Secret-backed servers launch through Neuron; remote servers have no
		// local command to check.
		if spec.UsesSecrets() || spec.URL != "" || spec.Command == "" {
			continue
		}
		if _, err := lookPath(spec.Command); err != nil {
			diags = append(diags, Diagnostic{
				Level:   "warn",
				Subject: name,
				Message: fmt.Sprintf("command %q not found on PATH", spec.Command),
			})
		}
	}

	return diags
}

// sortedKeys returns map keys in deterministic order.
func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
