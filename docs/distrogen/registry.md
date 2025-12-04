# Registry

A registry is a set of components that can be used in a [Distribution Spec](./distribution.md#components). It is a map of logical component names to the information needed to include that component in a distribution.

The structure is roughly:

```yaml
<component type>:
  <component name>:
    gomod: <go module url>
    docs_url: <url to component documentation>
    path: <path to component in project if it's a local component>
```

In the `distrogen` binary, [a registry with all core and contrib components is embedded](../../cmd/distrogen/registry.yaml). It is always on, meaning that components in it can always be referenced.

To reference a component that isn't in this registry, you will need to provide the registry in the [`generate` command flags](./command.md#generate).
If you want to use a fork of a component that is in the embedded registry, you can either give it
a unique name in your registry, or you can give it the same name as the one in the embedded registry; the provided registries are all merged with the embedded registry,
meaning that the component you provided in your additional registry overwrites the embedded one.