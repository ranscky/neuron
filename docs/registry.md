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

Publishing is **org-scoped and authenticated**. Every API key belongs to one org,
and an org may only publish packages under its own name (`acme/web-search`).
Reads — search, install, downloads — stay public.

Create an org on your registry (it needs `NEURON_REGISTRY_ADMIN_TOKEN` set) and
keep the key it prints; only its hash is stored, so it is shown once:

```bash
curl -sS -X POST http://127.0.0.1:8080/v1/orgs \
  -H "X-Admin-Token: $NEURON_REGISTRY_ADMIN_TOKEN" \
  -d '{"name":"acme"}'
# {"org":{"id":"org_…","name":"acme"},"api_key":"nrn_…", …}
```

Then publish under that namespace:

```bash
mkdir my-tool && cd my-tool
# create neuron.json with "name": "acme/my-tool" (see the manifest reference)
NEURON_REGISTRY_TOKEN=nrn_… neuron publish
```

Mint more keys (for CI, or to rotate) with the key you already have:

```bash
curl -sS -X POST http://127.0.0.1:8080/v1/keys \
  -H "Authorization: Bearer nrn_…" -d '{"label":"ci"}'
```

## HTTP API

The registry server exposes these routes:

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/v1/search?q=…` | Search packages by name and description |
| `GET` | `/v1/packages/<name>` | Latest version of a package |
| `GET` | `/v1/packages/<name>/versions` | Every published version |
| `GET` | `/v1/packages/<name>/<version>` | Manifest for a specific version |
| `GET` | `/v1/packages/<name>/<version>/download` | Package tarball |
| `POST` | `/v1/publish` | Publish a manifest + tarball (multipart) — **requires an API key** |
| `POST` | `/v1/orgs` | Create an org and get its first key — requires the admin token |
| `POST` | `/v1/keys` | Mint another key for the authenticated org |

## Running your own

```bash
cd registry
# the admin token gates org creation — without it, nobody can claim an org
NEURON_REGISTRY_ADMIN_TOKEN=$(openssl rand -hex 24) PORT=8080 go run ./cmd/server
```

Storage is a file-backed store under `registry/data/packages`, with an inverted
index for search; orgs and API-key hashes live in `registry/data/orgs.json`. Point
the CLI at it:

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

- **Publishing is authenticated and org-scoped.** `/v1/publish` needs an API key
  (`Authorization: Bearer nrn_…`), and an org may only publish under its own name.
  Org creation is gated by `NEURON_REGISTRY_ADMIN_TOKEN`; with it unset the
  registry answers 503 rather than handing an org to whoever finds it first.
- **Keys cannot be revoked yet.** Minting a new key is supported; retiring an old
  one has no endpoint, so rotation currently means replacing the org.
- **No signatures.** Packages are not cryptographically verified at install
  time.
- **No evaluation scores** surfaced in search yet.
