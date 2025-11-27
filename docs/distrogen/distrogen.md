#  Distrogen

Distrogen is a tool for creating and managing an [OpenTelemetry Collector](https://opentelemetry.io/docs/collector/) "[distribution](https://github.com/open-telemetry/opentelemetry-collector/tree/main/docs#opentelemetry-collector-distribution)". 

## Distributions

The tool can be used  for generating a distribution from a YAML specification.

* Features a full set of robust templates that most distributions will use
* Features a registry for all `opentelemetry-collector` and `opentelemetry-collector-contrib` components
* Allows you to provide your own templates and registry to build custom collectors with local or external components that work for your use case

Read more in the [Distribution docs](./distribution.md).

## Registries

A registry is a mapping of components to component details. It allows you to easily reference components within your distribution spec.

Read more in the [Registry docs](./registry.md).

## Projects

The tool can also be used to generate an entire project around your distribution. This is for scenarios where you want to write your own local components to go into your distribution.

* Provides in-depth Makefiles for project management tasks
* Creates a logical project structure for the primary distribution management tasks
    - Developing and maintaining a custom component, including keeping that component up to date with Collector libraries and keeping a local registry
    - Providing custom templates for your distribution
    - Distribution generation

Read more in the [Project docs](./project.md).

## Command Line

Distrogen is a command line tool. For usage, see the [full command reference](./command.md).

