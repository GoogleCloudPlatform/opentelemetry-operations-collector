# Project

A distrogen project is a project structure for managing your distribution, optionally along with custom components.

## Setup

To get started, you will write a [Distribution Specification](./distribution.md#specification), and use the [`distrogen project` command](./command.md#project). 
(Note that this initial setup will require you to [bootstrap a copy of distrogen](./command.md#installation), but [the `distrogen_version` field](./distribution.md#specification) in the specification will be used to manage future updates automatically).

## Structure

The initial structure will look like this:

```
components/          # Custom components dir
  receivers/         # receiver components
  exporters/         # exporter components
  ...                # any other component types
  registry.yaml      # registry for project components 
  Makefile           # Makefile for component management tasks
make/                # additional make code that is usually imported by other Makefiles
scripts/             # any scripts that may be used during development
templates/           # Custom templates for your distribution generation
<distribution name>/ # The generated distribution, uses `name` field from spec
.distrogen/          # distrogen project metadata
go.mod               # root go module for the project
Makefile             # Makefile for general project management
spec.yaml            # The distribution spec
```

## Makefile

The root Makefile has some common project management operations.

### `gen`

The `gen` target will generate a distribution. `regen` will do a generation with the `--force` flag.
`regen-v` will do a generation with `--force -v` flags.

[Tools](#tool-variables) used: `DISTROGEN_BIN`, `MDATAGEN_BIN`

| Variable | Purpose | Default |
|----------|---------|---------|
| `SPEC_FILE` | The spec file to use for generation. | The spec file used to generate the project. |

### `project-update`

The `project-update` target will update the distrogen project itself using a requested `DISTROGEN_PROJECT_VERSION`.
Please note that this process will overwrite the Makefile, so it may look different after running this target.

| Variable | Purpose | Default |
|----------|---------|---------|
| `DISTROGEN_PROJECT_VERSION` | The version of distrogen to use to update the project. | `latest` |

### `update-otel-components`

The `update-otel-components` target will update all local components to use the versions of
OpenTelemetry Collector dependencies as outlined in the `opentelemetry_version`, `opentelemetry_contrib_version`,
and `opentelemetry_stable_version` fields of the distribution spec.

[Tools](#tool-variables) used: `DISTROGEN_BIN`

### `test-otel-components` and `tidy-otel-components`

These will call `go test` and `go tidy` respectively for all local custom components.

### `workspace`

The `workspace` target will create a `go.work` file with all your local custom components included.

### Tool Variables

Used to provide installations of tools so the Makefile doesn't install them itself. Shared across multiple targets.  
If any of these variables are not provided, the Makefile will install the tool locally into [the `.tools` directory](#tools-directory). (If the tool is already present in the `.tools` directory it won't reinstall)

| Variable | Purpose | Default |
|----------|---------|---------|
| `DISTROGEN_BIN` | Provide a valid `distrogen` binary path. | `.tools/distrogen`. Will install using the `distrogen_version` field from the spec. |
| `MDATAGEN_BIN` | Provide a valid `mdatagen` binary path. | `.tools/mdatagen`. Will install using the `opentelemetry_version` field from the spec. |

#### Distrogen Version Check

When executing targets that depend on `distrogen`, the Makefile compares the version of the installed `DISTROGEN_BIN` against the `distrogen_version` requested in `spec.yaml`. If they differ, it will reinstall the required version.

To bypass this check (e.g., if you want to use a locally built custom binary), you can set the `FORCE_DISTROGEN_VERSION` environment variable to any non-empty value.

#### Tools Directory

All tools are downloaded to a local folder it creates called `.tools`. This folder should be added to a `.gitignore`.

## Custom Components

The primary advantage of using a `distrogen` project is the ability to easily manage custom Collector components. The project structure allows you to:
* Scaffold new components as separate Go modules under the `components/` directory.
* Automatically update OpenTelemetry library dependencies across all of your custom components.
* Maintain a local registry (`components/registry.yaml`) so your distribution can seamlessly discover and build your components.

### Generating a Component

To scaffold a new component, use the [`distrogen component`](./command.md#component) command:

```bash
distrogen component --spec <spec_file> --type <type> --name <name>
```

For example:
```bash
distrogen component --spec spec.yaml --type receiver --name foo
```

This command will:
1. Create a new directory at `components/receiver/fooreceiver` populated with boilerplate files (`Makefile`, `go.mod`, and `metadata.yaml`).
2. Register the new component in `components/registry.yaml` so it is available to your distribution.

> **Note:** Generating a component makes it available, but does not automatically add it to your built distribution. To include it, you must add the component to the `components` section of your `spec.yaml` file and regenerate your distribution.
