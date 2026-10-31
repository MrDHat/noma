# noma

Run your existing coding agents in isolated, reproducible remote development environments. Bring your own harness, configuration, and infrastructure.

## Architecture

Noma prepares and manages remote workspaces. You work with the agent through its own interface.

The initial design targets Claude Code on Kubernetes (EKS) in your AWS account:

```mermaid
flowchart LR
  subgraph laptop[Your laptop]
    admin[noma admin]
    cli[noma CLI]
  end
  phone[Claude app / claude.ai]
  subgraph aws[Your AWS account]
    alb[Load balancer + TLS]
    db[(Postgres)]
    subgraph eks[Kubernetes]
      subgraph sys[noma-system]
        server[Noma server]
      end
      subgraph user["per-user namespace"]
        ws1["Workspace (gVisor sandbox)<br/>noma-agent → Claude Code"]
        ws2["Workspace (gVisor sandbox)<br/>noma-agent → Claude Code"]
      end
    end
  end
  anthropic[(Anthropic)]
  admin -- install --> eks
  cli -- API --> alb --> server
  server --> db
  server -- manages --> user
  ws1 -- dials out --> server
  ws1 -- Remote Control --> anthropic
  phone <--> anthropic
```

- **Admin CLI** provisions AWS infrastructure and installs Noma: cluster, node pools, storage, Postgres, and the policies that constrain every workspace.
- **User CLI** creates and manages workspaces, captures your local agent setup, and relays login requests to your local browser.
- **Noma server** stores users and workspaces in Postgres and reconciles them into Kubernetes resources. It runs with narrow permissions, and admission policies limit what it can create.
- **Workspaces** run one per pod in a gVisor sandbox, each with its own volume, its own Claude login and no inbound network access. The **noma-agent** inside starts and supervises Claude Code and connects out to the server.

Once a workspace is ready, connect through Claude's native Remote Control. The agent and its tools keep running remotely when your laptop disconnects.
