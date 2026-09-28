---
layout: page
title: The neuron.json manifest
---

# The `neuron.json` manifest

Every Neuron package is defined by a `neuron.json` file at its root. This page
documents the fields the current parser reads.

```json
{
  "name": "agents/researcher",
  "version": "1.0.0",
  "description": "Research assistant that searches the web and synthesizes findings",
  "entry": "main.py",
  "runtime": "python",
  "capability": {
    "input": [
      { "name": "query", "type": "string", "required": true },
      { "name": "depth", "type": "integer", "required": false, "default": 3 }
    ],
    "output": { "type": "object", "format": "json" },
    "errors": ["rate_limited"]
  },
  "performance": {
    "avg_latency_ms": 3000,
    "p99_latency_ms": 8000,
    "success_rate": 0.95,
    "cost_per_call_usd": 0.005
  },
  "permissions": ["http", "env:TAVILY_API_KEY", "local:ollama"],
  "dependencies": { "tools/web-search": "^1.0.0" },
  "mcp_server": {
    "command": "python",
    "args": ["server.py"],
    "env": { "LOG_LEVEL": "info" }
  }
}
```

## Required fields

| Field | Type | Notes |
|---|---|---|
| `name` | string | Package name; `/` separates a namespace (`agents/researcher`) |
| `version` | string | SemVer; the registry resolves ranges like `^1.0.0` |
| `description` | string | One line, shown in search results |
| `entry` | string | Path to the executable entrypoint inside the package |
| `runtime` | string | `python` (Node.js is planned) |

A manifest missing any of these fails to parse with a `missing required field`
error.

## Optional fields

### `capability`

Strict typed inputs and outputs — the block that lets agents discover and
compose tools.

| Field | Type | Notes |
|---|---|---|
| `input[]` | array | Each entry: `name`, `type`, `required`; optional `mime[]`, `default` |
| `output` | object | `type` and `format` (e.g. `json`, `text`) |
| `errors` | string[] | Named error conditions the package can raise |

### `performance`

Advisory metrics used for ranking and display: `avg_latency_ms`,
`p99_latency_ms`, `success_rate`, `cost_per_call_usd`.

### `permissions`

An explicit allow-list. A package only receives the credentials it declares:

- `http` — outbound network access;
- `env:NAME` — the environment variable `NAME` is injected (e.g.
  `env:TAVILY_API_KEY`);
- `local:ollama` — access to a local Ollama instance.

Because provider credentials are gated on `env:` permissions, installing one
tool never exposes another tool's keys.

### `dependencies`

A map of package name to version range. When a package runs, Neuron installs
any dependency that is not already present.

### `models`

Model capability requirements: `capability` and `context` (tokens).

### `mcp`

Marks the package as MCP-compatible: `compatible` (bool) and `server` (string).

### `mcp_server`

If present, `neuron install` registers this definition as an MCP server and
syncs it to every client — see [Managing MCP servers](mcp.md).

| Field | Type |
|---|---|
| `command` | string |
| `args` | string[] |
| `env` | object of `KEY=VALUE` (non-secret) |

## A note on the `neuron` field

Older examples (including the README) include a `"neuron": "1"` field. The
current parser ignores unknown fields, so it is accepted but not read. Treat it
as reserved.
