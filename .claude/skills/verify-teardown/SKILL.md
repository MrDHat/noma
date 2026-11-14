---
name: verify-teardown
description: Model-check that taking a Noma install down leaves nothing orphaned (M3.5, NOM-54) with TLA+. Use when a change touches workspace creates (internal/server/server.go createWorkspace, insertWorkspace; internal/store/store.go CreateWorkspace), deletes (MarkDeleted, DeleteWorkspace), the reconciler's pass, ensure or remove (internal/reconcile/reconcile.go), the close endpoint or closing flag (NOM-60), the chart's pre-delete hook, or make aws-down (NOM-67). Also use when CI's teardown-model job fails.
---

# Verify teardown

`make aws-down` runs `helm uninstall`. The chart's pre-delete hook asks noma-server to close: in one transaction it refuses while any workspace row exists, and otherwise marks the install closing, so every later create is refused. aws-down then deletes the user namespaces, waits for their volumes to go, and destroys the cluster. `noma up`, `noma rm`, the reconciler, the hook, the CSI driver and aws-down all act at once. `verify/teardown/Teardown.tla` models one user with two workspaces, Postgres at READ COMMITTED, and TLC checks every order of their steps.

The model came first: it describes the design that NOM-60 (closing state) and NOM-67 (aws-down) build. Both are in the code now: the close and the locked create (NOM-60) and `hack/aws-down.sh` (NOM-67).

## What it promises

| Property | In plain words | Harm if it breaks |
| --- | --- | --- |
| `NoWorkspaceAfterClose` | Once the install is closing, no workspace exists and none can be created. Teardown refuses while any does, deleting ones included. | A workspace created during teardown loses its row with the database and its pod and volume run on with nothing to remove them. |
| `NothingOrphaned` | Once the cluster is destroyed, no volume is left behind. | An EBS volume nothing owns, billed until someone finds it. |

`TypeOK` is checked in every config too.

## Run it

```bash
make verify-teardown                       # or hack/verify-model.sh teardown
hack/verify-model.sh teardown no-lock      # only the checks whose name matches
VERBOSE=1 hack/verify-model.sh teardown    # TLC's full output, with every state
```

You need Java 11 or later. The checks take a few seconds.

| Check | Lock on the install row | `helm uninstall --wait` | Expected |
| --- | --- | --- | --- |
| `teardown` | yes | yes | pass |
| `no-wait` | yes | no | pass |
| `no-lock` | no | yes | fail `NoWorkspaceAfterClose` |
| `no-lock-no-wait` | no | no | fail `NothingOrphaned` |

The two failing checks are why the design takes the lock, not known bugs. Without it, a create and the close race even when each runs in one transaction: at READ COMMITTED the create reads closing as false, the close counts no rows and commits, and then the create commits its row. The lock makes the create take the install row `FOR SHARE` before reading closing, and the close update it before counting. Without `--wait` as well, the old noma-server's reconciler can apply that workspace's volume after aws-down's sweep and before destroy. With the lock, `--wait` is a second guard only.

Read every result with the pstack `/statechart` skill on `verify/teardown/Teardown.tla` and that check's config.

## Known findings

None.

A new finding is a bug in the code, not in the model. List it here with its path, and set its check's expected result to `fail <Property>` in `verify/teardown/checks`. Do not weaken the property or the model to make it pass. When a fix lands, mirror it in the model and set the check back.

## Does my change touch this?

| Code | Model |
| --- | --- |
| `internal/server/server.go`: `createWorkspace`, `insertWorkspace`; `internal/store/store.go`: `CreateWorkspace` | `CreateBegin`, `CreateRead`, `CreateCommit` |
| `internal/store/store.go`: `MarkDeleted` | `MarkDeleted` |
| `internal/reconcile/reconcile.go`: `pass` reads `store.Workspaces` | `ReadRows` |
| `internal/reconcile/reconcile.go`: `ensure` | `Ensure` |
| `internal/reconcile/reconcile.go`: `remove`, `gone`; `internal/store/store.go`: `DeleteWorkspace` | `Remove` |
| `internal/server/server.go`: `closeInstall`; `internal/store/store.go`: `CloseInstall` | `CloseBegin`, `CloseCount`, `CloseCommit` |
| The chart's pre-delete hook (`charts/noma/templates/uninstall-guard.yaml`), `hack/aws-down.sh` | `Uninstall`, `ServerGone`, `UninstallDone`, `Sweep`, `SweepDone`, `SweepTimeout`, `Destroy` |

A change touches the verification when it:

- adds a way to create a workspace row, or changes when a create reads closing or takes the lock;
- changes what the close counts, or when it takes the lock;
- lets the reconciler apply objects for a row it did not read in the same pass, or run passes at once;
- adds a billable object the sweep doesn't delete, or changes what aws-down waits for before destroy.

## Keeping it in step (CI enforces this)

`verify/teardown/covered.sha256` holds the SHA-256 of every covered file, as of the last time someone confirmed the model matches them. CI's `teardown-model` job runs `make verify-teardown` and `hack/model-sync.sh teardown check`, which fails when a covered file changed since. Read the diff it names, update the model if needed, run `make verify-teardown`, and only then run `hack/model-sync.sh teardown update` and commit `covered.sha256`.

These files are covered by the lifecycle model as well; a change there may need both skills.

## Updating the model

Keep one action per indivisible step: one SQL statement or commit, one Kubernetes call. Name the code in the comment above the action. For a new promise, add a property at the bottom of the `.tla` file and a check, and break the guarding code in a scratch copy to see it fail before you trust a pass. When the model was written, counting only running rows in `CloseCount` failed `NoWorkspaceAfterClose`, and `SweepDone` not waiting for volumes failed `NothingOrphaned`.

## Scope and assumptions

Verified: one user, two workspaces each created at most once, every interleaving of the steps above.

Not covered:

- several users: each has their own namespace, and the sweep deletes them all;
- stop and start: a stopped workspace keeps its row and volume, so the close refuses it like any other;
- several noma-server replicas: the reconcile lock keeps passes apart, and each create and the close are their own transactions, so more replicas add no new order;
- Postgres and the release's other objects: helm deletes them, and aws-down deletes noma-system, and so Postgres's claim, with the user namespaces, and waits for every PersistentVolume;
- the volume's EBS disk outliving its PersistentVolume: aws-down's final check for EBS volumes tagged with the cluster catches it;
- how long anything takes: `SweepTimeout` stops aws-down with the cluster kept.
