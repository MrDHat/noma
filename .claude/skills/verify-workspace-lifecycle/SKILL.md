---
name: verify-workspace-lifecycle
description: Model-check Noma's workspace lifecycle (create, reconcile, agent status, delete) with TLA+. Use when a change touches internal/reconcile/reconcile.go, internal/store/store.go's row writes, internal/server/server.go's create/delete handlers, internal/server/agent.go's status handler, the agent's status reports in internal/agent/agent.go or signin.go, or how many noma-server replicas run (deploy/dev/server.yaml). Also use when CI's lifecycle sync check fails, or to check a fix for one of the known findings below.
---

# Verify the workspace lifecycle

Three writers race on each workspace: the CLI API, the reconciler, and the workspace agent. They share one row in the `workspaces` table and one set of Kubernetes objects. `verify/lifecycle/WorkspaceLifecycle.tla` models one workspace, and TLC checks every order of their steps. Tests cover only the orders that happen to occur.

## What it promises

| Property | In plain words | Harm if it breaks |
| --- | --- | --- |
| `DeleteIsFinal` | A deleted workspace shows `deleting` until its row is gone. | `noma rm` looks undone, and the CLI shows a phase from before the delete. |
| `NoUndelete` | Nothing sets `desired` back to `running` after a delete. | A deleted workspace comes back and the reconciler recreates it. |
| `NoOrphans` | When the row is gone, the StatefulSet, PVC, ServiceAccount and pod are gone too. | A pod nobody tracks keeps running, and a volume keeps the user's repo and Claude login. The reconciler only visits rows, so it never cleans them up. |
| `DeletedGoesAway` | A deleted workspace's row and objects eventually go. | `noma rm` hangs in `deleting` forever. |
| `AgentPhaseKept` | While the agent's pod runs, the phase is the last one that agent reported. | The CLI shows a stale phase. If `sign-in` is overwritten, `noma up` never prompts, and the agent waits up to 15 minutes. |
| `ErrorClears` | Once a pass applies every object again, an `error` phase is gone by the end of that pass. | A healthy workspace shows `error`, and `noma up` exits with "failed". |

## Run it

```bash
make verify-lifecycle            # or hack/verify-lifecycle.sh
hack/verify-lifecycle.sh rollout # only the checks whose name matches
VERBOSE=1 hack/verify-lifecycle.sh agent-phase   # TLC's full output, with every state
```

You need Java 11 or later. The first run downloads TLC 1.7.4 (`tla2tools.jar`, checksum pinned in the script) to `.dev/`, which is gitignored. Set `TLA2TOOLS=/path/to/tla2tools.jar` to use another copy. All checks take about 10 seconds.

Each check has an expected result in `CHECKS` in `hack/verify-lifecycle.sh`. The script exits 0 only when every check does what it expects.

| Check | Config | Reconcilers | Kubernetes errors | Pod crashes | Expected |
| --- | --- | --- | --- | --- | --- |
| `deletion` | `deletion.cfg` | 1 | yes | 1 | pass: `TypeOK`, `DeleteIsFinal`, `NoOrphans`, `NoUndelete` |
| `deletion-liveness` | `deletion-liveness.cfg` | 1 | no | 1 | pass: `DeletedGoesAway` (weak fairness on passes and controllers) |
| `agent-phase` | `agent-phase.cfg` | 1 | no | 1 | fail `AgentPhaseKept`: finding 3 |
| `stale-error` | `stale-error.cfg` | 1 | yes | 0 | fail `ErrorClears`: finding 2 |
| `rollout` | `rollout.cfg` | 2 | no | 0 | fail `NoOrphans`: finding 1 |

A failing check prints the counterexample's steps by TLA+ action name. To read the path in plain words, run the pstack `/statechart` skill on `verify/lifecycle/WorkspaceLifecycle.tla` with that check's config. It shows the shortest path to each broken rule. When a check passes, `/statechart` draws a chart of how the workspace moves between states. Point it at the same TLC jar with the `TLA2TOOLS` environment variable.

## Known findings

These are bugs in the code, not in the model. Do not weaken a property or the model to make them pass.

1. **A rollout can orphan a deleted workspace's pod and volume** (`rollout`). The Deployment has 1 replica with the default RollingUpdate strategy. During a rollout, the old and new noma-server both run `reconcile.Run`, and the new one runs a pass at startup. The path: reconciler A reads the row while it is still running. The user runs `noma rm`. Reconciler B deletes the objects, sees them gone, and deletes the row. Then A, still in `ensure`, applies the ServiceAccount, PVC and StatefulSet again. No row points at them, so nothing removes them. The pod's agent is refused (`workspaceForToken` finds no row) and retries forever.
2. **One failed Kubernetes apply leaves a running workspace in `error`** (`stale-error`). `ensure` calls `SetPhase(error, "could not create workspace objects")` when any apply fails. On the next pass the applies succeed, but `observe` only rewrites `pending` or `starting` once the pod runs. The phase stays `error` until the agent next reports. On a workspace that is already `ready`, that is only when Claude restarts. The script's trace shows the shorter case, where the error comes during creation. The `ready` case is also reachable: once the agent reports `ready`, one failed apply leaves `error` with nothing left to report.
3. **`observe` can overwrite the agent's phase** (`agent-phase`). The pass reads the pod with `Pods().Get` while it is missing, creating or crash-looping. Before the pass's `SetPhase(starting, …)` lands, the pod starts and the agent reports `cloning`. `setStarting`'s write has no `from` guard, so it replaces `cloning` with `starting`. The window is the time between one Get and one UPDATE, so this is rare. Its worst case is overwriting `sign-in`, which hides the sign-in prompt from `noma up` until the agent's 15-minute sign-in timeout.

When a fix lands, mirror it in the model. The check should then pass. Change its expected result in `CHECKS` to `pass`, and remove the finding from this list.

## Does my change touch this?

Coverage, with the model operators that stand for each piece of code:

| Code | Model |
| --- | --- |
| `internal/store/store.go`: `SetPhase`, `MarkDeleted`, `DeleteWorkspace`, `CreateWorkspace`, `Workspaces` | `SetPhase`, `ApiDelete`, `RecDeleteRow`, `ApiCreate`, `RecStartPass` |
| `internal/server/server.go`: `createWorkspace`/`insertWorkspace`, `deleteWorkspace` | `ApiCreate`, `ApiDelete` |
| `internal/reconcile/reconcile.go`: `Run`, `pass`, `ensure`, `observe`, `setStarting`, `remove`, `gone` | `RecStartPass` … `RecDeleteRow`, `SetStarting` |
| `internal/server/agent.go`: `workspaceForToken`, `status` | `AgentSend` (auth read), `AgentLand` (`SetPhase`) |
| `internal/agent/agent.go`: `Run`, `setup`, `superviseClaude`, `report`; `signin.go`: `signIn`'s sign-in report | `StartReports`, `AgentSend` |
| `internal/reconcile/objects.go`: the pod's ServiceAccount, PVC claim and `HOME` on the volume | `PodCreate`, `PodStart`, `PVCFinalize`, `cloned`/`loggedIn` |
| `deploy/dev/server.yaml`: replica count and rollout strategy | `Reconcilers` in each config |

A change touches the verification when it does any of these:

- adds, removes, reorders or merges a SQL write or Kubernetes call in that code;
- changes a `WHERE` guard;
- changes which phases the agent reports, or in what order;
- changes what `observe` writes;
- adds a new writer of the `workspaces` row, such as a new handler or a background job;
- runs more than one reconciler.

Comment-only changes and changes to the object specs (labels, security context, NetworkPolicy rules) do not.

## Keeping it in step (CI enforces this)

`verify/lifecycle/covered.sha256` holds the SHA-256 of every file in the coverage table, as of the last time someone confirmed the model matches them. The **sync commit** is the last commit that changed that file:

```bash
git log -1 --format=%H -- verify/lifecycle/covered.sha256
```

CI runs `hack/lifecycle-sync.sh check` and `make verify-lifecycle` on every pull request and every push to `main`. The sync check fails when any covered file differs from its recorded hash, even for a change that doesn't touch the model, such as a comment or a NetworkPolicy rule. When it fails:

1. Read what changed since the sync commit. The failing check prints the `git diff` command to run.
2. Decide with the list above whether the change touches the model. If it does, update the model as described below and run `make verify-lifecycle`.
3. Only once you have confirmed that the model describes the code as it is now, run `hack/lifecycle-sync.sh update` and commit `verify/lifecycle/covered.sha256` with your change. A passing TLC run alone does not show that the model matches the code, so don't run `update` just to make CI green.

To cover a new file, add a line for it to `covered.sha256` (any hash) and to the coverage table, then run `update`.

## Updating the model for an intended change

1. Map each changed line to its operator with the coverage table. Keep one action per indivisible step: one SQL statement, one Kubernetes API call, or one controller reaction. When the code splits a write into two statements, split the action too. Merging steps hides interleavings.
2. Update the comment above the action so it names the code it stands for.
3. Run `hack/verify-lifecycle.sh`.
4. For a new promise, add a property at the bottom of the `.tla` file and a check for it in `CHECKS`. Before you trust it, break the guarding code in a scratch copy of the model and watch the check fail.

## Real bug or modeling error?

Read the counterexample (with `/statechart`, or `VERBOSE=1`) and ask, step by step, whether the code can really do this:

- **A real bug:** each step maps to real code, in an order that real concurrency allows. Report it with the path in the code's words. Don't change the model to hide it.
- **A modeling error:** a step the code cannot take. For example, an action that merges calls the code makes separately, or one that skips a guard the code has. Another case is a property that fires mid-pass, at a moment the code repairs within the same pass. `ErrorClears` checks only between passes for this reason. Fix the model so it matches the code, and name the code in the comment.
- `got 'error'` from the script means TLC itself failed, usually a parse error or a bad config. The script prints TLC's whole output.

## Scope and assumptions

Verified: one workspace, all interleavings of the steps above. Bounds: at most 1 container crash (0 in `stale-error` and `rollout`), and 1 reconciler unless a config says 2. A run explores every reachable state within those bounds (about 23,000 for `deletion`).

Not covered:

- several workspaces or owners, and name reuse;
- the namespace and NetworkPolicy, which are per owner and never deleted;
- `SetSignIn` and `SetLink`, which the model folds into `AgentLand`'s `SetPhase` (each has the same `desired = running` guard);
- the sign-in code relay (`server.signIn`, `AgentServer.Send`);
- the agent's own `error` reports;
- Postgres or Kubernetes losing writes;
- informer delays: a pass may start at any time, which covers the wake channel, the retry timer and the 5-minute ticker.

`DeletedGoesAway` assumes that Kubernetes calls eventually succeed and that passes keep coming.
