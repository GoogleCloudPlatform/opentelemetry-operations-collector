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

To reference a component that isn't in this registry, you will need to provide a registry in the [`generate` command flags](./command.md#generate). Additional registries are merged into the embedded registry in the order they're specified in CLI flags. In the merge process, if a new registry has an entry with a name that already exists in the current registry merge, **it is overwritten by the newer entry**.  
This enables use of a forked a component that is in the embedded registry by specifying an entry with the same name in your custom registry (though giving the forked component a unique name in the custom registry is also an option, and recommended if you need to be able to refer to the forked version as well). 