# Component boundaries

Noma uses one Go module. Each component owns a separate package. Executables under `cmd` connect those packages to a process entry point. Shared implementation packages live under `internal`, following the [Go project layout guidance](https://go.dev/doc/modules/layout).

The [Notion architecture baseline](https://app.notion.com/p/40617cdd3ca081cd99f5cdde251d5bc3) defines the product direction. This repository implements the configuration milestone first.

## Implemented components

| Location | Responsibility |
| --- | --- |
| `cmd/noma` | Start the CLI and return its exit status. |
| `internal/cli` | Parse command arguments, read the configuration file, print JSON or diagnostics. |
| `internal/config` | Parse YAML, apply overrides, validate fields, and select repository groups. |
| `scripts/verify-cli` | Build the executable and verify observable CLI behavior. |

`config.Parse` is the configuration boundary. It accepts bytes and options and returns a typed project configuration or an error. File I/O and terminal behavior stay in the CLI package. YAML nodes and editable maps stay private to configuration parsing.

The parser edits a temporary document, then checks the edited data against typed Go fields. It uses [the maintained Go YAML library](https://pkg.go.dev/go.yaml.in/yaml/v3). Exact field spelling and scalar types both require validation. A JSON string field must not silently accept a number from YAML or an override.

## Planned components

| Package boundary | Responsibility |
| --- | --- |
| Control plane | Own user access, workspace records, lifecycle scheduling, and GitHub App token issuance. |
| Provisioning | Allocate compute, construct the environment, and attach persistent workspace storage. |
| Workspace supervisor | Launch and monitor the native agent inside the workspace, report activity, and support reconnecting. |

These components will join the same module as separate packages when they have working behavior. There are no placeholder packages or provider interfaces in this milestone.

The control plane remains available when the developer disconnects. The supervisor runs beside the native coding agent. Persistent storage retains repositories and native session state independently of compute. The native coding agent continues to own its tools, MCP processes, and conversation.

## Next validation milestone

The next milestone must exercise one real workspace backend and one native agent. It needs to prove that an agent survives client disconnection, terminal attach reaches the same session, and suspend and resume retain workspace files and supported conversation state.

DevPod remains a provisioning candidate. This configuration milestone does not establish that DevPod supports the required storage lifecycle. GitHub login, repository tokens, automatic MCP authentication bridging, idle detection, and multi-user authorization also remain unimplemented.

## First design choice

We compared YAML-node editing with a private generic document. The generic document is the implementation base because the source file is never rewritten. It keeps selector editing independent of YAML node representation. Typed decoding then checks the edited result before any other component receives it.

The parser rejects YAML aliases and merge keys initially. Supporting them would require defined override semantics for shared nodes. That behavior can be added when a real configuration requires it.
