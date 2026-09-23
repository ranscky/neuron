package mcp

// serversKey returns the JSON key a client uses to hold its server map.
func serversKey(f Format) string {
	switch f {
	case FormatVSCodeServers:
		return "servers"
	case FormatZedContext:
		return "context_servers"
	default:
		return "mcpServers"
	}
}

// Materialize renders a spec into the object a client config should contain.
//
// Servers that declare secrets, or that are explicitly wrapped, are exposed
// through the Neuron launcher (`neuron mcp run <name>`): secret values never
// touch a client config, and every call is recorded by the proxy.
func Materialize(name string, spec ServerSpec) map[string]interface{} {
	entry := map[string]interface{}{}

	if spec.UsesSecrets() || spec.Proxy {
		entry["command"] = "neuron"
		entry["args"] = []string{"mcp", "run", name}
		if len(spec.Env) > 0 {
			entry["env"] = spec.Env
		}
		return entry
	}

	if spec.URL != "" {
		entry["url"] = spec.URL
		if spec.Type != "" {
			entry["type"] = spec.Type
		}
		return entry
	}

	if spec.Command != "" {
		entry["command"] = spec.Command
	}
	if len(spec.Args) > 0 {
		entry["args"] = spec.Args
	}
	if len(spec.Env) > 0 {
		entry["env"] = spec.Env
	}
	return entry
}

// wrapEntry adapts a materialised entry to a client's per-server shape.
func wrapEntry(f Format, entry map[string]interface{}) map[string]interface{} {
	if f != FormatZedContext {
		return entry
	}
	wrapped := map[string]interface{}{"source": "custom"}
	for k, v := range entry {
		wrapped[k] = v
	}
	return wrapped
}

// SyncResult summarises a reconciliation.
type SyncResult struct {
	Clients int
	Servers int
	Writes  int
}

// Sync writes every managed server into every detected client, idempotently.
// It only ever adds or updates Neuron-managed entries; servers a user added by
// hand are preserved untouched.
func Sync(store *Store, clients []Client) (SyncResult, error) {
	res := SyncResult{Clients: len(clients), Servers: len(store.Servers)}

	for _, c := range clients {
		cfg, err := ReadConfig(c.ConfigPath)
		if err != nil {
			return res, err
		}

		key := serversKey(c.Format)
		servers, _ := cfg[key].(map[string]interface{})
		if servers == nil {
			servers = map[string]interface{}{}
		}

		for _, name := range store.Names() {
			servers[name] = wrapEntry(c.Format, Materialize(name, store.Servers[name]))
			res.Writes++
		}

		cfg[key] = servers
		if err := WriteConfig(c.ConfigPath, cfg); err != nil {
			return res, err
		}
	}

	return res, nil
}

// Remove deletes a managed server from the store and from every client.
func Remove(store *Store, clients []Client, name string) error {
	if _, err := store.Get(name); err != nil {
		return err
	}
	if err := store.Remove(name); err != nil {
		return err
	}

	for _, c := range clients {
		cfg, err := ReadConfig(c.ConfigPath)
		if err != nil {
			return err
		}
		key := serversKey(c.Format)
		servers, ok := cfg[key].(map[string]interface{})
		if !ok {
			continue
		}
		if _, present := servers[name]; !present {
			continue
		}
		delete(servers, name)
		cfg[key] = servers
		if err := WriteConfig(c.ConfigPath, cfg); err != nil {
			return err
		}
	}

	return nil
}
