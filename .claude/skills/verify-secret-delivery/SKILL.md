---
name: verify-secret-delivery
description: Model-check how a changed secret reaches a running workspace's Claude (NOM-49) with TLA+. Use when a change touches noma secret's put/delete handlers or credentialsChanged (internal/server/secrets.go), the agent message queue or long poll (Send, box, messages in internal/server/agent.go), the agent's message loop, credentials refresh or Claude restart (internal/agent/agent.go serve and superviseClaude, internal/agent/credentials.go), or how Claude's command gets its env (internal/agent/claude.go). Also use when CI's credentials-model job fails.
---

# Verify secret delivery

When you change a secret with `noma secret`, noma-server queues a `credentials-changed` message for each of your running workspaces. The agent long-polls for it, fetches its credentials again, and restarts Claude if its environment variables changed. The server, the agent's message loop and Claude's supervisor all act on the same secret, and messages can be lost. `verify/credentials/SecretDelivery.tla` models one workspace and one env secret, and TLC checks every order of their steps.

## What it promises

| Property | In plain words | Harm if it breaks |
| --- | --- | --- |
| `ClaudeGetsLatest` | Once you stop changing the secret, Claude ends up running with the latest one. | Claude keeps using a revoked or wrong secret, and nothing tells you. Restarting Claude doesn't help, because the agent's in-memory copy is old too. |

`TypeOK` is checked in every config too.

## Run it

```bash
make verify-credentials                             # or hack/verify-model.sh credentials
hack/verify-model.sh credentials lost-message       # only the checks whose name matches
VERBOSE=1 hack/verify-model.sh credentials delivery # TLC's full output, with every state
```

You need Java 11 or later. The first run downloads TLC (checksum pinned in `hack/verify-model.sh`) to `.dev/`. The checks take a few seconds.

Each check's expected result is in `verify/credentials/checks`. The script exits 0 only when every check does what it expects.

| Check | Config | Messages lost | Fetches fail | Expected |
| --- | --- | --- | --- | --- |
| `delivery` | `delivery.cfg` | no | no | pass |
| `lost-message` | `lost-message.cfg` | yes | no | fail `ClaudeGetsLatest` |
| `failed-fetch` | `failed-fetch.cfg` | no | yes | fail `ClaudeGetsLatest` |

Each config allows 2 secret changes and a queue of 2. Fairness says the server eventually answers a fetch, the agent's loops keep running, and Claude's restart signal is acted on. Claude crashing and messages getting lost are allowed but never forced.

Read every result with the pstack `/statechart` skill on `verify/credentials/SecretDelivery.tla` and that check's config. It gives a failing check's shortest path in plain words, and a chart of the states when a check passes.

## Known findings

1. **A lost message leaves the workspace on the old secret** (`lost-message`). Path: you change the secret → `AgentServer.messages` takes the message off the queue → the long-poll response never reaches the agent (the connection drops, or noma-server restarts and its in-memory queue is gone) → nothing sends it again. The same happens when `Send` finds the queue full.
2. **A failed fetch leaves the workspace on the old secret** (`failed-fetch`). Path: you change the secret → the agent gets the message → `refreshCredentials`'s `loadCredentials` fails → it logs and returns, and nothing tries again.

In both cases the workspace stays on the old secret until the pod restarts or you change a secret again.

A new finding is a bug in the code, not in the model. List it here with its path, and set its check's expected result to `fail <Property>` in `verify/credentials/checks`. Do not weaken the property or the model to make it pass. When a fix lands, mirror it in the model, set the check to `pass`, and remove the finding from this list.

## Does my change touch this?

| Code | Model |
| --- | --- |
| `internal/server/secrets.go`: `putSecret`, `deleteSecret`, `credentialsChanged` | `YouChangeSecret` |
| `internal/server/agent.go`: `Send` (drops when the queue is full), `box`, `messages` | `YouChangeSecret`, `AgentGetsMessage`, `MessageLost` |
| `internal/agent/agent.go`: `serve` (calls `refreshCredentials` in the message loop) | `AgentGetsMessage` |
| `internal/agent/credentials.go`: `refreshCredentials`, `loadCredentials`, `vault` | `FetchOk`, `FetchFails` |
| `internal/agent/agent.go`: `superviseClaude` (drains `restartClaude`, the run's watcher, `errNewCredentials`), `Run` (loads credentials before Claude) | `ClaudeBegins`, `RestartForNewSecret`, `ClaudeExits`, `Init` |
| `internal/agent/claude.go`: `command` reads `Env` | `ClaudeLaunches` |

A change touches the verification when it:

- adds, removes or reorders a step between a secret write and Claude's restart;
- changes when the agent fetches credentials, or what it does when a fetch fails;
- changes how messages are queued, delivered or dropped;
- changes when `restartClaude` is signalled or drained, or when Claude reads its env.

Git tokens take effect at once through the credential helper and are not modelled. Comment-only changes don't touch it.

## Keeping it in step (CI enforces this)

`verify/credentials/covered.sha256` holds the SHA-256 of every file in the table above, as of the last time someone confirmed the model matches them. The **sync commit** is the last commit that changed it:

```bash
git log -1 --format=%H -- verify/credentials/covered.sha256
```

CI's `credentials-model` job runs `make verify-credentials` and `hack/model-sync.sh credentials check`. The sync check fails when any covered file changed since then, even for a comment. When it fails:

1. Read the diff since the sync commit; the failing check prints the command.
2. Decide with the list above whether the change touches the model. If it does, update the model and run `make verify-credentials`.
3. Only once you have confirmed that the model describes the code as it is now, run `hack/model-sync.sh credentials update` and commit `covered.sha256` with your change. A passing TLC run alone doesn't show that the model matches the code.

`agent.go` is covered by the lifecycle model as well; a change there may need both skills.

## Updating the model

1. Map each changed line to its action with the table. Keep one action per indivisible step: one HTTP request, one channel send or receive, one read of the vault.
2. Update the comment above the action so it names the code.
3. Run `make verify-credentials`.
4. For a new promise, add a property at the bottom of the `.tla` file and a check in `verify/credentials/checks`. Before you trust it, break the guarding code in a scratch copy of the model and watch the check fail. For `ClaudeGetsLatest`, `delivery` fails when `FetchOk` stops signalling a restart, or when `ClaudeLaunches` keeps the old env.

## Real bug or modeling error?

Read the counterexample with `/statechart` (or `VERBOSE=1`) and ask, step by step, whether the code can do this. If every step maps to real code in an order that real concurrency allows, it's a real bug: report it in the code's words. If a step merges calls the code makes separately, or skips a guard the code has, fix the model and name the code in the comment. `got 'error'` means TLC itself failed, usually a parse error; the script prints its output.

## Scope and assumptions

Verified: one workspace, one env secret, every interleaving of the steps above, with at most 2 secret changes and a queue of 2.

Not covered:

- several workspaces, or workspaces that start or stop while you change a secret (`credentialsChanged` sends only to workspaces whose desired state is running; a starting pod loads credentials in `Run`);
- git tokens, which take effect at once through the credential helper;
- which secrets a workspace's rules match (`repoMatches`, tested in `internal/server`);
- several noma-server replicas: each holds its own in-memory queues, so a message queued on one replica never reaches an agent polling another. It behaves like a lost message;
- Claude reading its env more than once per process.
