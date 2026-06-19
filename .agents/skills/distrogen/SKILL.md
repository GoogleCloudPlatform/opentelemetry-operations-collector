---
name: distrogen
description: Instructions for developing and using the distrogen CLI tool. Use this skill when modifying the distrogen codebase or troubleshooting its behavior.
---

# distrogen

This skill provides context and rules for developing the `distrogen` CLI tool, located in `cmd/distrogen`.

## What is distrogen?

`distrogen` is a command-line tool for creating and managing an OpenTelemetry Collector "distribution". It uses a YAML specification (`spec.yaml`) combined with Go templates to generate scaffolding.

Its primary generation capabilities include:
1. **Distributions:** Generates a folder containing an OpenTelemetry Collector Builder (OCB) config, Dockerfiles, goreleaser config, and Makefiles. This allows you to build a custom Collector binary.
2. **Projects:** Generates an entire workspace around your distribution. This is used when you need to maintain local custom components (receivers, processors, etc.). **This is intended to be used on an empty repo.** It provides a standard directory structure (`components/`, `make/`, `scripts/`, `templates/`) and root Makefiles for project management.
3. **Components:** Scaffolds boilerplate Go modules for new OpenTelemetry components within a project, and automatically adds them to the project's local registry.
4. **Registries:** Manages YAML files mapping logical component names to their Go Module information, docs URLs, and OTel versions.

## Architecture & Code Organization

The tool's source code is located in `cmd/distrogen/`. When making changes, it is critical to adhere to the separation of concerns:

### 1. `main.go` is NOT for Implementation
The `main.go` file is strictly used for command registration, argument parsing, and command orchestration. **CRITICAL RULE: No business logic belongs in `main.go`.** If you are adding a new feature or command, define its arguments here and call out to domain-specific files.

Do **NOT** place business logic or implementation details directly inside `main.go` (e.g., doing heavy lifting inside the `Run()` methods). Instead, place the core domain logic into dedicated domain-specific files:

* **`distribution.go`**: Contains `DistributionSpec` and `DistributionGenerator`. It handles parsing/validating the user's `spec.yaml` and generating the actual collector distribution code (OCB manifest, main.go, etc.).
* **`project.go`**: Contains `ProjectGenerator`. It generates the surrounding project infrastructure (e.g., `make` targets, scripts, `.distrogen` metadata cache) needed to build and manage a collector project.
* **`component.go`**: Contains `ComponentGenerator`. Used to bootstrap a new custom component (e.g., receiver, processor) into the `components/` directory and automatically registers it.
* **`registry.go`**: Manages the component `Registry`. It merges the large embedded `registry.yaml` (upstream OTel components) with custom user registries (`components/registry.yaml`), and handles dynamic version tagging based on OTel versions.
* **`templates.go`**: The rendering engine. Uses `text/template` to load `.go.tmpl` files from embedded filesystems or custom user directories, executing them with a `TemplateContext`.

### 2. Viewing Available Commands (`distrogen --help`)
You can view the available commands by running `distrogen --help`. This is useful for understanding the CLI surface:

```text
Usage: distrogen <command> [flags]
Available Commands:

  component
      --spec string   The distribution specification to use
      --type string   Type of component
      --name string   Name of component

  generate
      --spec string            The distribution specification to use
  -f, --force                  Force generate even if there are no differences detected
      --registry stringArray   Provide additional component registries
      --templates string       Path to custom templates directory
      --compare                Allows you to compare the generated distribution to the existing

  help
    Prints help for all commands

  otel_component_versions
      --otel_version string   The OpenTelemetry version to fetch component versions for

  project
      --spec string         The distribution specification to use
      --tools stringArray   Provide additional tools to install

  query
      --spec string    The distribution specification to use
      --field string   Field to query from the spec

  registry

  update-spec
      --spec string    The distribution specification to use
      --field string   Field to update in the spec. Supports nested structs via `::` (e.g. `foo::bar`).
      --value string   New value for the field
      --stdin          Read JSON value from stdin instead of using --value. Ideal for complex types like arrays/structs.
```

### 3. Testing
Always write unit tests when adding or modifying logic. Testing should occur in the corresponding `_test.go` file (e.g., `distribution_test.go`). Tests should aim to be table-driven and run in parallel where appropriate.
