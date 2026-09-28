---
layout: page
title: Neuron documentation
permalink: /
---

# Neuron documentation

**One place to install, configure, secure, and debug every MCP server across
every AI client you use.**

Neuron is a command-line control plane for [Model Context
Protocol](https://modelcontextprotocol.io) servers. Instead of hand-editing a
different JSON file for every client — and pasting API keys into plaintext —
you register a server once with `neuron mcp add` and Neuron writes it to every
client it detects, resolving credentials from your OS keychain at launch.

```bash
neuron mcp add github --command npx --arg=-y \
  --arg=@modelcontextprotocol/server-github --secret GITHUB_TOKEN=github-token
```

## Where to start

| If you want to… | Read |
|---|---|
| Install Neuron and register your first server | [Getting started](getting-started.md) |
| Understand how servers are synced across clients | [Managing MCP servers](mcp.md) |
| Keep credentials out of config files | [Secrets](secrets.md) |
| See what your agent is actually doing | [Observability](observability.md) |
| Move your setup between machines or share it with a team | [Sync](sync.md) |
| Publish or install packages | [The registry](registry.md) |
| Write a `neuron.json` manifest | [The manifest](manifest.md) |
| Look up a command or flag | [CLI reference](cli-reference.md) |
| Understand the code layout | [Architecture](architecture.md) |

## What Neuron is (and isn't)

Neuron **is**:

- a cross-client manager for MCP servers (Claude Code, Claude Desktop, Cursor,
  Cline, Windsurf, VS Code, Zed);
- a keychain-backed secret store — secret values are never written to a client
  config;
- a local proxy that records tool calls so you can see and debug them.

Neuron **also** ships a package registry and a package manager, but that layer
is being rebuilt on top of the MCP control plane. The MCP features above are
the stable, tested core today.

## Two-minute tour

```bash
# install (from source for now)
git clone https://github.com/ranscky/neuron && cd neuron/cli
go build -o neuron ./cmd/neuron && sudo mv neuron /usr/local/bin/neuron

# what does Neuron manage, and which clients did it find?
neuron mcp list

# register a server that needs a credential
neuron secrets set github-token ghp_xxxxxxxx
neuron mcp add github --command npx --arg=-y \
  --arg=@modelcontextprotocol/server-github --secret GITHUB_TOKEN=github-token

# confirm everything is wired up
neuron mcp doctor

# record and watch its calls
neuron mcp wrap github
neuron ui
```

## License

MIT.
