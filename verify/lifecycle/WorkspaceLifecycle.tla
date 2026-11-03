-------------------------- MODULE WorkspaceLifecycle --------------------------
\* One workspace's life, from `noma up` to `noma rm`, as four kinds of actor
\* see it: the CLI API, the reconciler (one per noma-server replica), the
\* workspace agent, and the Kubernetes controllers.
\*
\* Each action is one indivisible step in the code: one SQL statement, one
\* Kubernetes API call, or one controller reaction. Comments name the code.
\* hack/verify-lifecycle.sh runs every check; .claude/skills/
\* verify-workspace-lifecycle/SKILL.md says how to read and update it.
EXTENDS Naturals, Sequences, FiniteSets

CONSTANTS
    Reconcilers,   \* noma-server replicas running reconcile.Run at once
    KubeCanFail,   \* a Kubernetes apply or delete can return an error
    MaxCrashes     \* how often the workspace container may crash

None == "none"
\* An absent row, and a reconciler with no write to send. TLC cannot compare
\* a record with a string, so these are records too.
NoRow == [exists |-> FALSE, desired |-> "none", phase |-> "none", msg |-> ""]
NoWrite == [ph |-> "none", msg |-> ""]

\* api.Phase* in internal/api/api.go
AgentPhases == {"cloning", "starting-claude", "sign-in", "ready"}
Phases == {"pending", "starting", "error", "deleting"} \cup AgentPhases

VARIABLES
    row,       \* the workspaces row (store.Workspace), or NoRow
    created,   \* ghost: the API has inserted the row
    sa,        \* the workspace ServiceAccount exists
    sts,       \* the workspace StatefulSet exists
    pvc,       \* the workspace PVC: "none", "bound" or "terminating"
    pod,       \* pod <id>-0: "none", "creating", "running" or "crashing"
    crashes,   \* container crashes so far
    cloned,    \* the repo is on the volume
    loggedIn,  \* Claude's login is on the volume ($HOME is on it)
    todo,      \* statuses the running agent has still to report
    inc,       \* pod incarnation, so a report knows which agent sent it
    inflight,  \* status reports the server has authenticated but not stored
    landed,    \* ghost: phase the agent last stored while this pod runs
    rpc,       \* each reconciler's place in reconcile.pass
    snap,      \* the row as that reconciler's pass read it
    rwrite,    \* the SetPhase that reconciler is about to send
    applyFailed, \* ghost: the latest ensure failed to apply an object
    lock,      \* the reconciler holding the reconcile lock, or None
    changed    \* for each reconciler, the row's updated_at has moved since its pass read it

vars == <<row, created, sa, sts, pvc, pod, crashes, cloned, loggedIn, todo,
          inc, inflight, landed, rpc, snap, rwrite, applyFailed, lock, changed>>

RPCs == {"idle", "read", "unlock", "applySA", "applyPVC", "applySTS", "setError", "getPod",
         "setPhase", "delSTS", "delPVC", "delSA", "goneGetPod",
         "goneGetPVC", "deleteRow"}

Row == [exists: {TRUE}, desired: {"running", "deleted"}, phase: Phases, msg: STRING]

TypeOK ==
    /\ row = NoRow \/ row \in Row
    /\ pvc \in {"none", "bound", "terminating"}
    /\ pod \in {"none", "creating", "running", "crashing"}
    /\ rpc \in [Reconcilers -> RPCs]
    /\ lock \in Reconcilers \cup {None}

\* store.SetPhase: UPDATE ... SET updated_at = now() WHERE id = ? AND
\* desired = 'running'. SetPhaseIfUnchanged adds AND updated_at = ? from the
\* row the pass read: no write has landed since. One statement, so one step.
\* updated_at is a timestamp, so `changed` stands for it: every write to the
\* phase moves it for all reconcilers, and a pass's read clears it.
SetPhaseWhere(ph, m, guard) ==
    IF row.exists /\ row.desired = "running" /\ guard
    THEN /\ row' = [row EXCEPT !.phase = ph, !.msg = m]
         /\ changed' = [r \in Reconcilers |-> TRUE]
    ELSE UNCHANGED <<row, changed>>
SetPhase(ph, m) == SetPhaseWhere(ph, m, TRUE)
SetPhaseIfUnchanged(r, ph, m) == SetPhaseWhere(ph, m, ~changed[r])

\* What noma-agent reports after it starts (agent.Run, setup,
\* superviseClaude, waitForLogin). The clone and login live on the volume, so
\* a restarted pod skips them.
StartReports ==
    (IF cloned THEN <<>> ELSE <<"cloning">>)
    \o <<"starting-claude">>
    \o (IF loggedIn THEN <<>> ELSE <<"sign-in">>)
    \o <<"ready">>

Init ==
    /\ row = NoRow
    /\ created = FALSE
    /\ sa = FALSE /\ sts = FALSE /\ pvc = "none" /\ pod = "none"
    /\ crashes = 0 /\ cloned = FALSE /\ loggedIn = FALSE
    /\ todo = <<>> /\ inc = 0 /\ inflight = {} /\ landed = None
    /\ rpc = [r \in Reconcilers |-> "idle"]
    /\ snap = [r \in Reconcilers |-> NoRow]
    /\ rwrite = [r \in Reconcilers |-> NoWrite]
    /\ applyFailed = FALSE
    /\ lock = None
    /\ changed = [r \in Reconcilers |-> FALSE]

-----------------------------------------------------------------------------
\* CLI API: internal/server/server.go

\* createWorkspace -> insertWorkspace -> store.CreateWorkspace
ApiCreate ==
    /\ ~created
    /\ created' = TRUE
    /\ row' = [exists |-> TRUE, desired |-> "running", phase |-> "pending", msg |-> ""]
    /\ UNCHANGED <<sa, sts, pvc, pod, crashes, cloned, loggedIn, todo, inc,
                   inflight, landed, rpc, snap, rwrite, applyFailed, lock, changed>>

\* deleteWorkspace -> store.MarkDeleted
ApiDelete ==
    /\ row.exists /\ row.desired = "running"
    /\ row' = [row EXCEPT !.desired = "deleted", !.phase = "deleting", !.msg = ""]
    /\ changed' = [r \in Reconcilers |-> TRUE]
    /\ UNCHANGED <<created, sa, sts, pvc, pod, crashes, cloned, loggedIn, todo,
                   inc, inflight, landed, rpc, snap, rwrite, applyFailed, lock>>

-----------------------------------------------------------------------------
\* Reconciler: internal/reconcile/reconcile.go. rpc[r] is where reconciler
\* r is in one pass over this workspace. A pass can start at any time: the
\* API, informers, a retry timer and a 5-minute ticker all wake it.

Go(r, pc) == rpc' = [rpc EXCEPT ![r] = pc]

KubeVars == <<sa, sts, pvc>>
ApplyUnchanged == UNCHANGED <<created, pod, crashes, cloned, loggedIn, todo, inc,
                              inflight, landed, lock>>
RecUnchanged == ApplyUnchanged /\ UNCHANGED applyFailed

\* pass -> store.WithReconcileLock: BEGIN, then pg_try_advisory_xact_lock.
\* A replica that finds the lock taken skips the pass and retries later,
\* which changes nothing, so the model only has the attempt that wins.
RecLock(r) ==
    /\ rpc[r] = "idle" /\ lock = None
    /\ lock' = r
    /\ Go(r, "read")
    /\ UNCHANGED <<row, KubeVars, snap, rwrite, created, pod, crashes, cloned,
                   loggedIn, todo, inc, inflight, landed, applyFailed, changed>>

\* pass: store.Workspaces reads every row in one SELECT. With no row there is
\* nothing to do.
RecStartPass(r) ==
    /\ rpc[r] = "read"
    /\ snap' = [snap EXCEPT ![r] = row]
    /\ changed' = [changed EXCEPT ![r] = FALSE]
    /\ Go(r, CASE ~row.exists              -> "unlock"
                [] row.desired = "deleted" -> "delSTS"
                [] OTHER                   -> "applySA")
    /\ UNCHANGED <<row, KubeVars, rwrite>> /\ RecUnchanged

\* reconcilerPhase: a phase the reconciler set, not one the agent reported.
ErrObjects == "could not create workspace objects"
ReconcilerPhase(s) ==
    \/ s.phase \in {"pending", "starting"}
    \/ s.phase = "error" /\ s.msg = ErrObjects

\* Reconciler.setPhase: no write when the snapshot already says it.
RecWrite(r, ph, m) ==
    IF snap[r].phase = ph /\ snap[r].msg = m THEN NoWrite ELSE [ph |-> ph, msg |-> m]

\* WithReconcileLock: ROLLBACK once the pass is over frees the lock.
RecUnlock(r) ==
    /\ rpc[r] = "unlock"
    /\ lock' = None
    /\ Go(r, "idle")
    /\ UNCHANGED <<row, KubeVars, snap, rwrite, created, pod, crashes, cloned,
                   loggedIn, todo, inc, inflight, landed, applyFailed, changed>>

\* ensure: each server-side apply is one API call. A failed one ends the
\* pass, after setPhase(error) if the snapshot's phase is the reconciler's. Namespace and NetworkPolicy are per
\* owner and never deleted, so the model leaves them out.
RecApply(r, from, to, effect) ==
    /\ rpc[r] = from
    /\ \/ /\ effect
          /\ Go(r, to)
          /\ applyFailed' = (applyFailed /\ to # "getPod")
       \/ /\ KubeCanFail
          /\ LET w == RecWrite(r, "error", ErrObjects)
             IN Go(r, IF ReconcilerPhase(snap[r]) /\ w # NoWrite THEN "setError" ELSE "unlock")
          /\ applyFailed' = TRUE
          /\ UNCHANGED KubeVars
    /\ UNCHANGED <<row, snap, rwrite, changed>> /\ ApplyUnchanged

RecApplySA(r) == RecApply(r, "applySA", "applyPVC", sa' = TRUE /\ UNCHANGED <<sts, pvc>>)
\* Applying to a terminating PVC does not bring it back.
RecApplyPVC(r) == RecApply(r, "applyPVC", "applySTS",
    pvc' = (IF pvc = "none" THEN "bound" ELSE pvc) /\ UNCHANGED <<sa, sts>>)
RecApplySTS(r) == RecApply(r, "applySTS", "getPod", sts' = TRUE /\ UNCHANGED <<sa, pvc>>)

\* ensure: setPhase(ctx, ws, api.PhaseError, errObjects)
RecSetError(r) ==
    /\ rpc[r] = "setError"
    /\ SetPhaseIfUnchanged(r, "error", ErrObjects)
    /\ Go(r, "unlock")
    /\ UNCHANGED <<KubeVars, snap, rwrite>> /\ RecUnchanged

\* observe: one Pods().Get, then a decision on what it returned and on the
\* pass's snapshot. A creating pod is Pending with reason ContainerCreating,
\* which observe skips, so it reports "pod Pending".
RecGetPod(r) ==
    /\ rpc[r] = "getPod"
    /\ LET w == CASE pod = "none"     -> RecWrite(r, "starting", "creating pod")
                  [] pod = "crashing" -> RecWrite(r, "starting", "CrashLoopBackOff")
                  [] pod = "creating" -> RecWrite(r, "starting", "pod Pending")
                  [] pod = "running"  ->
                       IF ReconcilerPhase(snap[r])
                       THEN RecWrite(r, "starting", "starting agent")
                       ELSE NoWrite
       IN /\ rwrite' = [rwrite EXCEPT ![r] = w]
          /\ Go(r, IF w = NoWrite THEN "unlock" ELSE "setPhase")
    /\ UNCHANGED <<row, KubeVars, snap, changed>> /\ RecUnchanged

\* setStarting -> setPhase -> store.SetPhaseIfUnchanged
RecSetPhase(r) ==
    /\ rpc[r] = "setPhase"
    /\ SetPhaseIfUnchanged(r, rwrite[r].ph, rwrite[r].msg)
    /\ Go(r, "unlock")
    /\ rwrite' = [rwrite EXCEPT ![r] = NoWrite]
    /\ UNCHANGED <<KubeVars, snap>> /\ RecUnchanged

\* remove: three deletes; NotFound counts as success, any other error ends
\* the pass. The StatefulSet goes with background propagation.
RecDelete(r, from, to, effect) ==
    /\ rpc[r] = from
    /\ \/ effect /\ Go(r, to)
       \/ KubeCanFail /\ Go(r, "unlock") /\ UNCHANGED KubeVars
    /\ UNCHANGED <<row, snap, rwrite, changed>> /\ RecUnchanged

RecDelSTS(r) == RecDelete(r, "delSTS", "delPVC", sts' = FALSE /\ UNCHANGED <<sa, pvc>>)
RecDelPVC(r) == RecDelete(r, "delPVC", "delSA",
    pvc' = (IF pvc = "bound" THEN "terminating" ELSE pvc) /\ UNCHANGED <<sa, sts>>)
RecDelSA(r) == RecDelete(r, "delSA", "goneGetPod", sa' = FALSE /\ UNCHANGED <<sts, pvc>>)

\* gone: Pods().Get, then PersistentVolumeClaims().Get. Either one still
\* found ends the pass; an informer event wakes the next.
RecGoneGetPod(r) ==
    /\ rpc[r] = "goneGetPod"
    /\ Go(r, IF pod = "none" THEN "goneGetPVC" ELSE "unlock")
    /\ UNCHANGED <<row, KubeVars, snap, rwrite, changed>> /\ RecUnchanged

RecGoneGetPVC(r) ==
    /\ rpc[r] = "goneGetPVC"
    /\ Go(r, IF pvc = "none" THEN "deleteRow" ELSE "unlock")
    /\ UNCHANGED <<row, KubeVars, snap, rwrite, changed>> /\ RecUnchanged

\* remove -> store.DeleteWorkspace: DELETE WHERE id = ?, no other guard.
RecDeleteRow(r) ==
    /\ rpc[r] = "deleteRow"
    /\ row' = NoRow
    /\ Go(r, "unlock")
    /\ UNCHANGED <<KubeVars, snap, rwrite, changed>> /\ RecUnchanged

Reconcile(r) ==
    \/ RecLock(r) \/ RecStartPass(r) \/ RecUnlock(r) \/ RecApplySA(r) \/ RecApplyPVC(r) \/ RecApplySTS(r)
    \/ RecSetError(r) \/ RecGetPod(r) \/ RecSetPhase(r)
    \/ RecDelSTS(r) \/ RecDelPVC(r) \/ RecDelSA(r)
    \/ RecGoneGetPod(r) \/ RecGoneGetPVC(r) \/ RecDeleteRow(r)

-----------------------------------------------------------------------------
\* Kubernetes controllers and the kubelet.

KubeUnchanged == UNCHANGED <<row, created, rpc, snap, rwrite, applyFailed, lock, changed>>

\* The StatefulSet controller creates pod <id>-0. Admission needs the
\* ServiceAccount the pod names.
PodCreate ==
    /\ sts /\ sa /\ pod = "none"
    /\ pod' = "creating"
    /\ UNCHANGED <<sa, sts, pvc, crashes, cloned, loggedIn, todo, inc, inflight, landed>>
    /\ KubeUnchanged

\* The kubelet starts the pod once its claim is bound, and noma-agent starts.
PodStart ==
    /\ pod = "creating" /\ pvc = "bound"
    /\ pod' = "running"
    /\ todo' = StartReports
    /\ inc' = inc + 1
    /\ landed' = None
    /\ UNCHANGED <<sa, sts, pvc, crashes, cloned, loggedIn, inflight>>
    /\ KubeUnchanged

\* The container exits and waits in CrashLoopBackOff; the agent is gone.
PodCrash ==
    /\ pod = "running" /\ crashes < MaxCrashes
    /\ pod' = "crashing"
    /\ crashes' = crashes + 1
    /\ todo' = <<>>
    /\ landed' = None
    /\ UNCHANGED <<sa, sts, pvc, cloned, loggedIn, inc, inflight>>
    /\ KubeUnchanged

PodRestart ==
    /\ pod = "crashing"
    /\ pod' = "running"
    /\ todo' = StartReports
    /\ inc' = inc + 1
    /\ landed' = None
    /\ UNCHANGED <<sa, sts, pvc, crashes, cloned, loggedIn, inflight>>
    /\ KubeUnchanged

\* The garbage collector deletes the pod of a deleted StatefulSet.
PodGC ==
    /\ ~sts /\ pod # "none"
    /\ pod' = "none"
    /\ todo' = <<>>
    /\ landed' = None
    /\ UNCHANGED <<sa, sts, pvc, crashes, cloned, loggedIn, inc, inflight>>
    /\ KubeUnchanged

\* pvc-protection: a deleted claim goes once no pod uses it, with its data.
PVCFinalize ==
    /\ pvc = "terminating" /\ pod = "none"
    /\ pvc' = "none"
    /\ cloned' = FALSE /\ loggedIn' = FALSE
    /\ UNCHANGED <<sa, sts, pod, crashes, todo, inc, inflight, landed>>
    /\ KubeUnchanged

-----------------------------------------------------------------------------
\* Agent: internal/agent/agent.go reports through POST /agent/v1/status,
\* served by AgentServer.status in internal/server/agent.go.

\* The agent sends its next status. AgentServer.authed reads the row
\* (workspaceForToken: it exists and desired = running) before it writes.
\* agent.report retries until the server takes it, so one report is in
\* flight per agent. A report the server has authenticated still lands after
\* the pod that sent it is gone.
AgentSend ==
    /\ pod = "running" /\ todo # <<>>
    /\ ~\E m \in inflight : m.inc = inc
    /\ row.exists /\ row.desired = "running"
    /\ inflight' = inflight \cup {[ph |-> Head(todo), inc |-> inc]}
    /\ todo' = Tail(todo)
    \* the clone finished before "starting-claude", the login before "ready"
    /\ cloned' = (cloned \/ Head(todo) = "starting-claude")
    /\ loggedIn' = (loggedIn \/ Head(todo) = "ready")
    /\ UNCHANGED <<row, created, sa, sts, pvc, pod, crashes, inc, landed, rpc, snap,
                   rwrite, applyFailed, lock, changed>>

\* AgentServer.status: SetSignIn, SetLink and SetPhase, each guarded by
\* desired = running. Only SetPhase moves the phase, so the model writes
\* just that one.
AgentLand ==
    \E m \in inflight :
        /\ SetPhase(m.ph, "")
        /\ inflight' = inflight \ {m}
        /\ landed' = IF row.exists /\ row.desired = "running" /\ pod = "running"
                      THEN m.ph ELSE landed
        /\ UNCHANGED <<created, sa, sts, pvc, pod, crashes, cloned, loggedIn, todo, inc,
                       rpc, snap, rwrite, applyFailed, lock>>

-----------------------------------------------------------------------------

Next ==
    \/ ApiCreate \/ ApiDelete
    \/ \E r \in Reconcilers : Reconcile(r)
    \/ PodCreate \/ PodStart \/ PodCrash \/ PodRestart \/ PodGC \/ PVCFinalize
    \/ AgentSend \/ AgentLand

\* Passes keep coming (the ticker) and controllers keep working. Kubernetes
\* failures are off in the liveness check, so every step is weakly fair.
Fairness ==
    /\ \A r \in Reconcilers : WF_vars(Reconcile(r))
    /\ WF_vars(PodCreate) /\ WF_vars(PodStart) /\ WF_vars(PodGC) /\ WF_vars(PVCFinalize)

Spec == Init /\ [][Next]_vars
FairSpec == Spec /\ Fairness

-----------------------------------------------------------------------------
\* Properties. hack/verify-lifecycle.sh lists which check holds each one.

\* A workspace the user deleted shows "deleting" until its row is gone.
DeleteIsFinal ==
    row.exists /\ row.desired = "deleted" => row.phase = "deleting"

\* Nothing turns a deleted workspace back into a running one.
NoUndelete ==
    [][row.exists /\ row.desired = "deleted" =>
         ~row'.exists \/ row'.desired = "deleted"]_vars

\* Once the row is gone, so is every Kubernetes object of the workspace:
\* no pod keeps running and no volume keeps the user's repo and Claude login.
NoOrphans ==
    created /\ ~row.exists =>
        ~sa /\ ~sts /\ pvc = "none" /\ pod = "none"

\* A deleted workspace's row and objects eventually go.
DeletedGoesAway ==
    (row.exists /\ row.desired = "deleted") ~> (~row.exists /\ NoOrphans)

\* While the agent's pod runs, the phase the CLI shows is the last one that
\* agent reported. observe's comment promises it never overwrites what the
\* agent said.
AgentPhaseKept ==
    pod = "running" /\ landed # None /\ row.exists /\ row.desired = "running"
        => row.phase = landed

\* A running workspace shows "error" only while Kubernetes is failing: once
\* a later pass applies every object again, the error is gone by the end of
\* that pass. The agent's own error reports are outside the model, so every
\* error here is ensure's.
ErrorClears ==
    /\ \A r \in Reconcilers : rpc[r] = "idle"
    /\ row.exists /\ row.desired = "running" /\ row.phase = "error"
    => applyFailed

=============================================================================
