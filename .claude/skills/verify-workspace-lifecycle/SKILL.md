---
name: verify-workspace-lifecycle
description: Model-check Noma's workspace lifecycle (create, reconcile, agent status, stop, start, image swap, delete) with TLA+. Use when a change touches internal/reconcile/reconcile.go, internal/store/store.go's row writes, internal/server/server.go's create/delete/stop/start handlers, internal/server/agent.go's status handler, the agent's status reports in internal/agent/agent.go or signin.go, or how many noma-server replicas run (charts/noma/templates/server.yaml). Also use when CI's lifecycle sync check fails, or to check a fix for one of the known findings below.
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
| `VolumeKept` | Only a delete loses the volume's data. Stop, start, pod deletion and the image swap never do. M3's strongest promise: stop never loses work. | A stop or restart throws away the user's uncommitted work and Claude state. |
| `StoppedMeansStopped` | A workspace left stopped ends with no pod and shows `stopped`. | A stopped workspace keeps running (and costing), or the CLI shows it running. |
| `StartComesBack` | A workspace left running ends with its pod running on its volume, on the image its `noma.yaml` names. | `noma start` hangs, or the image swap never settles. |

## Run it

```bash
make verify-lifecycle                       # or hack/verify-model.sh lifecycle
hack/verify-model.sh lifecycle rollout      # only the checks whose name matches
VERBOSE=1 hack/verify-model.sh lifecycle agent-phase   # TLC's full output, with every state
```

You need Java 11 or later. The first run downloads TLC 1.7.4 (`tla2tools.jar`, checksum pinned in the script) to `.dev/`, which is gitignored. Set `TLA2TOOLS=/path/to/tla2tools.jar` to use another copy. All checks take about 2 minutes on a laptop.

Each check has an expected result in `verify/lifecycle/checks`. The script exits 0 only when every check does what it expects.

| Check | Config | Reconcilers | Kubernetes errors | Pod crashes | Expected |
| --- | --- | --- | --- | --- | --- |
| `deletion` | `deletion.cfg` | 1 | yes | 1 | pass: `TypeOK`, `DeleteIsFinal`, `NoOrphans`, `VolumeKept`, `NoUndelete` |
| `deletion-liveness` | `deletion-liveness.cfg` | 1 | no | 1 | pass: `DeletedGoesAway` (weak fairness on passes and controllers) |
| `agent-phase` | `agent-phase.cfg` | 1 | yes | 1 | pass: `AgentPhaseKept` |
| `stale-error` | `stale-error.cfg` | 1 | yes | 0 | pass: `ErrorClears` |
| `rollout` | `rollout.cfg` | 2 | no | 0 | pass: `NoOrphans`, `VolumeKept` with the reconcile lock |
| `stop-liveness` | `stop-liveness.cfg` | 1 | no | 1 | pass: `StoppedMeansStopped` (weak fairness, agent included) |
| `start-liveness` | `start-liveness.cfg` | 1 | no | 1 | pass: `StartComesBack` (weak fairness, agent included) |

A failing check prints the counterexample's steps by TLA+ action name. To read the path in plain words, run the pstack `/statechart` skill on `verify/lifecycle/WorkspaceLifecycle.tla` with that check's config. It shows the shortest path to each broken rule. When a check passes, `/statechart` draws a chart of how the workspace moves between states. Point it at the same TLC jar with the `TLA2TOOLS` environment variable.

## Known findings

None: every check passes.

Found while modeling M3's design, before code: if `noma start` were accepted while the old pod is still stopping, that pod's agent (already `ready`) never reports again and the row stays `starting` for good (`AgentPhaseKept`: `ApiStop` then `ApiStart` with the pod still running). `ApiStart` therefore requires `phase = 'stopped'` (decision M3-25); the stop/start code (NOM-38) must keep that guard. A new finding is a bug in the code, not in the model. List it here with its path, and give its check the expected result `fail <Property>` in `verify/lifecycle/checks`. Do not weaken a property or the model to make it pass.

When a fix lands, mirror it in the model. The check should then pass. Change its expected result in `verify/lifecycle/checks` to `pass`, and remove the finding from this list.

## Does my change touch this?

Coverage, with the model operators that stand for each piece of code:

| Code | Model |
| --- | --- |
| `internal/store/store.go`: `SetPhase`, `SetPhaseIfUnchanged`, `MarkDeleted`, `DeleteWorkspace`, `CreateWorkspace`, `Workspaces`, `WithReconcileLock` | `SetPhase`, `SetPhaseIfUnchanged` (`changed` stands for `updated_at`), `ApiDelete`, `RecDeleteRow`, `ApiCreate`, `RecStartPass`, `RecLock`/`RecUnlock` |
| `internal/server/server.go`: `createWorkspace`/`insertWorkspace`, `deleteWorkspace`, `stopWorkspace`, `startWorkspace` | `ApiCreate`, `ApiDelete`, `ApiStop`, `ApiStart` |
| `internal/store/store.go`: `MarkStopped`, `MarkStarted`; `SetImage` (NOM-50) | `ApiStop`, `ApiStart`; `AgentLand`'s image branch |
| `internal/reconcile/reconcile.go`: `Run`, `pass`, `ensure`, `reconcilerPhase`, `observe`, `setStarting`, `setPhase`, `remove`, `gone` | `RecLock` … `RecDeleteRow`, `ReconcilerPhase`, `RecWrite` |
| `internal/server/agent.go`: `workspaceForToken`, `status` | `AgentSend` (auth read), `AgentLand` (`SetPhase`) |
| `internal/agent/agent.go`: `Run`, `setup`, `superviseClaude`, `report`; `signin.go`: `signIn`'s sign-in report | `StartReports`, `AgentSend` |
| `internal/reconcile/objects.go`: the pod's ServiceAccount, PVC claim and `HOME` on the volume; the StatefulSet's replicas (`replicas`) and image (NOM-50) | `PodCreate`, `PodRemove`, `PodStart`, `PVCFinalize`, `cloned`/`loggedIn`, `RecApplySTS` |
| `internal/agent/image.go` (`useImage`, called from `agent.go`'s `Run`): the `noma.yaml` image report (NOM-50) | `StartReports`' `"image"` |
| `charts/noma/templates/server.yaml`: replica count and rollout strategy | `Reconcilers` in each config |

A change touches the verification when it does any of these:

- adds, removes, reorders or merges a SQL write or Kubernetes call in that code;
- changes a `WHERE` guard, or what sets `updated_at`;
- changes which phases the agent reports, or in what order;
- changes what `observe` writes;
- adds a new writer of the `workspaces` row, such as a new handler or a background job;
- runs more than one reconciler, or changes how the reconcile lock is taken or released.

Comment-only changes and changes to the object specs (labels, security context, NetworkPolicy rules) do not.

## Keeping it in step (CI enforces this)

`verify/lifecycle/covered.sha256` holds the SHA-256 of every file in the coverage table, as of the last time someone confirmed the model matches them. The **sync commit** is the last commit that changed that file:

```bash
git log -1 --format=%H -- verify/lifecycle/covered.sha256
```

CI runs `hack/model-sync.sh lifecycle check` and `make verify-lifecycle` on every pull request and every push to `main`. The sync check fails when any covered file differs from its recorded hash, even for a change that doesn't touch the model, such as a comment or a NetworkPolicy rule. When it fails:

1. Read what changed since the sync commit. The failing check prints the `git diff` command to run.
2. Decide with the list above whether the change touches the model. If it does, update the model as described below and run `make verify-lifecycle`.
3. Only once you have confirmed that the model describes the code as it is now, run `hack/model-sync.sh lifecycle update` and commit `verify/lifecycle/covered.sha256` with your change. A passing TLC run alone does not show that the model matches the code, so don't run `update` just to make CI green.

To cover a new file, add a line for it to `covered.sha256` (any hash) and to the coverage table, then run `update`.

## Updating the model for an intended change

1. Map each changed line to its operator with the coverage table. Keep one action per indivisible step: one SQL statement, one Kubernetes API call, or one controller reaction. When the code splits a write into two statements, split the action too. Merging steps hides interleavings.
2. Update the comment above the action so it names the code it stands for.
3. Run `make verify-lifecycle`.
4. For a new promise, add a property at the bottom of the `.tla` file and a check for it in `verify/lifecycle/checks`. Before you trust it, break the guarding code in a scratch copy of the model and watch the check fail.

## Real bug or modeling error?

Read the counterexample (with `/statechart`, or `VERBOSE=1`) and ask, step by step, whether the code can really do this:

- **A real bug:** each step maps to real code, in an order that real concurrency allows. Report it with the path in the code's words. Don't change the model to hide it.
- **A modeling error:** a step the code cannot take. For example, an action that merges calls the code makes separately, or one that skips a guard the code has. Another case is a property that fires mid-pass, at a moment the code repairs within the same pass. `ErrorClears` checks only between passes for this reason. Fix the model so it matches the code, and name the code in the comment.
- `got 'error'` from the script means TLC itself failed, usually a parse error or a bad config. The script prints TLC's whole output.

## Scope and assumptions

Verified: one workspace, all interleavings of the steps above. Bounds: at most 1 container crash (0 in `stale-error` and `rollout`), 1 stop, and 1 reconciler unless a config says 2.

The image swap (M3-17) was modeled before its code and checked against it when NOM-50 landed: the image report changes only the image (not the phase or `updated_at`) and wakes the reconciler, the StatefulSet takes its image from the pass's row, and the agent on the base image reports and then waits for its pod to be replaced. Stop and start are in the code (NOM-38). A run explores every reachable state within those bounds.

Not covered:

- several workspaces or owners, and name reuse;
- the namespace and NetworkPolicy, which are per owner and never deleted;
- removing `noma.yaml` from a repo on a project image: the agent keeps the project image and only logs it;
- `SetSignIn` and `SetLink`, which the model folds into `AgentLand`'s `SetPhase` (each has the same `desired = running` guard);
- the sign-in code relay (`server.signIn`, `AgentServer.Send`);
- an image report from a pod already on the project image (the model's repo always names one project image);
- Claude restarting inside a running pod, after a crash or for new credentials (NOM-49). Each restart reports `starting-claude` and then `ready` again, so the phase is still the agent's last report;
- the agent's own `error` reports. The reconciler tells them from its own error by the message, so the agent must never report `could not create workspace objects`;
- two phase writes in the same microsecond, which leave `updated_at` equal, so `SetPhaseIfUnchanged` cannot tell them apart;
- Postgres or Kubernetes losing writes;
- informer delays: a pass may start at any time, which covers the wake channel, the retry timer and the 5-minute ticker;
- a replica that fails to take the reconcile lock, which skips the pass and changes nothing;
- Postgres dropping the lock's connection while the pass keeps making Kubernetes calls. The lock then no longer excludes the other replica.

`DeletedGoesAway` assumes that Kubernetes calls eventually succeed and that passes keep coming.
