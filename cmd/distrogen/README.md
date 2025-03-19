# distrogen

This tool generates OpenTelemetry Collector distributions.

* Generates distributions from a simple yaml spec
* Features a full set of robust templates that most distributions will use
* Features a registry for all `opentelemetry-collector` and `opentelemetry-collector-contrib` components
* Allows you to provide your own templates and registry to build custom collectors that work for your use case

## Usage

Given a spec file such as:
```yaml
name: basic-distro
display_name: Basic OTel
version: 0.121.0
description: "A basic distribution of the OpenTelemetry Collector"
blurb: "A basic collector distro"
opentelemetry_version: 0.121.0
opentelemetry_stable_version: 1.27.0
binary_name: otelcol-basic
collector_cgo: false
go_version: 1.24.0

docker_repo: us-docker.pkg.dev/pretend-repo/otelcol-basic

components:
  receivers:
    - otlp
  processors:
    - batch
    - memorylimiter
  exporters:
    - otlp
  extensions:
    - pprof
  connectors:
    - forward
  providers:
    - yaml
```
Run `distrogen` with your spec:
```
distrogen -spec spec.yaml
```
It will generate a `basic-distro` directory. In that directory you can run `make build` to build a binary, or `make image-build` to build a binary as well as the resulting Docker container.