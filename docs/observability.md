---
layout: page
title: Observability
---

# Observability

Your agent calls tools you cannot see. Neuron can route a managed server
through a local proxy that records every JSON-RPC message, then show you a live
dashboard of what happened.

## Record calls

```bash
neuron mcp wrap github    # route github through the proxy
neuron mcp unwrap github  # stop routing it
```

`wrap` flips the server to run through `neuron mcp run`, which both resolves
secrets and proxies traffic. Nothing else about the server changes.

## Watch them

```bash
neuron ui                 # http://127.0.0.1:7717
neuron ui --port 7799     # a different port
```

The dashboard shows a live call stream plus per-server totals (calls, errors,
average latency, token estimates).

## What is recorded

Each call is appended to `~/.neuron/history.jsonl`:

| Field | Meaning |
|---|---|
| `method` | JSON-RPC method (`tools/call`, `tools/list`, …) |
| `tool` | Tool name for `tools/call` |
| `args` | Request arguments, **redacted** (see below) |
| `result` | Result payload, or the error |
| `status` | `ok` or error |
| `duration_ms` | Wall-clock latency |
| `tokens_in`, `tokens_out` | Rough token estimate |

The file is capped at 8 MB and rotated so it cannot grow without bound.

## Redaction

Before anything is written, argument keys matching any of `token`, `secret`,
`password`, `passwd`, `apikey`, `api_key`, `authorization`, `credential`, or
`private_key` (case-insensitive, at any nesting depth) have their values
replaced with `[redacted]`. Recording is best-effort: if the history file
cannot be written, the call still proceeds normally.

## Transparency guarantees

- **Bytes are forwarded verbatim**, including the presence or absence of a
  trailing newline, so wrapping a server cannot change client behaviour.
- The proxy never rewrites or reorders messages.
- `unwrap` returns the server to a direct launch; history accumulates only for
  wrapped servers.

## Privacy

History is local-only: `~/.neuron/history.jsonl` is never uploaded. (Encrypted
sync covers server *definitions*, not call history.) Delete the file at any
time to clear it.
