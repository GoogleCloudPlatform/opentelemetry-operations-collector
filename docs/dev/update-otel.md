# Updating OpenTelemetry Version for a Distribution

Updating the OpenTelemetry version for a distribution requires updating the specification for that distribution and updating the local components that are part of it.

1. Change `opentelemetry_version`, `opentelemetry_contrib_version` (see [note](#updating-contrib-version)), and `opentelemetry_stable_version` (see [note](#updating-stable-version)). 
1. Run `make update-<distribution>-components` to update all components necessary for that distribution.
1. Run `make test-all` to detect breakages in our components, usually arising from `mdatagen` generation changes or simply breaking API changes in core collector libraries.
1. Run `make gen-<distribution>`.
1. Run `make build` in the distribution directory to ensure the build still works after the update.

## Updating Contrib Version

Most of the time, the `opentelemetry_contrib_version` is the same as the `opentelemetry_version`. If it is the same, the `opentelemetry_contrib_version` can be omitted. However, sometimes there are patches just to core or just to contrib, meaning the versions diverge. In this case it is necessary to specify. As of writing, we just include `opentelemetry_contrib_version` anyway.

## Updating Stable Version

Go to [the core repo](https://github.com/open-telemetry/opentelemetry-collector) and look at the GitHub Release entry for the `opentelemetry_version` you are updating to. The `v1.x.x` version will be specified within the same Release entry name, and this is the `stable` version to use. We are working on automation to automatically detect this from the repo directly; this works for component updating but not for distribution generation yet (see #287).

## Adding a new component

If you have added a new component upstream that needs to be added to one of our collectors, you will need to update the `distrogen` embedded registry so you can refer to it in spec files.

Edit [registry.yaml](../../cmd/distrogen/registry.yaml) like so (choose the correct section for your component type, this assumes it's a receiver):
```yaml
receivers:
  newcomponent:
    gomod: github.com/open-telemetry/opentelemetry-collector-contrib/receiver/newcomponentreceiver
    docs_url: https://www.github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/receiver/newcomponentreceiver/README.md
```
This will allow you to refer to it in a spec:
```yaml
components:
  receivers:
    - newcomponent
```
