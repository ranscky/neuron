# Neuron — Architecture

## What is Neuron?

Neuron is a CLI that does two related jobs:

1. **A cross-client control plane for MCP servers.** Register a server once;
   Neuron writes it to every AI client it detects (Claude Code, Claude Desktop,
   Cursor, Cline, Windsurf, VS Code, Zed), keeps credentials in the OS keychain,
   and can proxy calls so you can see them.
2. **A package manager** for distributing agents, tools and models through a
   registry, with version resolution, dependencies and lockfiles.

Think npm for AI tools, plus a single place to configure and debug the MCP
servers those tools expose.

See [`../docs/`](../docs/index.md) for user-facing documentation.

---

## Command surface

```bash
# packages
neuron install <package>          # install from the registry (resolves the version)
neuron run <package> [args]       # run an installed package
neuron search <query>             # search the registry
neuron list                       # list installed packages
neuron update <package>           # update a package
neuron uninstall <package>        # uninstall a package
neuron publish                    # publish the current directory as a package
neuron run-flow <workflow> <query># run a defined workflow

# MCP servers
neuron mcp add|remove|list|sync|doctor
neuron mcp run <name>             # launch with secrets resolved (used by clients)
neuron mcp wrap|unwrap <name>     # route through the recording proxy

# secrets, dashboard, sync, config
neuron secrets set|get|rm|list
neuron ui                         # local activity dashboard
neuron login|logout|sync
neuron team create|join|list|sync
neuron config set|get|show
neuron init
neuron version
```

Full flags and environment variables: [`../docs/cli-reference.md`](../docs/cli-reference.md).

---

## The manifest: neuron.json

Every Neuron package has a `neuron.json` at its root. All tooling is built
around this file. Required fields are enforced by the parser
(`pkg/manifest/parser.go`): missing any of `name`, `version`, `description`,
`entry`, `runtime` fails the parse.

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
  "models": [{ "capability": "tool_use", "context": 32000 }],
  "dependencies": { "tools/web-search": "^1.0.0" },
  "mcp": { "compatible": true, "server": "server.py" },
  "mcp_server": { "command": "python", "args": ["server.py"] }
}
```

### Manifest fields

| Field | Required | Description |
|---|---|---|
| `name` | yes | Package name; `/` separates a namespace (`agents/researcher`) |
| `version` | yes | Semver version string |
| `description` | yes | Short description shown in search |
| `entry` | yes | Entry point file to execute |
| `runtime` | yes | `python` or `node` (`binary` is accepted but not implemented) |
| `permissions` | no | Declared permissions. **Only `env:` is enforced** — see below |
| `capability` | no | Typed `input[]` / `output` / `errors[]` for composition |
| `performance` | no | Advisory `avg_latency_ms`, `p99_latency_ms`, `success_rate`, `cost_per_call_usd` |
| `models` | no | Model capability requirements (provider-agnostic) |
| `dependencies` | no | Other Neuron packages this depends on |
| `mcp` | no | `compatible` flag and `server` entry point |
| `mcp_server` | no | `command` / `args` / `env`; `neuron install` registers it as an MCP server |

Unknown fields (such as the legacy `"neuron": "1"`) are ignored, not rejected.

---

## Project structure

```
cli/
├── cmd/neuron/main.go         # CLI entry point, cobra commands
├── pkg/
│   ├── mcp/                   # cross-client MCP management
│   │   ├── client.go          #   client registry: config paths + formats
│   │   ├── config.go          #   atomic reads/writes, backup, key preservation
│   │   ├── store.go           #   Neuron's server store (~/.neuron/mcp/servers.json)
│   │   ├── sync.go            #   materialise the store into every client
│   │   ├── run.go             #   launch a server with secrets resolved
│   │   └── doctor.go          #   validate configs, secrets and commands
│   ├── manifest/              # neuron.json struct, parser, validation
│   ├── registry/              # registry HTTP client, semver resolution, publish
│   ├── installer/             # download and install packages
│   ├── lockfile/              # ~/.neuron/lock.json (installed versions)
│   ├── runtime/               # runtime interface + python.go, node.go, utils.go
│   ├── secrets/               # OS keychain store + env injection
│   ├── executor/              # run a package step with resolved inputs
│   ├── workflow/              # parse and run workflow.json graphs
│   └── ui/                    # spinners and coloured terminal output
├── internal/
│   ├── config/config.go       # global CLI config (~/.neuron/config.json)
│   ├── proxy/                 # transparent MCP JSON-RPC proxy + history store
│   │   ├── proxy.go           #   byte-for-byte forwarding, redaction
│   │   ├── history.go         #   ~/.neuron/history.jsonl (8 MB cap + rotation)
│   │   └── dashboard.go       #   `neuron ui` HTTP endpoints
│   └── sync/                  # E2E-encrypted sync (crypto, client, engine, teams)
├── packages/shared/model.py   # shared helpers for packaged Python tools
├── neuron.json                # Neuron's own manifest (dogfooding)
├── ARCHITECTURE.md            # This file — always read before making changes
└── go.mod
```

---

## Local state

| Path | Contents |
|---|---|
| `~/.neuron/mcp/servers.json` | Managed MCP server definitions (source of truth) |
| `~/.neuron/history.jsonl` | Recorded tool calls for wrapped servers (8 MB cap + rotation) |
| `~/.neuron/lock.json` | Installed package versions (`map[name]version`) |
| `~/.neuron/packages/<name>/<version>/` | Installed package contents |
| `~/.neuron/config.json` | CLI configuration |
| `<client-dir>/.neuron.bak` | One-time backup of a client config before Neuron's first edit |

Secret **values** never appear in any of these files; they live only in the OS
keychain.

---

## Key design decisions

**Provider-agnostic by design.** The `models` field specifies capability
requirements, never specific provider names. This keeps Neuron neutral across
OpenAI, Anthropic, Groq, local Ollama, etc.

**Permission-scoped secrets.** Every package declares its permissions upfront in
`neuron.json`, but the runtime enforces only the `env:` permissions today: a
package receives an environment variable only if it declares `env:NAME`, so
installing one tool never exposes another tool's credentials. The `http`
permission is **not** enforced — there is no network proxy — so treat it as
documentation, not a boundary. (`local:ollama` is likewise declarative.)

**Secrets never touch a config file.** A server that needs a credential is
exposed to clients as `neuron mcp run <name>`; the value is read from the
keychain at launch. Client configs are written atomically (temp file + `fsync` +
rename), with a one-time `.neuron.bak` backup, and unknown keys are preserved
verbatim.

**MCP as a first-class citizen.** A package's `mcp_server` block is registered
on `neuron install` and synced to every detected client — see `../docs/mcp.md`.

**Byte-transparent proxy.** `neuron mcp wrap` routes a server through a
JSON-RPC proxy that forwards bytes verbatim (including the absence of a trailing
newline) and records calls with credential-shaped arguments redacted.

**Semver everywhere.** The resolver handles `^`, `~` and exact pins the way npm
does.

**Local-first.** Packages install to `~/.neuron/packages/`; a single lockfile at
`~/.neuron/lock.json` tracks installed versions.

---

## Tech stack

- **Language:** Go
- **CLI framework:** Cobra (`spf13/cobra`)
- **HTTP client:** standard library `net/http`
- **Prompts:** `AlecAivazis/survey`
- **Output:** `briandowns/spinner`, `fatih/color`
- **Keychain:** `zalando/go-keyring`
- **Testing:** standard library `testing` only (no assertion framework)

---

## What to always do before making changes

1. Read this file first.
2. Keep changes scoped to one package at a time.
3. Never write to files outside the project directory.
4. Always write actual files — do not describe what you would write.
5. After writing a file, confirm it compiles or passes basic syntax checks
   (`go build ./...`, `go vet ./...`, `go test ./...`).
