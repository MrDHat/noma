# Configuration reference

`noma config` reads one YAML document and prints its effective configuration as JSON. The schema is provisional while the first workspace backend is developed.

```sh
noma config --file noma.yaml --profile fullstack --set agent=codex
```

## Fields

| Field | Type | Requirement |
| --- | --- | --- |
| `agent` | String | Required. One of `claude`, `codex`, or `opencode`. |
| `environment.image` | String | Required and nonblank. |
| `environment.setup` | Array of strings | Optional. Each command must be nonblank. |
| `compute.provider` | String | Required and nonblank. |
| `compute.region` | String | Optional. Empty means unspecified. Whitespace-only values fail. |
| `compute.instanceType` | String | Optional. Empty means unspecified. Whitespace-only values fail. |
| `repos` | Array of strings | Optional. Each repository identifier must be nonblank. |
| `profiles` | Map of repository groups | Optional. Each profile has a nonblank name and a nonempty `repos` array. |

Provider settings are declarations. The command does not contact providers or check image, region, instance type, repository existence, or credentials.

## Repository selection

`--profile NAME` replaces the top-level repository list with the named profile's `repos`. Profiles contain only repository groups. They cannot override the agent, environment, or compute.

Without `--profile`, the command uses top-level `repos`. If neither declares repositories, the output omits `repos`. Git-origin resolution belongs to future workspace creation and does not run during configuration inspection.

The output contains the selected configuration. It does not contain the source profile definitions. Every declared profile must be valid, including profiles that the invocation does not select.

## Overrides

Repeated `--set PATH=VALUE` arguments apply in order. The last assignment to a field wins. Overrides affect the configuration in memory and never rewrite the file.

Selectors support dotted keys, zero-based indexes into existing arrays, and JSON-quoted map keys in brackets.

```sh
noma config --set agent=codex
noma config --set compute.instanceType=t3.xlarge
noma config --set 'environment.setup=["go version","go test ./..."]'
noma config --set 'environment.setup[0]=echo key=value'
noma config --profile web.apps --set 'profiles["web.apps"].repos[0]=acme/web'
```

An override can create missing map fields. The final configuration must match the schema. Index assignments replace existing array elements. They cannot append items or create sparse arrays. Replace the whole array to change its length.

Valid JSON values retain their types. Other bare values are strings. Malformed JSON-looking arrays, objects, and quoted strings fail.

```sh
noma config --set environment.image=123
noma config --set 'environment.image="123"'
```

The first command fails because `image` requires a string. The second accepts the string `123`. This validates the value's type, not whether an image with that name exists.

## Rejected input

Unknown fields, wrong field types, unsupported agents, blank required values, duplicate keys, multiple YAML documents, non-string map keys, null values, YAML aliases, merge keys, and unsupported YAML tags fail validation. Field names are case-sensitive.

Malformed selectors, missing profiles, indexes outside an array, and missing files also fail. The CLI prints a diagnostic to stderr and exits nonzero. It prints no JSON on failure.
