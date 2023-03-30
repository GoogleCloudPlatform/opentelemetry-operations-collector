# Upgrade OpenTelemetry

When the [opentelemetry-collector](https://github.com/open-telemetry/opentelemetry-collector) and [opentelemetry-collector-contrib](https://github.com/open-telemetry/opentelemetry-collector-contrib) repos do a release, we need to update our dependencies to pick up the new changes.

## Update All Dependencies

There is a `make` target to update all OpenTelemetry dependencies and regenerate necessary metadata. To run the update:
```
make update-opentelemetry
```

<details>
    <summary><code>update-opentelemetry</code> target details</summary>

First, update all `opentelemetry` dependencies to the newest possible version.
```
make update-components
```
These dependencies include the `mdatagen` tool, which is in a separate place from libraries (read more in [tools.md](./tools.md)). Since the `mdatagen` version has been updated in the tools `go.mod`, re-install tools to actually install the new version:
```
make install-tools
```
With the new version of `mdatagen` installed, regenerate the `metadata` packages for tests. This will bring the test packages in line with anything that's changed in the `opentelemetry-collector` base libraries.
```
GO_BUILD_TAGS=gpu make generate
```
</details>

## Test

Updating OpenTelemetry dependencies and regenerating metadata frequently includes breaking changes in the foundational libraries like `pdata` that require changes to our receivers and tests. Start by ensuring it is possible to build:
```
GO_BUILD_TAGS=gpu make build
```
Fix any build errors that may come with the dependency upgrade, then ensure tests also pass:
```
GO_BUILD_TAGS=gpu make test
```

Once the upgrade is complete and build and test are healthy, submit a PR with your changes. Ensure the new OpenTelemetry Collector version is within the PR title, so we can keep track of when we made each upgrade.
