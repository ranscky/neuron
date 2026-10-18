# Neuron — Build Roadmap

**Owner:** the agent (me). **Human:** reviews weekly, makes product calls.
**Objective:** turn Neuron into the **MCP control plane for developers** — free cross-client
server management, paid observability — and reach **$1–3k MRR by 2026-12-31**.
**Window:** 2026-09-23 → 2026-12-31 (14 weeks).

---

## The product in one line

**One place to install, configure, secure, and debug every MCP server across every AI client you use.**

| Free | Pro — $20/mo |
|---|---|
| CLI: add / remove / list / sync across clients | Persistent tool-call history and analytics |
| Keychain-backed secrets, never written to JSON | Token + cost tracking per server and tool |
| Single-server inspector | Replay, diffing, advanced debugging |
| Community registry | Cloud sync across machines, team-shared configs, hosted servers, support |

Later (same proxy, different packaging): org policy, SSO, audit retention, egress control → the
enterprise gateway. **The $20/mo product and the enterprise product are one engine.**

---

## Operating principles

1. **`main` stays green.** Every merge passes `go vet` + `go test` in CI.
2. **Every phase ends with a usable artifact**, tests, and docs — not a branch.
3. **No phase starts until the previous phase's acceptance criteria are met.**
4. **Build thin, ship, instrument.** Code is free; distribution is the constraint.
5. **No false claims.** If it isn't enforced, it isn't in the README.
6. **Charge from day one.** Free users are a funnel, paying users are the business.
7. **20% build / 80% distribution** once Phase 3 starts.
8. **Kill criteria are binding.** A dead niche gets one week of diagnosis, not a quarter of grinding.

---

## Starting position (audited 2026-09-23)

**Works:** manifest parsing, registry server + inverted index (tests green), semver resolver
(`resolve.go`), installer/extract, Python venv runtime, keychain store, MCP client detect/inject,
lockfile, executor, workflow runner.

**Broken / dishonest / missing:**

| Issue | Impact |
|---|---|
| `*registry.RegistryClient` missing `GetVersions` → whole CLI won't compile | Blocker |
| `TestSecretsIntegration` fails (keyring unavailable headless) | CI red |
| ~37MB binaries + `.pyc` + `test.tar.gz` tracked in git | Repo hygiene |
| No `.gitignore`, no `LICENSE`, no CI | Repo hygiene |
| `Sandbox.EnforcePermissions` is dead code, never called | False security claim |
| `python.go` injects *all* provider keys into *every* package | Secret leak |
| Runtime sets `NEURON_PROVIDER`, `agents/researcher` reads `NEURON_MODEL_PROVIDER` | Broken provider selection |
| Venv dir `tools_web-search` vs path `venv/tools/web-search` | Broken cross-package calls |
| Registry `/v1/publish` has no auth; `name`/`version` unsanitized into `filepath.Join` — **both fixed 2026-10-18** | Path traversal |
| 1,188-line `main.go`, commands inlined | Velocity risk |

---

## Phase 0 — Stabilize (Days 1–3) — ✅ COMPLETE 2026-09-23

**Goal:** green build, honest repo. Nothing new ships until this is done.

- [x] Implement `RegistryClient.GetVersions` via `GET /v1/packages/{name}/versions` (server endpoint already existed).
- [x] Fix `TestSecretsIntegration`: skip when no keyring backend is available; store under the bare key name.
- [x] Add `.gitignore`; `git rm --cached` all binaries, `__pycache__`, `test.tar.gz`.
      *Landed in parallel via the merged `phase-0/repo-hygiene` PR, whose `.gitignore` is more
      complete than the draft here.*
- [x] `LICENSE` settled **2026-09-28**: relicensed to **MIT** to match the README (human decision).
- [x] CI workflow written **and live 2026-09-28**. The first push was blocked by a token without
      `workflow` scope; it landed with the docs/Pages work and is green (run 36406070988). The
      release workflow added 2026-10-18 reuses the same build/vet/`-race` checks.
- [x] Delete `cli/pkg/runtime/sandbox.go` (dead) and drop the "sandboxed runtime" claim from the README and CLI help.
- [x] Stop injecting every provider key into every package — provider credentials are now gated on declared `env:` permissions.
- [x] Add `neuron version` (and `--version`).

**Extra defects found and fixed during Phase 0** (surfaced once the tree actually compiled):

- `pkg/executor/executor.go` — `fmt.Errorf` had a `%w` with a missing argument.
- `pkg/installer/installer_test.go` — referenced a `Lockfile` type that had moved; `MockRegistry` was
  missing the new `GetVersions` method.
- `pkg/lockfile` had no tests — the orphaned lockfile test was relocated there and made hermetic.

**Acceptance — MET:** `go build ./...`, `go vet ./...`, and `go test ./...` are green for both `cli/`
and `registry/`; the repo tracks no binaries; the README makes no claim the code doesn't enforce.

**Committed and pushed 2026-09-23** as `feat: Phase 0 + Phase 1 — …`, rebased onto the remote's
merged `phase-0/repo-hygiene` PR. The commit also folds in the pre-existing uncommitted WIP
(`executor/`, `lockfile/`, `workflow/`, `ROADMAP.md`, and the local-only `neuron init` commit) that
this work builds on.


---

## Phase 1 — The free hook: cross-client MCP control — ✅ CORE SHIPPED 2026-09-23

**Goal:** one command configures a server everywhere; secrets never touch JSON.

- [x] Rewrote `cli/pkg/mcp` into a real client registry: **Claude Code, Claude Desktop, Cursor,
      Cline, Windsurf, VS Code, Zed** — per-OS config paths, three config formats.
- [x] Atomic config writes (temp file + fsync + rename), one-time `.neuron.bak` backup, unknown keys
      preserved — including numbers via `json.Number`, so we never reformat a field we don't own.
- [x] Commands: `neuron mcp add|remove|list|sync|doctor` plus a hidden `neuron mcp run` launcher.
- [x] `neuron secrets set|get|rm|list`; secret references resolve at launch; **no secret value is
      ever written to a client config**.
- [x] `neuron mcp doctor` — validates every config, reports missing secrets and commands not on PATH.
- [x] `neuron install` now registers a package's `mcp_server` block into the store and syncs it.
- [x] Tests: 11 tests in `pkg/mcp` covering detection, round-trip preservation, backup-once,
      materialisation, all three client formats, idempotent sync, removal, store round-trip,
      secret injection, and doctor.
- [x] Working install path: `scripts/install.sh` (builds from source, verified end to end).

**Deviation, recorded honestly:** server definitions live in Neuron's own store at
`~/.neuron/mcp/servers.json` rather than in `neuron.json` manifests. A manifest-shaped definition is
still the right long-term format, but the store was the faster path to a working hook and it keeps
`mcp` independent of the package registry.

**Not done (deferred, needs accounts/publishing):** `goreleaser`, GitHub Releases, Homebrew tap,
`curl | sh`, npm shim. `scripts/install.sh` covers source installs today.

**Acceptance — PARTIALLY MET:** `neuron mcp add` writes correctly to every detected client and
`grep` finds no secret value in any config (verified end to end in an isolated `$HOME`). The
"100 real installs" bar requires the human to publish and launch; it cannot be met from this session.

**Evidence:**
```
$ HOME=$TMP go run ./cmd/neuron mcp add gh --command npx --arg=-y \
    --arg=server-github --secret GITHUB_TOKEN=gh-token
✓ Added gh to 2 client(s)

$ cat $TMP/.cursor/mcp.json
{ "mcpServers": { "gh": { "args": ["mcp","run","gh"], "command": "neuron" } } }

$ grep -q gh-token $TMP/.cursor/mcp.json && echo LEAK || echo "PASS: no secret value in config"
PASS: no secret value in config

$ HOME=$TMP go run ./cmd/neuron mcp doctor
✗ gh: secret "gh-token" (for GITHUB_TOKEN) is not set
```


---

## Phase 2 — The paid hook: local proxy + observability — ✅ CORE SHIPPED 2026-09-23

**Goal:** the demo that makes people say "I need this."

- [x] `internal/proxy`: transparent MCP JSON-RPC proxy over stdio. Bytes are forwarded verbatim —
      including the absence of a trailing newline — so client behaviour cannot change.
- [x] `neuron mcp wrap <name>` / `neuron mcp unwrap <name>`: force a server through the proxy.
      `neuron mcp run <name>` is the single seam; it both resolves secrets and proxies traffic.
- [x] Capture per call: method, tool name, args, result, error, duration, token estimate.
- [x] Local history store at `~/.neuron/history.jsonl`, capped at 8 MB with rotation.
- [x] `neuron ui`: local dashboard (live call stream, per-server stats, errors, token totals).
- [x] Redaction of credential-shaped arguments (`token`, `secret`, `api_key`, `authorization`, …)
      before anything is stored, plus a hard requirement that recording is best-effort.

**Deviations, recorded honestly:**

- **JSONL instead of SQLite.** `~/.neuron/history.jsonl` needs no new dependency and reads fine for
  a local dev tool. SQLite remains the right call once the dashboard needs real querying.
- **No health checks / auto-restart yet.** The proxy reaps the child and forwards its stderr, but it
  does not restart a crashed server. Deferred rather than half-built.
- **`neuron proxy` as a standalone command was not added**; `neuron mcp run` is the seam that
  clients actually call, so a second entry point would have been redundant.

**Acceptance — MET, with one caveat:** calls are captured and displayed with latency, result and
token totals, and the proxy is byte-transparent. Routing a *real* Claude Code session through it has
not been exercised from this session — that needs the human at a machine with the client installed.

**Evidence (end to end, isolated `$HOME`):**

```
$ neuron mcp add echo --command ./fake-mcp.sh
$ neuron mcp wrap echo
✓ echo now runs through the Neuron proxy — see `neuron ui`

$ printf '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"ping","arguments":{"token":"secret123"}}}\n' \
    | neuron mcp run echo
{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"pong"}]}}   # forwarded verbatim

$ cat ~/.neuron/history.jsonl
{"time":"…","server":"echo","method":"tools/call","tool":"ping",
 "args":"{\"arguments\":{\"token\":\"[redacted]\"},\"name\":\"ping\"}",
 "result":"{\"content\":[{\"type\":\"text\",\"text\":\"pong\"}]}","status":"ok",
 "duration_ms":0,"tokens_in":13,"tokens_out":11}
PASS: no secret value in history

$ neuron ui --port 7799 &  curl -s localhost:7799/api/stats
[{"server":"echo","calls":1,"errors":0,"avg_ms":0,"tokens_in":4,"tokens_out":11}]
```

**Tests:** 8 tests in `internal/proxy` — verbatim forwarding (terminated and unterminated), real
subprocess proxying, string-id matching, error capture, notification filtering, redaction, history
record/recent/stats/rotation behaviour, and the dashboard endpoints.

---

## Phase 3 — Paid tier + sync service — 🟡 CODE COMPLETE 2026-09-23, LAUNCH PENDING

**Goal:** first revenue.

- [x] Cloud service (`cloud/`): device auth, encrypted blob sync, rate limiting, Stripe webhook
      verification, plan state. Self-hostable and dependency-free.
- [x] End-to-end encrypted sync: client-side key derivation, AEAD envelopes bound to the server name
      and version, and conflict detection that never overwrites.
- [x] `neuron login` / `logout` / `sync`, with sync gated behind the paid plan.
- [x] `NEURON_SYNC_TOKEN` + `NEURON_PASSPHRASE` for headless and CI use.
- [x] Team-shared configs: teams with invite codes, per-team E2E keys, membership checks, and
      `neuron team create/join/list/sync`.
- [ ] Launch: Hacker News, Product Hunt, r/ClaudeAI, r/LocalLLaMA, MCP Discord, X, `awesome-mcp`.
- [ ] Docs site + 2-minute demo video.

**Deviations, recorded honestly:**

- **~~PBKDF2, not Argon2id~~ — resolved 2026-10-18.** The blocker is gone:
  `golang.org/x/crypto` installs fine now, so new payloads are sealed with Argon2id at RFC
  9106's second recommended configuration (t=3, m=64 MiB, p=4) as envelope **v2**. v1
  PBKDF2 envelopes remain readable forever — `OpenWithPassphrase` dispatches on the envelope's
  own version — so no synced account is stranded by the upgrade. Derivation parameters travel
  inside the envelope and are therefore attacker-controlled, so they are capped (8 MiB–1 GiB
  memory, t ≤ 16, p ≤ 32, PBKDF2 iterations ≤ 10M): a hostile blob is an error, not an OOM.
  Tests cover the v1 read path, the v1 wire format staying byte-identical, cross-KDF rejection
  and the parameter caps.
- **~~No Stripe Checkout creation~~ — code done 2026-10-18; going live still needs a key.**
  `POST /v1/billing/checkout` mints a subscription Checkout Session and `neuron upgrade` prints the
  URL, so Pro is now purchasable end to end in code. The plan still flips only from a
  signature-verified webhook, and an account already on Pro gets 409 rather than a second
  subscription. What remains is the human's: create the price in Stripe and set `STRIPE_SECRET_KEY`
  + `STRIPE_PRICE_ID` on the deployed service.
- **~~No browser approval page~~ — done 2026-10-18.** The service now serves a real page at the
  `/activate` URI it advertises: paste the code (plus the account id on the very first device), or
  open a one-time approval link. Links are minted by a signed-in machine via
  `POST /v1/device/approvals` (`neuron login --approve <code> --link`), are single-use, expire with
  the device code, and carry the account — so a browser never handles a session token or an account
  id. `POST /v1/device/approve` now also accepts a session instead of an account id. The terminal
  paths (`--approve`, `--auto-approve`) still work.
- **~~Team sharing uses a shared passphrase~~ — wrapped per member device 2026-10-18.** Each device
  has an X25519 keypair; the random team key is sealed to each device's public key (ephemeral ECDH →
  HKDF-SHA256 → AES-256-GCM) and the service stores only sealed blobs. `team create`/`join`/`sync`
  use this by default; any member grants the key to a newcomer on their next sync, so nobody shares
  a secret. `neuron team rotate` re-wraps a fresh key for every device. Rotation does not re-encrypt
  blobs shared earlier, and there is no `team remove` yet — both stated in `docs/sync.md`.

**Acceptance — NOT MET, and not reachable from here.** "1,000 free users, first paying customer" is
a go-to-market outcome, not a code outcome. The code is ready for it; the launch is the human's.

**Evidence (two machines, isolated `$HOME`, against the real service over HTTP):**

```
$ neuron sync                        # machine A
✓ Pushed: demo

$ neuron sync                        # machine B, same passphrase
✓ Pulled: demo
$ cat ~/.neuron/mcp/servers.json
{ "demo": { "command": "echo", "args": ["hi"] } }

$ neuron sync                        # machine B again
→ Everything is already in sync.

$ grep '"command"' <server data>     # the service only ever holds ciphertext
PASS: no plaintext command in server store

$ neuron sync                        # fresh machine, wrong passphrase
✗ Sync failed: decrypt secretive: decryption failed: wrong passphrase, …
PASS: wrong passphrase rejected
PASS: nothing written on failure

$ neuron team create acme            # owner publishes
Invite code: 2CFT-TUCA
$ neuron team sync acme
✓ Pushed: shared-github

$ neuron team join 2CFT-TUCA         # teammate pulls
$ neuron team sync acme
✓ Pulled: shared-github

$ curl .../v1/teams/<id>/blobs       # a non-member
HTTP 403
PASS: non-member refused
PASS: no plaintext command in the team store
```

**Tests:** 30 in `cli/internal/sync` and 32 in `cloud/`, covering the crypto envelope, AAD binding
(per-server and per-team), tamper rejection, the conflict path, token hashing, blob version
monotonicity, account and team isolation, team membership enforcement, rate limiting, and webhook
signature / rotation / replay handling.

---

## 2026-09-28 — Housekeeping session (docs, CI, license, branch triage)

Not a roadmap phase; recorded so the state is unambiguous.

- [x] **Relicensed to MIT.** The `LICENSE` file was Apache-2.0 while the README said MIT; the human
      chose MIT. `LICENSE` and the README now agree.
- [x] **CI written and green** — `.github/workflows/ci.yml` builds, vets and `-race`-tests all three
      modules (`cli`, `registry`, `cloud`) on push to `main` and on PRs. Passing on GitHub Actions
      (run 36406070988).
- [x] **Docs site written and live** under `docs/` (getting started, MCP, secrets, observability,
      sync, registry, manifest reference, CLI reference, architecture), published to GitHub Pages by
      `.github/workflows/pages.yml` at <https://ranscky.github.io/neuron/>.
- [x] **Corrected false claims** in `cli/ARCHITECTURE.md`: the deleted sandbox and the unenforced
      `http` permission were still described as enforced.
- [x] **`phase-1/registry-auth` abandoned (human decision).** The branch is based on `4b9781d`,
      *before* Phases 1–3, so a merge would have reverted the proxy, sync, cloud and MCP work. It also
      does not compile: it imports `pkg/auth`, `handlers.NewAuthHandler` and
      `store.MigrateLegacyPackages`, none of which exist on the branch, and its CLI registry-client
      API conflicts with the shipped one. Registry auth (API keys, org-scoped storage) will be
      **rebuilt from scratch** as its own scoped effort rather than resurrected from this branch. The
      branch was **deleted from the remote on 2026-10-18** (cleanup); it pointed at `f816ef63` with
      `f079f7bf` beneath it, so it can be restored with
      `git push origin f816ef63:refs/heads/phase-1/registry-auth` if it is ever wanted.

---

## 2026-10-18 — Distribution, crypto, and the purchase path

- [x] **Release pipeline.** `.github/workflows/release.yml` builds `neuron` for linux/darwin/windows
      on amd64+arm64 (CGO-free), gates the release on the same build/vet/`-race` test checks as CI,
      publishes checksummed archives, and attaches a rendered Homebrew formula (`neuron.rb`) to the
      release. Cut one with `git tag -a v0.1.0 -m "neuron 0.1.0" && git push origin v0.1.0`.
      All six targets were cross-built and the workflow's shell steps (version resolution,
      packaging, checksums, formula rendering) were executed locally against the artifacts.
- [x] **A real installer.** `scripts/install.sh` now downloads the release binary and verifies it
      against `checksums.txt` instead of requiring a Go toolchain. It supports `--version`,
      `--prefix`, `--source`, `--dry-run` and `--no-verify`, and fails closed when a release has no
      checksums. The README no longer advertises the dead `get.neuron.ai` URL.
- [x] **Homebrew tap, prepared.** `scripts/publish-tap.sh` copies a release's rendered formula into
      `ranscky/homebrew-neuron`, creating the repo on first run. Deliberately not wired into CI:
      creating a repository should stay a human decision.
- [x] **Argon2id (envelope v2)** — see the deviation note under Phase 3; v1 blobs still decrypt.
- [x] **Checkout** — `POST /v1/billing/checkout` + `neuron upgrade`; see the deviation note.
- [x] **Registry publishing is now authenticated and org-scoped.** `/v1/publish` requires a bearer
      API key; each key belongs to one org, and an org may only publish under its own prefix
      (`acme/web-search`). Keys are stored as SHA-256 hashes and shown once, at creation. Org
      creation is gated by `NEURON_REGISTRY_ADMIN_TOKEN` and answers 503 when unset, so an
      unconfigured instance cannot be claimed by whoever reaches it first. `POST /v1/keys` mints
      more keys (CI, rotation), `neuron publish --token` / `NEURON_REGISTRY_TOKEN` supplies one, and
      reads stay public. Rebuilt from scratch — the abandoned `phase-1/registry-auth` branch was
      never reused. Not yet covered: key revocation.
- [x] **Package names and versions are validated before they become paths.** The Phase 0 audit
      flagged `name`/`version` going unsanitised into `filepath.Join`; publish now accepts only
      lowercase `<org>/<package>` and a plain version, `..` is refused anywhere, and `FileStore.Save`
      re-checks both so a caller cannot bypass the HTTP layer. Tests cover traversal attempts both
      through the handler and straight at the store.
- [x] **Sync tests now run on Windows.** `os.UserHomeDir` reads `USERPROFILE`, not `HOME`, so the
      old `t.Setenv("HOME", …)` isolation silently did nothing there and seven engine/team/auth
      tests failed (and leaked state between each other). Tests now isolate both variables, and the
      POSIX-mode assertion skips on Windows.

**Section D (Phase 5 groundwork), also 2026-10-18:** local policy enforcement (`docs/policy.md`),
audit export, per-device team key wrapping with rotation, OIDC-backed device approval, and the remote
(HTTP/SSE) MCP transport. Each item's roadmap entry records what it does *not* cover.

**Known gaps after this session:** no release or tag has been pushed yet, the tap repo does not
exist yet, and the demo video is still missing. (The Windows `pkg/installer.TestInstaller` failure
recorded in the previous entry was fixed on 2026-10-18 — see below.)
From section D specifically: policy is local only (not served from `cloud/`), team rotation does not
re-encrypt existing blobs and there is no `team remove`, remote servers are proxied but not hosted,
remote-endpoint credentials are not injected, and registry API keys have no revocation endpoint.

**2026-10-18 — Python environments fixed (the `TestInstaller` failure).** Two real bugs, not an
environment quirk: venv paths were POSIX-only (`bin/python3`, `bin/pip`), so on Windows a good
environment was never found and `python -m venv` was re-run into the half-built one forever — the
"Unable to copy … venvlauncher.exe" error; and the test set only `HOME`, which Windows ignores
(`os.UserHomeDir` reads `USERPROFILE`), so it asserted against a directory the installer never wrote
to. Environments are now located per platform, rebuilt with `--clear` when the interpreter inside
does not run, and skip pip entirely when a package has no `requirements.txt`. Interpreter selection
tries `NEURON_PYTHON`, `python`, `py -3`, `python3` on Windows and `python3`, `python` elsewhere.
`go test ./...` is green on Windows for the first time.

---

## Phase 4 — Conversion & retention (Weeks 9–12)

**Goal:** $1–3k MRR.

- [ ] Opt-in anonymous telemetry; instrument the funnel end to end.
- [ ] Identify the single feature that converts free → paid, and double down on it.
- [ ] Replay/diff, cost alerts, team features.
- [ ] Onboarding polish, changelog, support channel.

**Acceptance:** 50–150 paying users, $1–3k MRR, monthly churn < 5%.

---

## Phase 5 — Gateway foundation (Weeks 13+, into 2027)

**Goal:** the enterprise on-ramp, built on the same proxy.

- [x] **Policy engine: per-server allow/deny, scopes, rate limits** — local enforcement shipped
      2026-10-18 (`docs/policy.md`). The proxy refuses a `tools/call` before forwarding it, answers
      with `-32001`/`-32002`, and records the block in the history; `neuron policy show|check`
      explains decisions. Still to come for the gateway: the same policy served from `cloud/` so it
      applies to a team, not just one machine.
- [x] **Audit log export** — `neuron audit export` shipped 2026-10-18. JSONL or CSV, filtered by
      `--since`/`--until` (duration or RFC3339), `--server`, `--tool` and `--status`, to stdout or a
      file with `0600` permissions. It exports exactly what was recorded (already redacted and
      truncated), so policy blocks and crash-abandoned calls appear as reasoned `error` rows.
      Forwarding to a SIEM or an HTTP sink is still to come.
- [x] **Remote MCP servers (client side)** — shipped 2026-10-18. A server registered with `--url` is
      reached over HTTP through the same session as a local one, so policy and recording apply
      unchanged: streamable HTTP (POST, JSON or SSE response, `Mcp-Session-Id` echoed) and the legacy
      SSE transport (GET stream announces a POST endpoint). Transport failures answer the request with
      `-32000` rather than hanging. **Hosting** a remote server on `cloud/` is still to come, as is
      injecting credentials for a remote endpoint.
- [x] **SSO / OIDC scaffolding** — shipped 2026-10-18. `/v1/auth/oidc/start` and `/v1/auth/oidc/callback`
      implement authorization-code + PKCE (S256) against any configured issuer, verify the ID token's
      RS256 signature against the provider's JWKS, check issuer/audience/expiry/nonce, link by verified
      email, and approve the waiting device code; the `/activate` page offers it. RS256 and
      `client_secret_post` are the documented limits. Remaining for a gateway: group/role claims mapped
      to policy, and provider-initiated logout.

**Acceptance:** a design partner can enforce a policy across a team.

---

## Scoreboard (reviewed weekly)

| Metric | Phase 1 | Phase 2 | Phase 3 | Phase 4 |
|---|---|---|---|---|
| Free installs | 100 | 400 | 1,000 | 2,500 |
| Weekly active users | — | 100 | 400 | 1,000 |
| Paying users | 0 | 0 | 1 | 50–150 |
| MRR | $0 | $0 | $20 | $1–3k |
| Servers managed / user | 1 | 3 | 4 | 5+ |

---

## Risks

| Risk | Mitigation |
|---|---|
| Free incumbents (MCP Inspector, Docker, ToolHive) | Win on cross-client + local-first + debugging breadth, not on price |
| Devs won't pay $20/mo | Validate by week 8; the paywalled feature must be observability, not config sync |
| AI makes the code copyable | Moat is distribution + client breadth + polish, not the code |
| 14 weeks is short | Phase 0–1 are the only committed deliverables; later phases adapt to traction |
| Solo bandwidth | Ruthless scope: one workflow per phase, no speculative features |

## Kill criteria (binding)

- **Phase 0:** not green by day 3 → stop and fix, ship nothing.
- **Phase 1:** < 100 installs 2 weeks after release → the hook is wrong; re-cut it, don't add features.
- **Phase 3:** < 2% free→paid 4 weeks after Pro launches → the paywall is on the wrong feature.
- **Any phase:** 2 consecutive weeks with no shipped artifact → the scope is too big; cut it.

---

## Definition of done

A phase is done only when: the artifact is usable by a stranger from the docs alone; tests cover the
new behavior; CI is green; the README is accurate; and the acceptance criteria above are met with
evidence (numbers, logs, or a demo), not assertion.
