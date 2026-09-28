---
layout: page
title: Architecture
---

# Architecture

Neuron is three independent Go modules plus a set of example packages. Each
module has its own `go.mod` and is built, vetted and tested separately in CI.

```
neuron/
├── cli/            # the neuron CLI (module github.com/ranscky/neuron)
├── cloud/          # the self-hostable sync service (…/neuron-cloud)
├── registry/       # the package registry server (…/neuron-registry)
├── packages/       # example packages published to the registry
└── scripts/        # install.sh (build from source)
```

## `cli/`

The command-line tool. Cobra commands live in `cmd/neuron/main.go`; the logic
is in packages.

| Package | Responsibility |
|---|---|
| `pkg/mcp` | Client registry (config paths + formats), atomic writes, sync, doctor |
| `internal/proxy` | Transparent MCP JSON-RPC proxy and the call-history store |
| `internal/sync` | E2E-encrypted sync engine (personal and team) |
| `pkg/secrets` | OS keychain store and env injection |
| `pkg/manifest` | `neuron.json` struct, parser and validation |
| `pkg/registry` | Registry HTTP client and semver resolution |
| `pkg/installer` | Download/extract packages |
| `pkg/lockfile` | Installed-version tracking |
| `pkg/runtime` | Python (and planned Node) execution |
| `pkg/workflow` | Workflow runner for composed packages |
| `pkg/ui` | Terminal spinners and colour output |

### Local state

| Path | Contents |
|---|---|
| `~/.neuron/mcp/servers.json` | Managed MCP server definitions (source of truth) |
| `~/.neuron/history.jsonl` | Recorded tool calls for wrapped servers (capped at 8 MB) |
| `~/.neuron/lock.json` | Installed package versions |
| `~/.neuron/packages/<name>/<version>/` | Installed package contents |
| `~/.neuron/config.json` | CLI configuration |

Secret **values** are never written to any of these; they live in the OS
keychain.

## `cloud/`

A dependency-free HTTP service: device-flow auth, encrypted-blob sync, teams,
rate limiting, Stripe webhook verification and plan state. It stores only
ciphertext — the passphrase and keys never leave the client. Self-hostable.

## `registry/`

A file-backed package registry with an inverted search index. It stores
manifests and tarballs and serves the `/v1/search`, `/v1/packages/…` and
`/v1/publish` routes. See [The registry](registry.md) for the API and current
limitations.

## Permission model — what is and isn't enforced

Packages declare `permissions` in `neuron.json`. Today Neuron enforces **only**
the `env:` permission: a package receives an environment variable only if it
declares `env:NAME`, and every other provider key is withheld.

The `http` permission is declared in manifests but is **not** enforced — there
is no network proxy yet. Do not rely on it as a security boundary. (A stricter
runtime proxy is planned.)

## Further reading

- [`cli/ARCHITECTURE.md`](https://github.com/ranscky/neuron/blob/main/cli/ARCHITECTURE.md)
- [`registry/ARCHITECTURE.md`](https://github.com/ranscky/neuron/blob/main/registry/ARCHITECTURE.md)
