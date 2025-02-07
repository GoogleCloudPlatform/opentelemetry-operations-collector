docker run -d \
    -p 4317:4317 \ # Exposes the OTLP receiver ports.
    -p 4318:4318 \
    us-docker.pkg.dev/cloud-ops-agents-artifacts/google-cloud-opentelemetry-collector/otelcol-google