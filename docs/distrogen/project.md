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

#### Tools Directory

All tools are downloaded to a local folder it creates called `.tools`. This folder should be added to a `.gitignore`.

## Custom Components

The biggest strength for a full project setup is for custom components. In your project you can:

* Add code for custom Collector components as separate Go modules in the `components/<type>` subfolder
* Automatically update Collector library dependencies across all components easily
* Manage a local registry that can will allow you to refer to your custom components in your distribution

This feature is under construction and the documentation will be updated when it works properly.