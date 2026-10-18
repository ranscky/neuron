---
layout: page
title: Managing MCP servers
---

# Managing MCP servers

Neuron keeps its own list of servers in `~/.neuron/mcp/servers.json` and pushes
that list into whichever clients it finds on the machine. The store is the
source of truth; client configs are a projection of it.

## Commands

| Command | What it does |
|---|---|
| `neuron mcp add <name> …` | Register a server and sync it to every client |
| `neuron mcp remove <name>` | Delete it from the store and every client |
| `neuron mcp list` | Show managed servers and detected clients |
| `neuron mcp sync` | Push everything to every detected client again |
| `neuron mcp doctor` | Validate configs, secrets and commands |
| `neuron mcp run <name>` | Launch a server with its secrets resolved (used by clients) |
| `neuron mcp wrap <name>` | Route the server through the recording proxy |
| `neuron mcp unwrap <name>` | Stop routing it through the proxy |

### `neuron mcp add` flags

| Flag | Meaning |
|---|---|
| `--command` | Command to launch the server (stdio servers) |
| `--arg` | Argument for the command; repeatable |
| `--env` | Non-secret environment variable, `KEY=VALUE`; repeatable |
| `--secret` | Keychain-backed env var, `ENV_NAME=SECRET_KEY`; repeatable |
| `--url` | Remote server URL |
| `--type` | Remote transport type, e.g. `http`, `sse` |

## Remote servers

A server registered with `--url` is reached over HTTP instead of being spawned.
`neuron mcp run <name>` proxies it through the same session as a local server, so
**policy and recording apply identically** — a denied tool never leaves the
machine, and every call lands in the history.

```bash
neuron mcp add linear --url https://mcp.linear.app/mcp --type http
```

Two transports are supported:

| `--type` | How it works |
|---|---|
| `http` (default) | Each client message is POSTed to the URL. The response may be one JSON message or an SSE stream. An `Mcp-Session-Id` response header is echoed on later requests. |
| `sse` | A GET stream announces an `endpoint` event naming where to POST; every response arrives on that stream. |

When the transport itself fails — the host is unreachable, or the server answers
with an error status — the waiting request is answered with a JSON-RPC `-32000`
error instead of hanging, and the failure is recorded like any other.

What is *not* covered: Neuron does not host remote servers. `cloud/` stores
encrypted configuration; running a shared MCP server on the service is still to
come. A remote server's credentials are also not injected by Neuron — they belong
to the endpoint, so put them in that endpoint's own configuration.

## Supported clients

Neuron detects these clients and writes to the config file each one uses. It
handles three config *formats*, so unknown keys are preserved and fields it does
not own are never reformatted.

| Client | Config file |
|---|---|
| Claude Code | `~/.claude.json` |
| Claude Desktop | `~/Library/Application Support/Claude/claude_desktop_config.json` (macOS), `%APPDATA%\Claude\claude_desktop_config.json` (Windows), `~/.config/Claude/claude_desktop_config.json` (Linux) |
| Cursor | `~/.cursor/mcp.json` |
| Cline | VS Code `globalStorage/saoudrizwan.claude-dev/settings/cline_mcp_settings.json` |
| Windsurf | `~/.codeium/windsurf/mcp_config.json` |
| VS Code | `<User>/mcp.json` (uses the `servers` key) |
| Zed | `~/.config/zed/settings.json` (uses `context_servers`) |

## What Neuron guarantees when it writes config

- **Atomic writes.** Every config is written to a temp file, `fsync`'d, then
  renamed into place — a crash mid-write cannot leave a truncated config.
- **One-time backup.** Before the first edit of a file, Neuron saves
  `.neuron.bak` next to it. It does not overwrite an existing backup.
- **Nothing it doesn't own changes.** Unknown keys are preserved verbatim,
  including numbers.
- **No secret values on disk.** Any server with a secret is exposed to clients
  as `neuron mcp run <name>`; the value is resolved from the keychain at launch.

## Troubleshooting

```bash
neuron mcp doctor
```

`doctor` checks every managed server against every detected client and reports:

- a secret referenced by a server but not present in the keychain;
- a `--command` that is not on `PATH`;
- a config file that cannot be parsed.

A common first-run symptom is a client showing a server as failed: run `doctor`,
set the missing secret with `neuron secrets set`, then `neuron mcp sync`.
