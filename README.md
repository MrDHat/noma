# Noma

Run your existing coding agents in isolated, reproducible remote development environments. Bring your own harness, configuration, and infrastructure.

Noma is under development. This first change establishes the Go module and CLI entry point. Workspace commands are not implemented yet.

## Run the CLI

With Go 1.26 installed, run this command from the repository root.

```sh
go run ./cmd/noma --help
```

The command prints usage. Unsupported commands fail with a diagnostic on stderr.

## Package boundaries

Noma uses one Go module. `cmd/noma` starts the process. `internal/cli` owns terminal input and output. Configuration, the control plane, provisioning, and the workspace supervisor will join the same module as separate packages when implemented.

The [Notion architecture baseline](https://app.notion.com/p/40617cdd3ca081cd99f5cdde251d5bc3) defines the product direction.

## Check a change

```sh
go test -race ./...
go vet ./...
```
