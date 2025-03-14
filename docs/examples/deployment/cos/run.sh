docker run -d \
    --network otel \
    --name opentelemetry-collector \
    -v /etc/config:/etc/config \
    us-docker.pkg.dev/cloud-ops-agents-artifacts/google-cloud-opentelemetry-collector/otelcol-google \
    --config=/etc/config/config.yaml