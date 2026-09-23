# Neuron

**The MCP control plane for developers.**

```bash
neuron mcp add github --command npx --arg=-y \
  --arg=@modelcontextprotocol/server-github --secret GITHUB_TOKEN=github-token
```

One command registers an MCP server and syncs it to **every** AI client you
use — Claude Code, Claude Desktop, Cursor, Cline, Windsurf, VS Code, and Zed —
with credentials kept in your OS keychain instead of in plaintext JSON.

---

## What is Neuron?

MCP is becoming the USB-C of AI tools, but every client keeps its own config
file, stores secrets in plaintext, and gives you almost no visibility into what
your agent is actually doing.

Neuron is the missing control plane:

- **One manifest, every client.** Register a server once; Neuron writes it to
  every client it detects, preserving keys and formatting it does not own.
- **Secrets never touch a config file.** Servers that need credentials are
  launched through `neuron mcp run`, which resolves them from the OS keychain
  at launch.
- **Safe by default.** Atomic writes, a one-time `.neuron.bak` backup of every
  config it edits, and `neuron mcp doctor` to catch missing secrets and dead
  commands.

Neuron also remains a package manager for distributed agents and tools; that
layer is being rebuilt on top of this foundation.

---

## Install

```bash
# coming soon: curl -fsSL https://get.neuron.ai | sh

# for now, build from source
git clone https://github.com/ranscky/neuron
cd neuron/cli
go build -o neuron ./cmd/neuron
sudo mv neuron /usr/local/bin/neuron
```

---

## Quick start

```bash
# search the registry
neuron search agents

# install a package
neuron install agents/researcher

# run it
neuron run agents/researcher '{"query": "latest AI trends"}'

# publish your own tool
cd my-ai-tool
neuron publish
```

---

## Manage MCP servers

```bash
# see what Neuron manages and which clients it found
neuron mcp list

# register a local (stdio) server
neuron mcp add filesystem --command npx --arg=-y \
  --arg=@modelcontextprotocol/server-filesystem

# register a server that needs a credential: the value lives in your keychain,
# never in a client config file
neuron secrets set github-token ghp_xxx
neuron mcp add github --command npx --arg=-y \
  --arg=@modelcontextprotocol/server-github --secret GITHUB_TOKEN=github-token

# register a remote server
neuron mcp add linear --url https://mcp.linear.app/sse --type sse

# push everything to every detected client again
neuron mcp sync

# check for missing secrets and unreachable commands
neuron mcp doctor

# remove a server from the store and from every client
neuron mcp remove github

# route a server through the proxy so its calls are recorded
neuron mcp wrap github
```

Servers that declare `--secret`, or that you `wrap`, are exposed to clients as
`neuron mcp run <name>`. Neuron resolves credentials from the keychain when the
client starts the server, so values never land on disk, and every JSON-RPC
message passes through the proxy where it can be recorded. Neuron writes
configs atomically, keeps a one-time `.neuron.bak` backup, and preserves any
keys it does not understand.

---

## See what your agent is doing

```bash
neuron ui          # dashboard at http://127.0.0.1:7717
```

Every call on a wrapped server is recorded — method, tool name, arguments,
result, error, latency, and a rough token estimate — to
`~/.neuron/history.jsonl`. The dashboard shows a live call stream plus
per-server totals. Credential-shaped arguments (`token`, `secret`, `api_key`,
`authorization`, …) are redacted before storage, and the proxy forwards bytes
to the client unmodified.

| Client | Config file |
|---|---|
| Claude Code | `~/.claude.json` |
| Claude Desktop | `~/Library/Application Support/Claude/claude_desktop_config.json` (macOS) |
| Cursor | `~/.cursor/mcp.json` |
| Cline | VS Code globalStorage `.../saoudrizwan.claude-dev/settings/cline_mcp_settings.json` |
| Windsurf | `~/.codeium/windsurf/mcp_config.json` |
| VS Code | `<User>/mcp.json` (uses the `servers` key) |
| Zed | `~/.config/zed/settings.json` (uses `context_servers`) |

---

## The registry

10 official packages across 4 categories:

### Agents
| Package | Description |
|---|---|
| `agents/researcher` | Research any topic — searches the web and synthesizes findings |
| `agents/code-reviewer` | Review code for bugs, security issues and improvements |
| `agents/content-writer` | Write blog posts, summaries and articles |
| `agents/startup-analyst` | Analyze any business idea with market research and scoring |
| `agents/customer-support` | Answer support queries using your knowledge base |

### Tools
| Package | Description |
|---|---|
| `tools/web-search` | Real-time web search powered by Tavily |
| `tools/github` | Interact with GitHub repos, issues and files |

### RAG
| Package | Description |
|---|---|
| `rag/pdf-reader` | Chat with any PDF document |
| `rag/notion-sync` | Use your Notion workspace as an AI knowledge base |

### Models
| Package | Description |
|---|---|
| `models/qwen-coder` | Qwen3 Coder 480B — local code generation via Ollama |

---

## The neuron.json standard

Every Neuron package is defined by a `neuron.json` manifest:

```json
{
  "name": "agents/researcher",
  "version": "1.0.0",
  "description": "Research assistant that searches the web and synthesizes findings",
  "capability": {
    "input": [
      { "name": "query", "type": "string", "required": true },
      { "name": "depth", "type": "integer", "required": false, "default": 3 }
    ],
    "output": { "type": "object", "format": "json" }
  },
  "performance": {
    "avg_latency_ms": 3000,
    "success_rate": 0.95,
    "cost_per_call_usd": 0.005
  },
  "permissions": ["http", "env:TAVILY_API_KEY", "local:ollama"],
  "dependencies": { "tools/web-search": "^1.0.0" },
  "runtime": "python",
  "entry": "main.py",
  "neuron": "1"
}
```

The `capability` block is the key innovation — strict typed inputs/outputs
that let agents discover, compose, and chain tools automatically.

---

## Composition

Agents declare dependencies on tools. Neuron resolves and wires them:

```
agents/researcher
 ├── tools/web-search    (fetches real-time data)
 └── models/qwen-coder   (synthesizes findings)

agents/startup-analyst
 ├── tools/web-search    (researches market)
 └── models/qwen-coder   (generates analysis)
```

This is the foundation of the composition engine — coming in v2.

---

## Publishing a package

```bash
mkdir my-tool && cd my-tool

# create neuron.json
cat > neuron.json << 'EOF'
{
  "name": "tools/my-tool",
  "version": "1.0.0",
  "description": "Does something useful",
  "capability": {
    "input": [{ "name": "input", "type": "string", "required": true }],
    "output": { "type": "string", "format": "text" }
  },
  "permissions": [],
  "runtime": "python",
  "entry": "main.py",
  "neuron": "1"
}
EOF

# create main.py (reads stdin, writes stdout)
cat > main.py << 'EOF'
import sys, json
inputs = json.loads(sys.stdin.read())
result = {"output": f"processed: {inputs['input']}"}
print(json.dumps(result))
EOF

# publish
neuron publish
```

---

## Roadmap

The full plan lives in [BUILD_ROADMAP.md](BUILD_ROADMAP.md).

- [x] Cross-client MCP management — `neuron mcp add/sync/list/doctor`
- [x] Keychain-backed secrets that never touch client configs
- [x] Atomic config writes with backups; unknown keys preserved
- [ ] CI (vet, build, test) for both modules — not pushed yet; the token lacks `workflow` scope
- [x] Local MCP proxy with tool-call history and a dashboard (Phase 2)
- [ ] Cloud sync, team configs, and the $20/mo Pro tier (Phase 3)
- [ ] Registry package distribution for agents, tools, and models
- [ ] Node.js runtime
- [ ] Policy engine, audit log, SSO — the enterprise gateway (Phase 5)

---

## Architecture

```
neuron/
├── cli/          # Go CLI built with Cobra
└── registry/     # Go HTTP registry server
    └── deployed on Railway
```

Full architecture details in [cli/ARCHITECTURE.md](cli/ARCHITECTURE.md)
and [registry/ARCHITECTURE.md](registry/ARCHITECTURE.md).

---

## Contributing

Neuron is early and moving fast. The best way to contribute right now:

1. Build a package and publish it to the registry
2. Open an issue if something breaks
3. Star the repo if you believe in the vision

---

## License

MIT