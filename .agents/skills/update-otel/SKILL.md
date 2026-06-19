---
name: Update OpenTelemetry Version
description: Instructions for updating the OpenTelemetry version for a distribution (like google-built-opentelemetry-collector or otelopscol) in this repository.
---
# Update OpenTelemetry Version

This skill guides you through updating the OpenTelemetry version for a distribution in this repository.

## Prerequisites
Ensure you are in the root of the `opentelemetry-operations-collector` repository.

## Steps

1. **Choose the spec file**:
   - For `google-built-opentelemetry-collector`, use `specs/google-built-opentelemetry-collector.yaml`.
   - For `otelopscol`, use `specs/otelopscol.yaml`.

2. **Update versions in the spec file**:
   - Find the upstream release versions you want to update to (e.g., `v0.154.0` which corresponds to core `v1.60.0`).
   - Update `opentelemetry_version` and `opentelemetry_contrib_version` (usually they match).
   - Update `opentelemetry_stable_version` to the corresponding core version.
   - Update the distribution `version` (usually matches `opentelemetry_contrib_version`).
   - (Optional) Check if there is a newer patch version for the `go_version` and update it if desired.

3. **Update components**:
   - Run `make update-<distribution>-components` where `<distribution>` is:
     - `google-otel` for Google-Built OpenTelemetry Collector.
     - `otelopscol` for OTel Ops Col.
   - Example: `make update-google-otel-components`

4. **Run tests**:
   - Run `make test-all` to run all unit tests and detect breakages.
   - Alternatively, run scoped tests:
     - `make test-google-otel-components`
     - `make test-otelopscol-components`

5. **Generate distribution**:
   - Run `make gen-<distribution>` where `<distribution>` is:
     - `google-built-otel` for Google-Built OpenTelemetry Collector.
     - `otelopscol` for OTel Ops Col.
   - Example: `make gen-google-built-otel`

6. **Build the distribution**:
   - Change directory to the distribution folder (e.g., `google-built-opentelemetry-collector` or `otelopscol`).
   - Run `make build` to ensure it compiles.

7. **Update Kokoro build containers (Googlers Only)**:
   - If Go version was updated, or if you want to use the latest base images:
     - Update `BUILD_CONTAINER` and `CERT_CONTAINER` in the relevant `.gcl` files in `kokoro/config/build/` (e.g., `build_image.gcl`, `image.gcl`, `experiment_aoss_build_image.gcl`).
     - Use `gcloud container images list-tags` to find the correct digests for the desired versions.
