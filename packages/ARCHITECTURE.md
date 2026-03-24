# Neuron Packages — Architecture

## What is this?

This folder contains the official first-party Neuron packages.
Each subfolder is a standalone Neuron package that can be published
to the Neuron registry independently.

---

## Folder structure

```
packages/
├── ARCHITECTURE.md         ← this file
├── tools/
│   ├── web-search/         ← Tier 1: primitive, no dependencies
│   └── github/             ← Tier 1: primitive, no dependencies
├── models/
│   └── qwen-coder/         ← Tier 1: model abstraction
├── rag/
│   ├── pdf-reader/         ← Tier 2: depends on tier 1 patterns
│   └── notion-sync/        ← Tier 2: depends on tier 1 patterns
└── agents/
    ├── researcher/          ← Tier 3: uses tools/web-search
    ├── code-reviewer/       ← Tier 3: uses tools/github
    ├── content-writer/      ← Tier 3: uses tools/web-search
    ├── startup-analyst/     ← Tier 3: uses tools/web-search
    └── customer-support/    ← Tier 3: uses rag/notion-sync
```

---

## Every package must have

```
<package>/
├── neuron.json     ← capability manifest (required)
├── main.py         ← entry point (required)
├── requirements.txt ← python dependencies (required)
└── README.md       ← usage instructions (required)
```

---

## Build order

Always build in tier order. Tier 1 first, then 2, then 3.
Agents depend on tools — tools must exist before agents are built.

```
Tier 1 → tools/web-search, tools/github, models/qwen-coder
Tier 2 → rag/pdf-reader, rag/notion-sync
Tier 3 → agents/researcher, agents/code-reviewer,
          agents/content-writer, agents/startup-analyst,
          agents/customer-support
```

---

## neuron.json structure for every package

```json
{
  "name": "category/package-name",
  "version": "1.0.0",
  "description": "One line description",
  "capability": {
    "input": [
      { "name": "field", "type": "string", "required": true }
    ],
    "output": { "type": "string", "format": "text" },
    "errors": ["error_type"]
  },
  "performance": {
    "avg_latency_ms": 0,
    "p99_latency_ms": 0,
    "success_rate": 0.99,
    "cost_per_call_usd": 0.000
  },
  "permissions": [],
  "models": [
    { "capability": "tool_use", "context": 32000 }
  ],
  "runtime": "python",
  "entry": "main.py",
  "dependencies": {},
  "neuron": "1"
}
```

---

## How main.py must be structured

Every package entry point follows this exact pattern:

```python
import sys
import json

def run(inputs: dict) -> dict:
    # inputs is a dict of the capability input fields
    # return a dict matching the capability output schema
    pass

if __name__ == "__main__":
    # Neuron passes inputs as a JSON string via stdin
    raw = sys.stdin.read()
    inputs = json.loads(raw)
    result = run(inputs)
    print(json.dumps(result))
```

Neuron runtime pipes JSON in via stdin and reads JSON out via stdout.
Never use argparse or sys.argv — always use stdin/stdout.

---

## Search API

All packages that need web search use Tavily.
API key is declared as permission "env:TAVILY_API_KEY" in neuron.json
and injected by the Neuron runtime via the OS keychain.

Install: pip install tavily-python

---

## Rules

1. Read this file before making any changes.
2. One package per Cline session.
3. Always write actual files — never describe what you would write.
4. Every package must be self-contained — no shared code between packages.
5. Keep implementations simple — clarity over cleverness.
6. Test every package manually before publishing.
