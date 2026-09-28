---
layout: page
title: The registry
---

# The registry

Neuron ships with a package registry — a Go HTTP service that stores
`neuron.json` manifests and package tarballs, and serves them to `neuron
install` and `neuron publish`. The CLI defaults to the hosted registry; you can
point it at your own with `NEURON_REGISTRY_URL`.

## Using the registry

```bash
neuron search agents                      # find packages
neuron install agents/researcher          # install one (resolves the version)
neuron list                               # what is installed
neuron run agents/researcher '{"query":"latest AI trends"}'
neuron update agents/researcher
neuron uninstall agents/researcher
```

## Publishing

```bash
mkdir my-tool && cd my-tool
# create neuron.json and your entrypoint (see the manifest reference)
neuron publish
```

`publish` validates `neuron.json`, archives the current directory, and uploads
the manifest and tarball to the registry.

## HTTP API

The registry server exposes three routes:

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/v1/search?q=…` | Search packages by name and description |
| `GET` | `/v1/packages/<name>` | Latest version of a package |
| `GET` | `/v1/packages/<name>/versions` | Every published version |
| `GET` | `/v1/packages/<name>/<version>` | Manifest for a specific version |
| `GET` | `/v1/packages/<name>/<version>/download` | Package tarball |
| `POST` | `/v1/publish` | Publish a manifest + tarball (multipart) |

## Running your own

```bash
cd registry
PORT=8080 go run ./cmd/server
```

Storage is a file-backed store under `registry/data/packages`, with an inverted
index for search. Point the CLI at it:

```bash
export NEURON_REGISTRY_URL=http://127.0.0.1:8080
neuron search agents
```

## Official packages

Neuron ships 10 packages across 4 categories.

### Agents

| Package | Description |
|---|---|
| `agents/researcher` | Research any topic — searches the web and synthesizes findings |
| `agents/code-reviewer` | Review code for bugs, security issues and improvements |
| `agents/content-writer` | Write blog posts, summaries and articles |
| `agents/startup-analyst` | Analyze a business idea with market research and scoring |
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

## Status and limitations

The registry is functional but the layer is being rebuilt on top of the MCP
control plane. Known gaps, stated plainly:

- **`/v1/publish` has no authentication.** Anyone who can reach the service can
  publish. Do not run an untrusted public instance. (API-key auth and
  org-scoped storage are implemented on an unmerged branch and need
  reconciliation before shipping.)
- **No signatures.** Packages are not cryptographically verified at install
  time.
- **No evaluation scores** surfaced in search yet.
