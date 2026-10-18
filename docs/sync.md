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

`neuron login` prints a short code and the service's activation page. Approve it
one of three ways:

```bash
# in a browser (any device): paste the code, or open an approval link
open http://127.0.0.1:8080/activate

# from another terminal on a signed-in machine
neuron login --approve K7QP-3M2X --account acct_… --server http://127.0.0.1:8080

# ask a signed-in machine for a one-time browser link (approve from your phone)
neuron login --approve K7QP-3M2X --link
```

An approval link is single-use, expires with the device code, and is minted by a
signed-in machine — the browser never handles a session token or an account id.
The account id is only needed for the very first device, where no session exists
anywhere yet.

`neuron sync` prints what it pushed and pulled, and reports
`Everything is already in sync.` when there is nothing to do.

## What is encrypted

Sync is **end-to-end encrypted**. Your passphrase never leaves the machine; the
service stores only AES-256-GCM ciphertext. Keys are derived with Argon2id
(envelope v2); envelopes written by older clients use PBKDF2 (envelope v1) and
still decrypt. Every envelope is bound to the
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

## Sign in with an identity provider

A service can delegate device approval to any OpenID Connect provider, so nobody
has to copy an account id around. Configure the service with:

| Variable | Meaning |
|---|---|
| `NEURON_OIDC_ISSUER` | Provider issuer URL (discovery is read from it) |
| `NEURON_OIDC_CLIENT_ID` / `NEURON_OIDC_CLIENT_SECRET` | Client credentials |
| `NEURON_OIDC_REDIRECT_URL` | Callback URL (default: `<public base>/v1/auth/oidc/callback`) |

All three of issuer, client id and secret are required; without them the
endpoints answer `503` and the activation page hides the option.

With it configured, the activation page shows **Sign in and approve**, which runs
the standard authorization-code flow with PKCE (S256): the service verifies the
ID token's RS256 signature against the provider's published keys, checks issuer,
audience, expiry and nonce, links the identity to an account by **verified**
email, and approves the waiting device code. The CLI polls
`/v1/device/token` exactly as before.

Limits, stated plainly: RS256 only; the client authenticates to the token
endpoint with `client_secret_post`; and an identity is linked by verified email,
so the provider must assert one.

## Subscribing

```bash
neuron upgrade        # prints a Stripe Checkout URL
```

Checkout Sessions are created by the sync service, so it needs these where it
runs:

| Variable | Meaning |
|---|---|
| `STRIPE_SECRET_KEY` | API key used to create Checkout Sessions; unset disables checkout |
| `STRIPE_PRICE_ID` | the recurring price a subscription buys |
| `STRIPE_WEBHOOK_SECRET` | signing secret for the webhook that flips the plan |
| `NEURON_PUBLIC_URL` | origin customers return to after checkout (default: the request's origin) |

The plan changes only when Stripe calls the service's webhook:
`checkout.session.completed`, `customer.subscription.updated` and `invoice.paid`
grant Pro; `customer.subscription.deleted` returns the account to free. A client
that abandons checkout stays on the free plan, and card details never reach the
Neuron service.

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
neuron team rotate platform      # new team key, re-wrapped for every device
neuron team list
```

A team's key is **random and wrapped per member device**. Each device generates
an X25519 keypair; the team key is sealed to its public key (ephemeral ECDH →
HKDF → AES-256-GCM) and the service stores only that sealed blob. Nobody types a
passphrase, and the service cannot read a shared server.

Joining registers your device's public key. Any member who already holds the key
wraps it for you the next time they run `neuron team sync`, so the first sync
after joining needs a teammate to sync first. Shared blobs are bound to the team
as well as to the server name, so a blob cannot be replayed across teams. Any
member can publish; a server changed by two people at once is reported as a
conflict, exactly as in personal sync.

The device identity lives in your OS keychain. Headless machines and CI can
supply it instead with `NEURON_MEMBER_KEY` (the base64 private key).

**Rotation.** `neuron team rotate` generates a new team key and wraps it for
every registered device, so someone who has left cannot read what is shared from
then on. Payloads that already exist stay sealed with the previous key —
re-encrypting history is not done yet, so re-share or delete a server if its old
ciphertext must stop being readable.

**Legacy teams.** A team created with `--passphrase` (or
`NEURON_TEAM_PASSPHRASE`) derives its key from a shared passphrase instead. Those
teams keep working unchanged:

| Where | Personal | Legacy team |
|---|---|---|
| Flag | `--passphrase` | `--passphrase` |
| Environment | `NEURON_PASSPHRASE` | `NEURON_TEAM_PASSPHRASE` |
| Prompt | interactive | interactive |

## Known limitations

- **Rotation does not re-encrypt history.** A new team key protects everything
  synced after `neuron team rotate`, but blobs shared before it stay sealed with
  the previous key. Re-share or delete a server to revoke access to its old
  ciphertext.
- **There is no `team remove` yet.** Rotation is the primitive; removing a
  member's device records is the next step.
- Personal sync covers server definitions and secrets, not call history.
