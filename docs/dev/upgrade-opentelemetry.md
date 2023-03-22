# Upgrade OpenTelemetry

When the [opentelemetry-collector](https://github.com/open-telemetry/opentelemetry-collector) and [opentelemetry-collector-contrib](https://github.com/open-telemetry/opentelemetry-collector-contrib) repos do a release, we need to update our dependencies to pick up the new changes.

## Update All Dependencies

There is a `make` target that will update all `opentelemetry-collector-contrib` dependencies, which will include all indirect `opentelemetry-collector` dependencies:
```
make update-components
```
And then re-install tools to install the new version of the `mdatagen` tool:
```
make install-tools
```

## Regenerate Metadata Code

With the new version of `mdatagen` installed, next we'll need to re-generate the `metadata` packages for tests. This will bring the test packages in line with anything that's changed in the `opentelemetry-collector` base libraries.
```
GO_BUILD_TAGS=gpu make generate
```

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
