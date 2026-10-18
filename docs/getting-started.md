---
layout: page
title: Getting started
---

# Getting started

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/ranscky/neuron/main/scripts/install.sh | sh
```

That downloads the release binary for your OS and architecture, verifies it
against the release's `checksums.txt`, and installs it to `~/.local/bin` (use
`--prefix` to change that). Override the release with `--version 0.1.0`, or
build from a checkout with `--source`.

Building from source by hand works too, if you have Go:

```bash
git clone https://github.com/ranscky/neuron
cd neuron/cli
go build -o neuron ./cmd/neuron
sudo mv neuron /usr/local/bin/neuron
```

Verify:

```bash
neuron version
```

> A Homebrew tap is published per release; `brew install ranscky/neuron/neuron`
> becomes available once `scripts/publish-tap.sh` has been run for a release.

## Register your first server

`neuron mcp add` registers a server in Neuron's own store and then **syncs it to
every client it detects**. A local (stdio) server:

```bash
neuron mcp add filesystem --command npx --arg=-y \
  --arg=@modelcontextprotocol/server-filesystem
```

A remote server:

```bash
neuron mcp add linear --url https://mcp.linear.app/sse --type sse
```

A server that needs a credential — the value goes in your keychain, not in any
config file:

```bash
neuron secrets set github-token ghp_xxxxxxxx
neuron mcp add github --command npx --arg=-y \
  --arg=@modelcontextprotocol/server-github --secret GITHUB_TOKEN=github-token
```

`--secret ENV_NAME=SECRET_KEY` says: when this server launches, inject the
keychain entry `SECRET_KEY` as the environment variable `ENV_NAME`. Neuron
rewrites the client config so the client launches `neuron mcp run github`
instead of `npx`, so the value is resolved at launch time and never written to
disk.

## Check your work

```bash
neuron mcp list     # managed servers + which clients were detected
neuron mcp doctor   # validate configs, missing secrets, unreachable commands
```

`doctor` is the fastest way to find a server that will fail to start: it reports
secrets that are not set and commands that are not on `PATH`.

## See it working

Route a server through the local proxy and open the dashboard:

```bash
neuron mcp wrap github
neuron ui            # http://127.0.0.1:7717
```

Every JSON-RPC call is recorded (with credential-shaped arguments redacted).
See [Observability](observability.md).

## Next steps

- [Managing MCP servers](mcp.md) — the full command set and the client table.
- [Secrets](secrets.md) — how keychain-backed credentials work.
- [Sync](sync.md) — move your setup to another machine.
