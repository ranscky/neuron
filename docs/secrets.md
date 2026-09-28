---
layout: page
title: Secrets
---

# Secrets

MCP servers need credentials — API tokens, personal access tokens, service
keys. The usual approach is to paste them into a client's JSON config, where
they sit in plaintext, get synced to backups, and end up in screenshots.

Neuron stores secrets in your **OS keychain** and injects them into a server's
environment at launch.

## Commands

```bash
neuron secrets set github-token ghp_xxxxxxxx   # write a secret
neuron secrets get github-token                # read it back
neuron secrets list                            # secrets required by managed servers
neuron secrets rm github-token                 # delete it
```

## How a secret reaches a server

1. You store the value once:

   ```bash
   neuron secrets set github-token ghp_xxxxxxxx
   ```

2. You register the server and name the secret, mapping it to an environment
   variable:

   ```bash
   neuron mcp add github --command npx --arg=-y \
     --arg=@modelcontextprotocol/server-github --secret GITHUB_TOKEN=github-token
   ```

   `--secret GITHUB_TOKEN=github-token` means *inject the keychain entry named
   `github-token` as the environment variable `GITHUB_TOKEN`.*

3. Neuron writes the client config so the client runs `neuron mcp run github`.
   At launch, Neuron reads the keychain and starts the real server with the
   environment variable set.

The value never appears in any client config, in Neuron's server store, or on
disk. You can confirm:

```bash
grep -r ghp_ ~/.cursor/mcp.json ~/.claude.json   # no hits
```

## Where the keychain lives

Neuron uses the platform keyring: Keychain on macOS, Secret Service /
`gnome-keyring` on Linux, Credential Manager on Windows. If no keyring backend
is available (common in headless CI), the secret tests skip rather than fail,
and `neuron secrets set` will report the backend error.

## For headless and CI use

Non-interactive environments can pass credentials through the environment
instead of the keychain — see [Sync](sync.md) for `NEURON_SYNC_TOKEN` and
`NEURON_PASSPHRASE`.

## Providers (packages, not MCP servers)

For package execution, Neuron reads provider credentials from the environment
and only when the package declares them:

- `NEURON_OPENAI_API_KEY`, `NEURON_ANTHROPIC_API_KEY`, `NEURON_GROQ_API_KEY`
- `NEURON_OLLAMA_BASE_URL` (defaults to a local Ollama)
- provider selection: `NEURON_PROVIDER`, model: `NEURON_MODEL`

A package only receives the provider keys it declares in its manifest's
`permissions` (e.g. `env:TAVILY_API_KEY`), so installing one tool never leaks
another tool's credentials.
