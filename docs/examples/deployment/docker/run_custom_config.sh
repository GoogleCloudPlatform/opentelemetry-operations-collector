docker run -d \
    -p 4317:4317 \ # Exposes the OTLP receiver ports.
    -p 4318:4318 \
    -v $CONFIG_DIR:/etc/config \
    us-docker.pkg.dev/cloud-ops-agents-artifacts/google-cloud-opentelemetry-collector/otelcol-google \
    --config=/etc/otelcol-google/config.yaml \ # OPTIONAL: Remove if you do not want to run the default pipelines
    --config=/etc/config/$CONFIG_FILE