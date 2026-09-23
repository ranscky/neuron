package mcp

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDetectClientsFindsInstalledClients(t *testing.T) {
	home := t.TempDir()

	// Cursor is "installed" (its directory exists); Claude Code has a config
	// file; Windsurf is absent entirely.
	if err := os.MkdirAll(filepath.Join(home, ".cursor"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".claude.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := detectClients(KnownClients(), home, "linux", fileExists)

	ids := map[string]string{}
	for _, c := range got {
		ids[c.ID] = c.ConfigPath
	}
	if _, ok := ids["cursor"]; !ok {
		t.Error("expected cursor to be detected from its directory")
	}
	if _, ok := ids["claude-code"]; !ok {
		t.Error("expected claude-code to be detected from its file")
	}
	if _, ok := ids["windsurf"]; ok {
		t.Error("did not expect windsurf to be detected")
	}
}

func TestReadWriteConfigPreservesUnknownKeysAndNumbers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	// This integer is far beyond float64's exact range, so it only survives a
	// round-trip if we decode with json.Number.
	const bigNumber = "123456789012345678901234567890"
	original := `{
  "unknown": {"nested": [1, 2, 3]},
  "big": ` + bigNumber + `,
  "flag": true
}`
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := ReadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg["mcpServers"] = map[string]interface{}{
		"x": map[string]interface{}{"command": "echo"},
	}
	if err := WriteConfig(path, cfg); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"unknown"`) {
		t.Error("unknown key was dropped")
	}
	if !strings.Contains(string(raw), bigNumber) {
		t.Errorf("large number was reformatted; got %s", raw)
	}

	backup, err := os.ReadFile(path + ".neuron.bak")
	if err != nil {
		t.Fatalf("expected a backup of the original file: %v", err)
	}
	if string(backup) != original {
		t.Error("backup does not match the original file")
	}
}

func TestWriteConfigBacksUpOnlyOnce(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(`{"v":1}`), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := WriteConfig(path, map[string]interface{}{"v": 2}); err != nil {
		t.Fatal(err)
	}
	if err := WriteConfig(path, map[string]interface{}{"v": 3}); err != nil {
		t.Fatal(err)
	}

	backup, err := os.ReadFile(path + ".neuron.bak")
	if err != nil {
		t.Fatal(err)
	}
	if string(backup) != `{"v":1}` {
		t.Errorf("backup should preserve the pristine original, got %s", backup)
	}
}

func TestMaterializeWrapsSecretBackedServers(t *testing.T) {
	spec := ServerSpec{
		Command: "npx",
		Args:    []string{"-y", "@modelcontextprotocol/server-github"},
		Secrets: map[string]string{"GITHUB_TOKEN": "github-token"},
	}

	entry := Materialize("github", spec)

	if entry["command"] != "neuron" {
		t.Fatalf("expected secret-backed server to launch via neuron, got %v", entry["command"])
	}
	args, ok := entry["args"].([]string)
	if !ok || len(args) != 3 || args[0] != "mcp" || args[1] != "run" || args[2] != "github" {
		t.Fatalf("unexpected launcher args: %v", entry["args"])
	}
	if _, leaked := entry["env"]; leaked {
		t.Error("secret values must never be materialised into a client config")
	}
}

func TestMaterializePlainAndRemoteServers(t *testing.T) {
	plain := Materialize("fs", ServerSpec{Command: "npx", Args: []string{"-y", "server-fs"}})
	if plain["command"] != "npx" {
		t.Errorf("expected direct command, got %v", plain["command"])
	}

	remote := Materialize("api", ServerSpec{URL: "https://example.com/mcp", Type: "http"})
	if remote["url"] != "https://example.com/mcp" || remote["type"] != "http" {
		t.Errorf("unexpected remote entry: %v", remote)
	}
}

func TestSyncWritesEveryClientFormat(t *testing.T) {
	home := t.TempDir()
	store, err := newStoreAt(filepath.Join(home, ".neuron", "mcp", "servers.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Add("plain", ServerSpec{Command: "npx", Args: []string{"-y", "plain"}}); err != nil {
		t.Fatal(err)
	}

	clients := []Client{
		{ID: "cursor", Name: "Cursor", Format: FormatMCPServers, ConfigPath: filepath.Join(home, "cursor.json")},
		{ID: "vscode", Name: "VS Code", Format: FormatVSCodeServers, ConfigPath: filepath.Join(home, "vscode.json")},
		{ID: "zed", Name: "Zed", Format: FormatZedContext, ConfigPath: filepath.Join(home, "zed.json")},
	}

	res, err := Sync(store, clients)
	if err != nil {
		t.Fatal(err)
	}
	if res.Writes != 3 {
		t.Errorf("expected 3 writes, got %d", res.Writes)
	}

	assertServer := func(path, key string) map[string]interface{} {
		cfg, err := ReadConfig(path)
		if err != nil {
			t.Fatal(err)
		}
		servers, ok := cfg[key].(map[string]interface{})
		if !ok {
			t.Fatalf("%s: missing %q key", path, key)
		}
		entry, ok := servers["plain"].(map[string]interface{})
		if !ok {
			t.Fatalf("%s: server not written", path)
		}
		return entry
	}

	assertServer(clients[0].ConfigPath, "mcpServers")
	assertServer(clients[1].ConfigPath, "servers")
	zedEntry := assertServer(clients[2].ConfigPath, "context_servers")

	if zedEntry["source"] != "custom" {
		t.Errorf("expected zed entry source=custom, got %v", zedEntry["source"])
	}
	if zedEntry["command"] != "npx" {
		t.Errorf("expected zed entry to keep the command, got %v", zedEntry["command"])
	}
}

func TestSyncPreservesUserServersAndIdempotent(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "cursor.json")
	if err := os.WriteFile(configPath, []byte(`{"mcpServers":{"mine":{"command":"mine"}}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	store, err := newStoreAt(filepath.Join(home, "servers.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Add("managed", ServerSpec{Command: "npx"}); err != nil {
		t.Fatal(err)
	}

	clients := []Client{{ID: "cursor", Name: "Cursor", Format: FormatMCPServers, ConfigPath: configPath}}
	if _, err := Sync(store, clients); err != nil {
		t.Fatal(err)
	}
	if _, err := Sync(store, clients); err != nil {
		t.Fatal(err)
	}

	cfg, err := ReadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	servers := cfg["mcpServers"].(map[string]interface{})
	if _, ok := servers["mine"]; !ok {
		t.Error("sync must not remove servers the user added by hand")
	}
	if _, ok := servers["managed"]; !ok {
		t.Error("managed server was not written")
	}
}

func TestRemoveDeletesFromStoreAndClients(t *testing.T) {
	home := t.TempDir()
	store, err := newStoreAt(filepath.Join(home, "servers.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Add("gone", ServerSpec{Command: "npx"}); err != nil {
		t.Fatal(err)
	}

	configPath := filepath.Join(home, "cursor.json")
	clients := []Client{{ID: "cursor", Name: "Cursor", Format: FormatMCPServers, ConfigPath: configPath}}
	if _, err := Sync(store, clients); err != nil {
		t.Fatal(err)
	}
	if err := Remove(store, clients, "gone"); err != nil {
		t.Fatal(err)
	}

	if _, err := store.Get("gone"); err == nil {
		t.Error("server should be gone from the store")
	}
	cfg, err := ReadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	servers, _ := cfg["mcpServers"].(map[string]interface{})
	if _, ok := servers["gone"]; ok {
		t.Error("server should be gone from the client config")
	}
}

func TestStoreRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "servers.json")
	store, err := newStoreAt(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Add("a", ServerSpec{
		Command: "npx",
		Secrets: map[string]string{"TOKEN": "a-token"},
	}); err != nil {
		t.Fatal(err)
	}

	reloaded, err := newStoreAt(path)
	if err != nil {
		t.Fatal(err)
	}
	spec, err := reloaded.Get("a")
	if err != nil {
		t.Fatal(err)
	}
	if spec.Secrets["TOKEN"] != "a-token" {
		t.Errorf("secret reference did not survive the round trip: %v", spec.Secrets)
	}

	if names := reloaded.Names(); len(names) != 1 || names[0] != "a" {
		t.Errorf("unexpected names: %v", names)
	}
}

func TestRunServerInjectsSecrets(t *testing.T) {
	store, err := newStoreAt(filepath.Join(t.TempDir(), "servers.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Add("env-echo", ServerSpec{
		Command: "sh",
		Args:    []string{"-c", `printf %s "$MY_TOKEN"`},
		Secrets: map[string]string{"MY_TOKEN": "tok"},
	}); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	err = RunServer(store, "env-echo", func(key string) (string, error) {
		if key != "tok" {
			t.Errorf("unexpected secret key %q", key)
		}
		return "s3cr3t", nil
	}, nil, nil, &out, &out)
	if err != nil {
		t.Fatal(err)
	}
	if out.String() != "s3cr3t" {
		t.Errorf("expected the secret in the child environment, got %q", out.String())
	}
}

func TestDoctorReportsMissingSecretAndMissingCommand(t *testing.T) {
	store, err := newStoreAt(filepath.Join(t.TempDir(), "servers.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Add("needs-secret", ServerSpec{
		Command: "npx",
		Secrets: map[string]string{"TOKEN": "tok"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Add("missing-cmd", ServerSpec{Command: "definitely-not-installed-xyz"}); err != nil {
		t.Fatal(err)
	}

	diags := Doctor(
		store,
		nil,
		func(key string) bool { return false },
		func(file string) (string, error) { return "", os.ErrNotExist },
	)

	var missingSecret, missingCmd bool
	for _, d := range diags {
		if d.Level != "error" && d.Level != "warn" {
			continue
		}
		if d.Subject == "needs-secret" {
			missingSecret = true
		}
		if d.Subject == "missing-cmd" {
			missingCmd = true
		}
	}
	if !missingSecret {
		t.Error("expected a diagnostic for the unset secret")
	}
	if !missingCmd {
		t.Error("expected a diagnostic for the missing command")
	}
}
