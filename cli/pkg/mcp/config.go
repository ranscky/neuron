package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// ReadConfig reads a client config, preserving unknown keys and the exact
// representation of numbers. A missing or empty file yields an empty config.
func ReadConfig(path string) (map[string]interface{}, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]interface{}{}, nil
		}
		return nil, err
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return map[string]interface{}{}, nil
	}

	dec := json.NewDecoder(bytes.NewReader(data))
	// UseNumber keeps numbers byte-identical on round-trip, so we never
	// reformat fields we do not understand in someone else's config.
	dec.UseNumber()

	var cfg map[string]interface{}
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("invalid JSON in %s: %w", path, err)
	}
	if cfg == nil {
		cfg = map[string]interface{}{}
	}
	return cfg, nil
}

// WriteConfig writes a client config atomically and keeps a one-time backup of
// the original file at <path>.neuron.bak.
func WriteConfig(path string, cfg map[string]interface{}) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	// Preserve the original file exactly once, before Neuron ever edits it.
	if _, err := os.Stat(path); err == nil {
		backup := path + ".neuron.bak"
		if _, statErr := os.Stat(backup); os.IsNotExist(statErr) {
			if err := copyFile(path, backup); err != nil {
				return fmt.Errorf("failed to back up %s: %w", path, err)
			}
		}
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), ".neuron-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	return os.Rename(tmpName, path)
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o644)
}
