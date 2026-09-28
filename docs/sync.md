---
layout: page
title: Sync
---

# Sync

Neuron can move your MCP server definitions between machines — encrypted
end-to-end — and share a set of servers with a team.

The sync service is self-hostable and dependency-free.

## Run the service

```bash
cd cloud
NEURON_CLOUD_DATA=./data/cloud.json PORT=8080 go run ./cmd/neuron-cloud
```

## Sign in and sync

```bash
neuron login --server http://127.0.0.1:8080   # device flow
neuron sync                                    # push and pull servers
neuron logout
```

`neuron sync` prints what it pushed and pulled, and reports
`Everything is already in sync.` when there is nothing to do.

## What is encrypted

Sync is **end-to-end encrypted**. Your passphrase never leaves the machine; the
service stores only AES-256-GCM ciphertext. Every envelope is bound to the
server name and version, so a compromised service cannot serve one server's
payload in place of another's. A server changed on two machines is reported as
a **conflict** and left untouched rather than silently overwritten.

The session token lives in your OS keychain, never in a config file.

## Headless / CI use

Skip the interactive prompt and the keychain:

```bash
export NEURON_SYNC_TOKEN=...       # a session token
export NEURON_PASSPHRASE=...       # the sync passphrase
neuron sync
```

## Self-hosting without a paid plan

Encrypted sync is the paid feature; local configuration management, secrets and
the dashboard stay free. Self-hosters can lift the gate:

```bash
NEURON_REQUIRE_PRO=false ...
```

## Team-shared servers

```bash
neuron team create platform      # prints an invite code
neuron team join 2CFT-TUCA       # on a teammate's machine
neuron team sync platform        # push and pull the shared set
neuron team list
```

A team has its own passphrase and therefore its own key. Shared servers are
encrypted with the **team key**, and every blob is bound to the team as well as
to the server name, so a blob cannot be replayed across teams. Any member can
publish; a server changed by two people at once is reported as a conflict,
exactly as in personal sync. The service never sees the team passphrase.

Team passphrases can also come from the environment or a prompt:

| Where | Personal | Team |
|---|---|---|
| Flag | `--passphrase` | `--passphrase` |
| Environment | `NEURON_PASSPHRASE` | `NEURON_TEAM_PASSPHRASE` |
| Prompt | interactive | interactive |

## Known limitations

- The service stores ciphertext, but **removing a team member currently means
  rotating the team passphrase** — team sharing uses a shared passphrase rather
  than wrapping the team key to each member's public key.
- Personal sync covers server definitions and secrets, not call history.
