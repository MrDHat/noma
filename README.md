# Noma

Run your existing coding agents in isolated, reproducible remote development environments. Bring your own harness, configuration, and infrastructure.

Noma is under development. The first working command validates `noma.yaml` and prints the effective configuration as JSON. Workspace creation, the control plane, and agent execution are not implemented yet.

## Inspect a configuration

With Go 1.26 installed, run these commands from the repository root.

```sh
go run ./cmd/noma config --file examples/noma.yaml
go run ./cmd/noma config --file examples/noma.yaml --profile fullstack --set agent=codex
```

The second command selects the `fullstack` repository group and changes the agent for that invocation. The source file stays unchanged.

The default file is `noma.yaml` in the current directory. Help works without a configuration file.

```sh
go run ./cmd/noma --help
go run ./cmd/noma config --help
```

See the [configuration reference](docs/configuration.md) for fields, overrides, and errors. See the [component boundaries](docs/architecture.md) for the monorepo layout and remaining milestones.

## Check a change

```sh
go test -race ./...
go vet ./...
sh scripts/verify-cli.sh
```

The verification script builds and runs the CLI. It checks JSON output, profile selection, typed overrides, invalid inputs, and unchanged source files. It uses Go and a POSIX shell.
