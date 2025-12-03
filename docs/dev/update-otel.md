# Updating OpenTelemetry Version for a Distribution

Updating the OpenTelemetry version for a distribution requires updating the specification for that distribution and updating the local components that are part of it.

1. Choose the file for your distribution from the `specs` folder.
1. Change `opentelemetry_version`, `opentelemetry_contrib_version` (see [note](#updating-contrib-version)), and `opentelemetry_stable_version` (see [note](#updating-stable-version)). 
1. Change the distribution `version`. This is the actual version for the produced binary. For both distributions in this repository, we simply match `version` with `opentelemetry_contrib_version`.
1. (Optional) Check if there is a newer patch version available for the version of `go` we are on (i.e. a newer `x` for `go1.24.x`).
1. Run `make update-<distribution>-components` to update all components necessary for that distribution. (For example, for `google-built-opentelemetry-collector` the `<distribution>` here would be `google-otel`).
1. Run `make test-all` to detect breakages in our components, usually arising from `mdatagen` generation changes or simply breaking API changes in core collector libraries.
1. Run `make gen-<distribution>`.
1. Change to the distribution directory.
1. Run `make build` in the distribution directory to ensure the build still works after the update.
1. GOOGLERS ONLY: Update the containers used in the Kokoro build config. You will need to search for the most up-to-date bookworm tag for the `CERT_CONTAINER` in Airlock, and for the desired Go version for the `boringcrypto` `BUILD_CONTAINER` by pasting the image URL into your browser (non-Googlers will get an authentication error attempting this).

## Updating Contrib Version

Most of the time, the `opentelemetry_contrib_version` is the same as the `opentelemetry_version`. If it is the same, the `opentelemetry_contrib_version` can be omitted. However, sometimes there are patches just to core or just to contrib, meaning the versions diverge. In this case it is necessary to specify. As of writing, we just include `opentelemetry_contrib_version` in the spec file anyway even when it is the same.

## Updating Stable Version

Go to [the core repo](https://github.com/open-telemetry/opentelemetry-collector) and look at the GitHub Release entry for the `opentelemetry_version` you are updating to. The `v1.x.x` version will be specified within the same Release entry name, and this is the `stable` version to use. We are working on automation to automatically detect this from the repo directly; this works for component updating but not for distribution generation yet (see https://github.com/GoogleCloudPlatform/opentelemetry-operations-collector/issues/287).
