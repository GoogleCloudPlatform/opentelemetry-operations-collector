# Adding a new component to our Collectors

These instructions are for adding a new component to one of our collector distributions.

## `distrogen` registries

`distrogen` uses a registry yaml file to know how to refer to different components for distribution generation. In this repository there are 3 registry files:

* [The embedded registry][embedded registry], which is embedded in the `distrogen` binary and contains all upstream `opentelemetry-collector` and `opentelemetry-collector-contrib` components
* [`google-built-opentelemetry-collector` components local registry](../../components/google-built-opentelemetry-collector/registry.yaml) which contains components from this repo that are meant for the `google-built-opentelemetry-collector` distribution
* [`otelopscol` components local registry](../../components/otelopscol/registry.yaml) which contains components from this repo that are meant for the `otelopscol` distribution

These files contain components that can be referred to in `distrogen` spec files.

## Adding upstream component

To add a new upstream component to one of our collectors, you can refer to it in that collector's spec file (found in the `specs` folder).

```yaml
components:
  receivers:
  - componentname
```

The component name is the name of the component that is in the [`distrogen` embedded registry][embedded registry]. 

## Newly added upstream component

If you have added a new component upstream that needs to be added to one of our collectors, you will need to ensure it is in the `distrogen` embedded registry so you can refer to it in spec files.

Edit [registry.yaml](../../cmd/distrogen/registry.yaml) like so (choose the correct section for your component type, this assumes it's a receiver):
```yaml
receivers:
  componentname:
    gomod: github.com/open-telemetry/opentelemetry-collector-contrib/receiver/componentnamereceiver
    docs_url: https://www.github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/receiver/componentnamereceiver/README.md
```

## Newly added local component

If you are adding a new local component:

1. Create the component in the correct location in that distribution's `components` subfolder. Your component will go in the component type subfolder i.e. `receiver` or `exporter` (create it if it does not exist) and will be its own proper Go module.
2. Add it to the local registry for that subfolder. You will ensure the `gomod` entry matches what is in the `go.mod` file for the component, and that the `path` entry is to the right folder. (NOTE: the `path` starts with `..` because the assumption is that it is being referred to from a generated distribution working directory).

After this, you will be able to refer to the component in the respective spec file.

[embedded registry](../../cmd/distrogen/registry.yaml)]