---
layout: page
title: Policy
---

# Policy

A policy decides which tool calls may reach a server. It is enforced **in the
proxy**, on this machine, before a call is forwarded — so a refused tool never
reaches the server, and the refusal is recorded like any other failed call.

No policy file means no restrictions. A policy is opt-in.

## The file

`~/.neuron/policy.json` — or any path in `NEURON_POLICY`:

```json
{
  "default": "allow",
  "scopes": {
    "read":  ["get_*", "list_*", "read_*"],
    "write": ["create_*", "update_*", "delete_*"]
  },
  "servers": {
    "github": {
      "grant": ["read"],
      "allow": ["create_issue"],
      "deny": ["delete_repo"],
      "rate_limit": {"calls": 60, "per": "1m"}
    },
    "filesystem": {"grant": ["read"]}
  }
}
```

| Field | Meaning |
|---|---|
| `default` | What happens to servers with no entry: `allow` (the default) or `deny` |
| `scopes` | Named groups of tool patterns you can grant to servers |
| `servers.<name>.grant` | Scopes whose patterns are allowed on that server |
| `servers.<name>.allow` | Extra tool patterns allowed on top of the scopes |
| `servers.<name>.deny` | Tool patterns that are always refused — **deny wins** |
| `servers.<name>.rate_limit` | At most `calls` per `per` (`30s`, `1m`, `1h`) |

Patterns are case-insensitive globs: `*` matches anything, so `get_*` covers
`get_file`. A server entry with only `deny` rules means "everything else is
allowed". A server entry with `grant`/`allow` rules means "only those".

## Checking before you run

```bash
neuron policy show                     # what is loaded, and from where
neuron policy check github delete_repo # explains the decision, exits 1 if denied
```

`check` evaluates the rules without consuming rate-limit quota, so it is safe to
run while a session is live.

## What a refusal looks like

A blocked call is answered with a JSON-RPC error and recorded in the history:

```
{"jsonrpc":"2.0","id":2,
 "error":{"code":-32001,"message":"blocked by Neuron policy: \"rm_rf\" is denied for demo by rule \"rm_rf\""}}
```

Codes: `-32001` policy denial, `-32002` rate limit. Both show up as `error`
entries in `~/.neuron/history.jsonl`, so `neuron ui` reports them like any other
failure.

## Limits, stated plainly

- **Only `tools/call` is policed.** Blocking protocol chatter would break the
  session, so `initialize`, `tools/list` and friends always pass.
- **Rate limits are per process.** Each `neuron mcp run` session counts its own
  calls; they are not shared across clients or persisted.
- **Enforcement is local.** A client that talks to a server directly, without
  going through `neuron mcp run`, is outside the policy. The policy protects the
  managed path — which is the path Neuron writes into every client config.
- **No per-client or per-user scopes yet.** One policy file applies to every
  session on the machine.
