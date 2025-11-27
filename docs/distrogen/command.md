# Command

Distrogen is primarily through a command line interface. It has a hierarchical command structure, so the general usage is `distrogen <subcommand> [flags for subcommand]`.

## Installation

Currently, `distrogen` installation is only possible through `go install`. You must [install Go](https://go.dev/doc/install) and [setup your environment for `go install`](https://thenewstack.io/golang-how-to-use-the-go-install-command/), then you can install `distrogen` with the following command:
```
go install github.com/GoogleCloudPlatform/opentelemetry-operations-collector/cmd/distrogen@latest
```

## `generate` 

The `generate` command generates a new distribution based on a specification file.

| Flag | Shorthand | Description |
|------|-----------|-------------|
| `--spec` | | The distribution specification to use |
| `--force` | `-f` | Force generate even if there are no differences detected |
| `--registry` | | Provide an additional component registry. This is an [array flag](#array-flags).  |
| `--templates` | | Path to custom templates directory |
| `--compare` | | Allows you to compare the generated distribution to the existing |

## `query`

The query command allows you to request the value of a field from a given spec.

| Flag | Shorthand | Description |
|------|-----------|-------------|
| `--spec` | | The distribution specification to use |
| `--field` | | Field to query from the spec. This is  |

## `otel_component_versions`

The `otel_component_versions` command fetches component versions for a given OpenTelemetry version.

| Flag | Shorthand | Description |
|------|-----------|-------------|
| `--otel_version` | | The OpenTelemetry version to fetch component versions for |

## `project`

The `project` command generates a new project based on a specification file.

| Flag | Shorthand | Description |
|------|-----------|-------------|
| `--spec` | | The distribution specification to use |
| `--tools` | | Provide additional tools to install |

## `component`

The `component` command generates a new component. Only used in [project mode](./project.md#custom-components).

| Flag | Shorthand | Description |
|------|-----------|-------------|
| `--spec` | | The distribution specification to use |
| `--type` | | Type of component |
| `--name` | | Name of component |

## `registry`

The `registry` command generates a new components registry. This command has no flags.

## Array Flags

The usage of array flags is to provide one instance of the flag for each additional entry. For example, to provide multiple registries with the `--registry` flag, you provide them like so:
```
distrogen generate --spec x.yaml --registry reg1.yaml --registry reg2.yaml --registry reg3.yaml
```

