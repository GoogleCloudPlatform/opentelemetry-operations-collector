# Verification Summary Attestation (VSA)

This directory contains Verification Summary Attestations (VSAs) for released Google Built OpenTelemetry Collector (GBOC) container images.

Starting from Google Built OpenTelemetry Collector release `0.134.0`, VSAs are generated for each released container image and are organized in subdirectories corresponding to the collector's release version. The VSA filename follows the pattern: `<container_image_digest>.intoto.jsonl`.

## Finding the VSA for an Image

To find the VSA for a specific container image, you first need the image's digest (the SHA256 hash).

**Example:**

Given the GBOC container image:
`us-docker.pkg.dev/cloud-ops-agents-artifacts/google-cloud-opentelemetry-collector/otelcol-google:0.134.0`

1.  **Pull the image:**
    ```bash
    docker pull us-docker.pkg.dev/cloud-ops-agents-artifacts/google-cloud-opentelemetry-collector/otelcol-google:0.134.0
    ```

2.  **Inspect to get the digest of the image:**
    ```bash
    IMAGE_NAME="us-docker.pkg.dev/cloud-ops-agents-artifacts/google-cloud-opentelemetry-collector/otelcol-google:0.134.0"
    DIGEST=$(docker inspect --format='{{index .RepoDigests 0}}' "${IMAGE_NAME}" | cut -d'@' -f2)
    echo "${DIGEST}"
    ```

    Output:
    ```
    sha256:934bda3bc2d79810c622d2deee6990b9f4bb8e3e40e6f41fd074110e4c17135f
    ```

3.  **Locate the VSA file:**

    The VSA for this image is located at:
    `google-built-opentelemetry-collector/VSA/0.134.0/sha256:934bda3bc2d79810c622d2deee6990b9f4bb8e3e40e6f41fd074110e4c17135f.intoto.jsonl`

## Related Links

*   [Google-Built Open Telemetry Collector Documentation](https://cloud.google.com/stackdriver/docs/instrumentation/google-built-otel)
*   [What is a VSA?](https://slsa.dev/spec/v1.1/verification_summary)