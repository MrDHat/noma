---- MODULE Teardown ----
\* Taking a Noma install down leaves nothing orphaned (M3.5-18, M3.5-19).
\* make aws-down runs helm uninstall, whose pre-delete hook asks noma-server
\* to close: in one transaction it refuses while any workspace row exists,
\* and otherwise marks the install closing, after which creates are refused.
\* Then aws-down deletes the user namespaces, waits for their volumes to go,
\* and destroys the cluster (NOM-67).
\*
\* The writers: `noma up` creating workspaces, `noma rm` marking them
\* deleted, the reconciler applying and removing their objects, the hook,
\* the CSI driver deleting volumes, and aws-down. Postgres runs at READ
\* COMMITTED: each statement reads what is committed when it runs.
\* Each action is one step in the code; the comment above it names that code.

CONSTANTS
  W,              \* workspaces, each created at most once
  Login,          \* the user's claude-login volume
  Locked,         \* creates and the close take a lock on the install row
  WaitForServer   \* helm uninstall --wait: noma-server is gone before the sweep

VARIABLES
  rows,      \* the workspaces table: w -> "none" | "running" | "deleted"
  closing,   \* the install's committed closing flag
  create,    \* each create's step: "idle" | "read" | "insert" | "done" | "refused"
  shared,    \* creates holding the install row lock FOR SHARE
  excl,      \* the close holds the install row lock FOR UPDATE
  hook,      \* the pre-delete hook: "idle" | "count" | "commit" | "closed"
  snap,      \* the reconciler's pass: w -> the desired state it read, or "none"
  ns,        \* the user's namespace, with its claude-login claim, exists
  objs,      \* workspaces whose StatefulSet and volume claim exist
  vols,      \* volumes the CSI driver has made and not yet deleted
  server,    \* noma-server: "up" | "stopping" | "gone"
  td         \* aws-down: "up" | "uninstalling" | "uninstalled" | "sweeping" | "swept"
             \*   | "destroyed" | "stuck"

vars == <<rows, closing, create, shared, excl, hook, snap, ns, objs, vols, server, td>>

Vols == W \cup {Login}

TypeOK ==
  /\ rows \in [W -> {"none", "running", "deleted"}]
  /\ closing \in BOOLEAN
  /\ create \in [W -> {"idle", "read", "insert", "done", "refused"}]
  /\ shared \subseteq W
  /\ excl \in BOOLEAN
  /\ hook \in {"idle", "count", "commit", "closed"}
  /\ snap \in [W -> {"none", "running", "deleted"}]
  /\ ns \in BOOLEAN
  /\ objs \subseteq W
  /\ vols \subseteq Vols
  /\ server \in {"up", "stopping", "gone"}
  /\ td \in {"up", "uninstalling", "uninstalled", "sweeping", "swept", "destroyed", "stuck"}

Init ==
  /\ rows = [w \in W |-> "none"]
  /\ closing = FALSE
  /\ create = [w \in W |-> "idle"]
  /\ shared = {}
  /\ excl = FALSE
  /\ hook = "idle"
  /\ snap = [w \in W |-> "none"]
  /\ ns = FALSE
  /\ objs = {}
  /\ vols = {}
  /\ server = "up"
  /\ td = "up"

Serving == server # "gone"

----
\* noma up: server.go createWorkspace -> insertWorkspace -> store.CreateWorkspace.

\* The create's transaction begins. With Locked it takes the install row
\* FOR SHARE, and waits while the close holds it FOR UPDATE.
CreateBegin(w) == Serving /\ create[w] = "idle"
  /\ (IF Locked THEN ~excl /\ shared' = shared \cup {w} ELSE UNCHANGED shared)
  /\ create' = [create EXCEPT ![w] = "read"]
  /\ UNCHANGED <<rows, closing, excl, hook, snap, ns, objs, vols, server, td>>

\* It reads the committed closing flag, and refuses if it is set.
CreateRead(w) == Serving /\ create[w] = "read"
  /\ (IF closing
       THEN create' = [create EXCEPT ![w] = "refused"] /\ shared' = shared \ {w}
       ELSE create' = [create EXCEPT ![w] = "insert"] /\ UNCHANGED shared)
  /\ UNCHANGED <<rows, closing, excl, hook, snap, ns, objs, vols, server, td>>

\* It inserts the row and commits, releasing the lock; the server wakes the
\* reconciler.
CreateCommit(w) == Serving /\ create[w] = "insert"
  /\ rows' = [rows EXCEPT ![w] = "running"]
  /\ create' = [create EXCEPT ![w] = "done"]
  /\ shared' = shared \ {w}
  /\ UNCHANGED <<closing, excl, hook, snap, ns, objs, vols, server, td>>

\* noma rm: server.go deleteWorkspace -> store.MarkDeleted.
MarkDeleted(w) == Serving /\ rows[w] = "running"
  /\ rows' = [rows EXCEPT ![w] = "deleted"]
  /\ UNCHANGED <<closing, create, shared, excl, hook, snap, ns, objs, vols, server, td>>

----
\* The reconciler: reconcile.go pass, under the reconcile lock, so one pass
\* runs at a time across replicas.

\* pass reads every row (store.Workspaces).
ReadRows == Serving /\ \A w \in W : snap[w] = "none"
  /\ \E x \in W : rows[x] # "none"
  /\ snap' = rows
  /\ UNCHANGED <<rows, closing, create, shared, excl, hook, ns, objs, vols, server, td>>

\* ensure applies the namespace and the StatefulSet and creates the
\* claude-login and volume claims unless they exist (createClaim), from the
\* row it read, even if the row changed since. The CSI driver makes the
\* volumes.
Ensure(w) == Serving /\ snap[w] = "running"
  /\ ns' = TRUE
  /\ objs' = objs \cup {w}
  /\ vols' = vols \cup {w, Login}
  /\ snap' = [snap EXCEPT ![w] = "none"]
  /\ UNCHANGED <<rows, closing, create, shared, excl, hook, server, td>>

\* remove deletes the StatefulSet and volume claim, and once gone reports
\* the pod and claim gone, deletes the row (store.DeleteWorkspace). The
\* namespace and claude-login claim stay.
Remove(w) == Serving /\ snap[w] = "deleted"
  /\ objs' = objs \ {w}
  /\ rows' = [rows EXCEPT ![w] = "none"]
  /\ snap' = [snap EXCEPT ![w] = "none"]
  /\ UNCHANGED <<closing, create, shared, excl, hook, ns, vols, server, td>>

\* The CSI driver deletes a volume once its claim is gone, while the cluster
\* exists.
CsiDelete(v) == td # "destroyed" /\ v \in vols
  /\ (IF v = Login THEN ~ns ELSE v \notin objs)
  /\ vols' = vols \ {v}
  /\ UNCHANGED <<rows, closing, create, shared, excl, hook, snap, ns, objs, server, td>>

----
\* The chart's pre-delete hook calls noma-server's close endpoint.

\* The close's transaction begins. With Locked it updates the install row
\* first, which waits for every create holding it FOR SHARE.
CloseBegin == Serving /\ td = "up" /\ hook = "idle"
  /\ (IF Locked THEN ~excl /\ shared = {} /\ excl' = TRUE ELSE UNCHANGED excl)
  /\ hook' = "count"
  /\ UNCHANGED <<rows, closing, create, shared, snap, ns, objs, vols, server, td>>

\* It counts the committed rows, deleting ones included. While any exists
\* it rolls back and the hook fails helm uninstall; you can run it again.
CloseCount == Serving /\ hook = "count"
  /\ (IF \E w \in W : rows[w] # "none"
       THEN hook' = "idle" /\ excl' = FALSE
       ELSE hook' = "commit" /\ UNCHANGED excl)
  /\ UNCHANGED <<rows, closing, create, shared, snap, ns, objs, vols, server, td>>

\* It sets closing and commits.
CloseCommit == Serving /\ hook = "commit"
  /\ closing' = TRUE
  /\ hook' = "closed"
  /\ excl' = FALSE
  /\ UNCHANGED <<rows, create, shared, snap, ns, objs, vols, server, td>>

----
\* make aws-down, after the hook passes.

\* helm deletes the release: noma-server's pods start terminating.
Uninstall == td = "up" /\ hook = "closed"
  /\ server' = "stopping"
  /\ td' = "uninstalling"
  /\ UNCHANGED <<rows, closing, create, shared, excl, hook, snap, ns, objs, vols>>

\* noma-server's last pod exits. In-flight transactions roll back and the
\* reconciler's pass ends.
ServerGone == server = "stopping"
  /\ server' = "gone"
  /\ create' = [w \in W |-> IF create[w] \in {"read", "insert"} THEN "refused" ELSE create[w]]
  /\ shared' = {}
  /\ snap' = [w \in W |-> "none"]
  /\ UNCHANGED <<rows, closing, excl, hook, ns, objs, vols, td>>

\* helm uninstall returns; with --wait, only once noma-server is gone.
UninstallDone == td = "uninstalling"
  /\ (WaitForServer => server = "gone")
  /\ td' = "uninstalled"
  /\ UNCHANGED <<rows, closing, create, shared, excl, hook, snap, ns, objs, vols, server>>

\* aws-down deletes the user namespaces, which takes every claim and pod.
Sweep == td = "uninstalled"
  /\ ns' = FALSE
  /\ objs' = {}
  /\ td' = "sweeping"
  /\ UNCHANGED <<rows, closing, create, shared, excl, hook, snap, vols, server>>

\* It waits until the namespaces and every volume are gone.
SweepDone == td = "sweeping" /\ ~ns /\ objs = {} /\ vols = {}
  /\ td' = "swept"
  /\ UNCHANGED <<rows, closing, create, shared, excl, hook, snap, ns, objs, vols, server>>

\* The wait times out: aws-down stops with an error and keeps the cluster.
SweepTimeout == td = "sweeping"
  /\ td' = "stuck"
  /\ UNCHANGED <<rows, closing, create, shared, excl, hook, snap, ns, objs, vols, server>>

\* terraform destroy deletes the cluster; nothing in it runs any more.
Destroy == td = "swept"
  /\ td' = "destroyed"
  /\ server' = "gone"
  /\ snap' = [w \in W |-> "none"]
  /\ UNCHANGED <<rows, closing, create, shared, excl, hook, ns, objs, vols>>

\* Nothing is left to do.
Done == td \in {"destroyed", "stuck"} /\ UNCHANGED vars

Next ==
  \/ \E w \in W : CreateBegin(w) \/ CreateRead(w) \/ CreateCommit(w) \/ MarkDeleted(w)
  \/ ReadRows
  \/ \E w \in W : Ensure(w) \/ Remove(w)
  \/ \E v \in Vols : CsiDelete(v)
  \/ CloseBegin \/ CloseCount \/ CloseCommit
  \/ Uninstall \/ ServerGone \/ UninstallDone \/ Sweep \/ SweepDone \/ SweepTimeout \/ Destroy
  \/ Done

Spec == Init /\ [][Next]_vars

----
\* Once the install is closing, no workspace exists or can be created: the
\* close refused while any did, and every create since was refused.
NoWorkspaceAfterClose == closing => \A w \in W : rows[w] = "none"

\* Once the cluster is destroyed, no volume is left behind to bill.
NothingOrphaned == td = "destroyed" => vols = {}
====
