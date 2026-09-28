---
layout: page
title: CLI reference
---

# CLI reference

```
neuron <command> [flags]
neuron --version
```

## Packages

| Command | Description |
|---|---|
| `neuron install <package>` | Install from the registry. Accepts a version constraint, e.g. `agents/researcher@^1.0.0` |
| `neuron run <package> [args]` | Run an installed package (installs missing dependencies first) |
| `neuron search <query>` | Search the registry |
| `neuron list` | List installed packages |
| `neuron update <package>` | Update a package to the latest version |
| `neuron uninstall <package>` | Uninstall a package |
| `neuron publish` | Publish the current directory as a package |
| `neuron run-flow <workflow> <query>` | Run a defined workflow |

## MCP servers

| Command | Description |
|---|---|
| `neuron mcp add <name>` | Register a server and sync it to every client |
| `neuron mcp remove <name>` | Remove it from the store and every client |
| `neuron mcp list` | List managed servers and detected clients |
| `neuron mcp sync` | Push every managed server to every detected client |
| `neuron mcp doctor` | Check clients, servers and secrets for problems |
| `neuron mcp run <name>` | Launch a server with its secrets resolved (used by clients) |
| `neuron mcp wrap <name>` | Route a server through the recording proxy |
| `neuron mcp unwrap <name>` | Stop routing a server through the proxy |

`neuron mcp add` flags:

| Flag | Description |
|---|---|
| `--command` | Command to launch the server |
| `--arg` | Argument for the command (repeatable) |
| `--env` | Non-secret environment variable, `KEY=VALUE` (repeatable) |
| `--secret` | Keychain-backed env var, `ENV_NAME=SECRET_KEY` (repeatable) |
| `--url` | Remote server URL |
| `--type` | Remote transport type, e.g. `http` |

## Secrets

| Command | Description |
|---|---|
| `neuron secrets set <key> <value>` | Store a secret in the OS keychain |
| `neuron secrets get <key>` | Read a secret |
| `neuron secrets rm <key>` | Delete a secret |
| `neuron secrets list` | List secrets required by managed servers |

## Dashboard

| Command | Description |
|---|---|
| `neuron ui` | Serve the local activity dashboard (default `http://127.0.0.1:7717`) |

| Flag | Default | Description |
|---|---|---|
| `--port` | `7717` | Port to serve the dashboard on |

## Sync

| Command | Description |
|---|---|
| `neuron login` | Sign in to a sync service via device flow |
| `neuron logout` | Sign out |
| `neuron sync` | Sync MCP servers and secrets to your other machines |

`neuron login` flags:

| Flag | Description |
|---|---|
| `--server` | Sync service base URL |
| `--account` | Existing account id to sign in to |
| `--approve` | Approve a pending user code instead of signing in |
| `--auto-approve` | Approve this device automatically (self-hosted convenience) |

`neuron sync` flags:

| Flag | Description |
|---|---|
| `--passphrase` | Sync passphrase (or set `NEURON_PASSPHRASE`) |

## Teams

| Command | Description |
|---|---|
| `neuron team create <name>` | Create a team and print its invite code |
| `neuron team join <invite-code>` | Join a team |
| `neuron team list` | List teams known to this machine |
| `neuron team sync <team>` | Sync MCP servers with a team |

`create`, `join` and `sync` accept `--passphrase` (or `NEURON_TEAM_PASSPHRASE`).

## Configuration

| Command | Description |
|---|---|
| `neuron init` | Initialize Neuron configuration (interactive) |
| `neuron config set <key> <value>` | Set a configuration value |
| `neuron config get <key>` | Get a configuration value |
| `neuron config show` | Show all configuration values |
| `neuron version` | Print the version |

## Environment variables

| Variable | Purpose |
|---|---|
| `NEURON_REGISTRY_URL` | Registry to install from / publish to |
| `NEURON_PROVIDER` | Provider for package execution (`openai`, `anthropic`, `groq`, `ollama`) |
| `NEURON_MODEL` | Model name |
| `NEURON_OPENAI_API_KEY` / `NEURON_ANTHROPIC_API_KEY` / `NEURON_GROQ_API_KEY` | Provider credentials |
| `NEURON_OLLAMA_BASE_URL` | Local Ollama endpoint |
| `NEURON_SYNC_SERVER` / `NEURON_SYNC_TOKEN` | Sync service URL / session token |
| `NEURON_PASSPHRASE` | Personal sync passphrase |
| `NEURON_TEAM_PASSPHRASE` | Team sync passphrase |
| `NEURON_REQUIRE_PRO` | Set `false` on a self-hosted service to lift the paid gate |
| `PORT` | Registry / cloud service listen port |
| `NEURON_CLOUD_DATA` | Cloud service data file |
