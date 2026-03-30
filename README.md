# Neuron

**The package manager for AI tools, agents, and models.**

```bash
neuron install agents/researcher
neuron run agents/researcher '{"query": "AI startups in Africa"}'
```

---

## What is Neuron?

Neuron is to AI tools what npm is to JavaScript.

Developers today wire together agents, models, RAG pipelines and MCP servers
manually — cloning repos, reading READMEs, managing dependencies by hand.
Neuron standardizes how AI tools are packaged, distributed, and executed.

One command to install. One command to run. Any tool, any model, any agent.

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

- [x] CLI with install, publish, run, search, list
- [x] Live registry with persistent storage
- [x] Typed capability schema (neuron.json v1)
- [x] Sandboxed Python runtime with venv isolation
- [x] Secrets management with OS keychain
- [x] 10 official packages
- [ ] neuron secrets set command
- [ ] Automatic dependency installation on neuron run
- [ ] Composition engine — chain agents automatically
- [ ] Node.js runtime
- [ ] Private registries for enterprises
- [ ] Usage metrics and outcome-based ranking
- [ ] Pay-per-execution billing layer

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