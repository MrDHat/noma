---- MODULE SecretDelivery ----
\* How a changed secret reaches a running workspace's Claude (NOM-49).
\* One workspace, one env secret. Each action is one step in the code; the
\* comment above it names that code.
EXTENDS Naturals

CONSTANTS
  MaxSecret,  \* how many times you change the secret
  MaxBox,     \* the agent's message queue size (AgentServer.box: 8)
  CanLose,    \* a message can be taken off the queue and never arrive
  CanFail     \* the agent's credentials fetch can fail

VARIABLES
  secret,     \* version of the secret noma-server stores
  box,        \* credentials-changed messages queued for the agent
  kick,       \* the credentialsChanged channel holds a signal
  agent,      \* keepCredentials: "idle" | "fetching"
  vault,      \* version the agent holds in memory (vault)
  pending,    \* the restartClaude channel holds a signal
  claude,     \* superviseClaude: "down" | "starting" | "running"
  claudeEnv   \* version Claude's process was started with

vars == <<secret, box, kick, agent, vault, pending, claude, claudeEnv>>

\* Run loads the credentials before Claude starts.
Init == secret = 0 /\ box = 0 /\ kick = FALSE /\ agent = "idle" /\ vault = 0
     /\ pending = FALSE /\ claude = "down" /\ claudeEnv = 0

\* server/secrets.go putSecret or deleteSecret, then credentialsChanged:
\* AgentServer.Send queues a message, or drops it when the queue is full.
YouChangeSecret == secret < MaxSecret
  /\ secret' = secret + 1
  /\ box' = (IF box < MaxBox THEN box + 1 ELSE box)
  /\ UNCHANGED <<kick, agent, vault, pending, claude, claudeEnv>>

\* AgentServer.messages answers the long poll; serve passes it on with a
\* non-blocking send on credentialsChanged.
AgentGetsMessage == box > 0
  /\ box' = box - 1 /\ kick' = TRUE
  /\ UNCHANGED <<secret, agent, vault, pending, claude, claudeEnv>>

\* keepCredentials takes the credentialsChanged signal and starts
\* refreshCredentials.
FetchOnMessage == agent = "idle" /\ kick
  /\ agent' = "fetching" /\ kick' = FALSE
  /\ UNCHANGED <<secret, box, vault, pending, claude, claudeEnv>>

\* keepCredentials's ticker fires (every credentialRefresh) and starts
\* refreshCredentials.
FetchOnTick == agent = "idle"
  /\ agent' = "fetching"
  /\ UNCHANGED <<kick, secret, box, vault, pending, claude, claudeEnv>>

\* AgentServer.messages takes the message off the queue, but the response
\* never reaches the agent (the connection drops, or noma-server restarts
\* and the in-memory queue is gone).
MessageLost == CanLose /\ box > 0
  /\ box' = box - 1
  /\ UNCHANGED <<kick, secret, agent, vault, pending, claude, claudeEnv>>

\* refreshCredentials: loadCredentials succeeds; if the env changed, a
\* non-blocking send on restartClaude.
FetchOk == agent = "fetching"
  /\ vault' = secret /\ agent' = "idle"
  /\ pending' = (pending \/ vault /= secret)
  /\ UNCHANGED <<kick, secret, box, claude, claudeEnv>>

\* refreshCredentials: loadCredentials fails; it logs and returns, and the
\* next tick tries again.
FetchFails == CanFail /\ agent = "fetching"
  /\ agent' = "idle"
  /\ UNCHANGED <<kick, secret, box, vault, pending, claude, claudeEnv>>

\* superviseClaude, after waitForLogin: drain restartClaude, then start the
\* run and its watcher.
ClaudeBegins == claude = "down"
  /\ claude' = "starting" /\ pending' = FALSE
  /\ UNCHANGED <<kick, secret, box, agent, vault, claudeEnv>>

\* Claude.RemoteControl builds the command, reading vault.env.
ClaudeLaunches == claude = "starting"
  /\ claude' = "running" /\ claudeEnv' = vault
  /\ UNCHANGED <<kick, secret, box, agent, vault, pending>>

\* The run's watcher takes the restartClaude signal and stops the run with
\* errNewCredentials.
RestartForNewSecret == claude \in {"starting", "running"} /\ pending
  /\ claude' = "down" /\ pending' = FALSE
  /\ UNCHANGED <<kick, secret, box, agent, vault, claudeEnv>>

\* Claude crashes or exits; superviseClaude starts it again.
ClaudeExits == claude = "running"
  /\ claude' = "down"
  /\ UNCHANGED <<kick, secret, box, agent, vault, pending, claudeEnv>>

Next == YouChangeSecret \/ AgentGetsMessage \/ MessageLost
     \/ FetchOnMessage \/ FetchOnTick \/ FetchOk
     \/ FetchFails \/ ClaudeBegins \/ ClaudeLaunches
     \/ RestartForNewSecret \/ ClaudeExits

\* The server eventually answers a fetch; the agent's own loops and its
\* ticker keep going.
\* Claude crashing and messages getting lost are never forced.
Fairness == WF_vars(AgentGetsMessage) /\ WF_vars(FetchOnMessage)
         /\ WF_vars(FetchOnTick) /\ SF_vars(FetchOk)
         /\ WF_vars(ClaudeBegins) /\ WF_vars(ClaudeLaunches)
         /\ WF_vars(RestartForNewSecret)

Spec == Init /\ [][Next]_vars /\ Fairness

TypeOK == secret \in 0..MaxSecret /\ box \in 0..MaxBox /\ kick \in BOOLEAN /\ vault \in 0..MaxSecret
       /\ claudeEnv \in 0..MaxSecret /\ agent \in {"idle", "fetching"}
       /\ claude \in {"down", "starting", "running"} /\ pending \in BOOLEAN

\* Once you stop changing the secret, Claude ends up running with the
\* latest one, and stays on it whenever it runs.
ClaudeGetsLatest == <>[](claude = "running" => claudeEnv = secret)
====
