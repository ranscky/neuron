# Neuron — Architecture

## What is Neuron?

Neuron is a CLI-based distribution layer for AI tools, agents, and MCP servers.
Think npm, but for AI. Developers can publish, discover, install, and run AI tools
with a single command. Neuron handles versioning, dependencies, secrets, and
sandboxed execution.

---

## Core commands (MVP)

```bash
neuron install <package>      # install a tool from the registry
neuron publish                # publish current directory as a Neuron package
neuron run <package> [args]   # run an installed tool
neuron search <query>         # search the registry
neuron list                   # list installed packages
```

---

## The manifest: neuron.json

Every Neuron package has a `neuron.json` at its root. This is the standard
Neuron package format — all tooling is built around this file.

```json
{
  "name": "my-agent",
  "version": "1.0.0",
  "description": "Does something useful with AI",
  "entry": "main.py",
  "runtime": "python",
  "permissions": ["http", "env:OPENAI_KEY"],
  "models": [
    { "capability": "tool_use", "context": 32000 }
  ],
  "dependencies": {
    "web-search-tool": "^1.2.0"
  },
  "mcp": {
    "compatible": true,
    "server": "server.py"
  }
}
```

### Manifest fields

| Field | Required | Description |
|---|---|---|
| `name` | yes | Unique package name on the registry |
| `version` | yes | Semver version string |
| `description` | yes | Short description shown in search |
| `entry` | yes | Entry point file to execute |
| `runtime` | yes | `python`, `node`, or `binary` |
| `permissions` | no | Declared sandbox permissions |
| `models` | no | Model capability requirements (provider-agnostic) |
| `dependencies` | no | Other Neuron packages this depends on |
| `mcp.compatible` | no | Whether this package exposes an MCP server |
| `mcp.server` | no | MCP server entry point if applicable |

---

## Project structure

```
neuron/
├── cmd/
│   └── neuron/
│       └── main.go           # CLI entry point, cobra commands
├── pkg/
│   ├── manifest/
│   │   ├── manifest.go       # Manifest struct definition
│   │   ├── parser.go         # Read and validate neuron.json
│   │   └── validator.go      # Field validation logic
│   ├── registry/
│   │   ├── registry.go       # Registry client interface
│   │   ├── resolve.go        # Package resolution and version matching
│   │   └── publish.go        # Publishing a package to the registry
│   ├── runtime/
│   │   ├── runtime.go        # Runtime interface
│   │   ├── sandbox.go        # Permission enforcement and sandboxing
│   │   ├── python.go         # Python runtime executor
│   │   └── node.go           # Node runtime executor
│   ├── installer/
│   │   ├── installer.go      # Download and install packages
│   │   └── lockfile.go       # neuron.lock generation and reading
│   └── secrets/
│       ├── store.go          # OS keychain integration
│       └── inject.go         # Inject secrets into runtime env
├── internal/
│   └── config/
│       └── config.go         # Global CLI config (~/.neuron/config.json)
├── neuron.json               # Neuron's own manifest (dogfooding)
├── ARCHITECTURE.md           # This file — always read before making changes
└── go.mod
```

---

## Key design decisions

**Provider-agnostic by design.** The `models` field in `neuron.json` specifies
capability requirements, never specific provider names. This keeps Neuron neutral
across OpenAI, Anthropic, local models, etc.

**Permission model first.** Every package must declare its permissions upfront in
`neuron.json`. The runtime enforces these. No undeclared HTTP calls, no undeclared
env var access. This builds trust in the ecosystem.

**MCP as a first-class citizen.** Packages that expose MCP servers are
auto-detected and can be registered with any MCP-compatible client via
`neuron run <package> --mcp`.

**Semver everywhere.** Package versioning strictly follows semver. The resolver
handles `^`, `~`, and exact pins the same way npm does.

**Local-first registry.** Packages install to `~/.neuron/packages/`. A global
lockfile at `~/.neuron/lock.json` tracks installed versions.

---

## Tech stack

- **Language:** Go
- **CLI framework:** Cobra
- **HTTP client:** Standard library `net/http`
- **Keychain:** `zalando/go-keyring` for cross-platform secret storage
- **Testing:** Standard library `testing` + `testify`

---

## What to always do before making changes

1. Read this file first.
2. Keep changes scoped to one package at a time.
3. Never write to files outside the project directory.
4. Always write actual files — do not describe what you would write.
5. After writing a file, confirm it compiles or passes basic syntax checks.
