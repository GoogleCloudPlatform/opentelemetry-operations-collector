docker run -d \
    --network otel \
    --name opentelemetry-collector \
    -v /etc/config:/etc/config \
    us-docker.pkg.dev/cloud-ops-agents-artifacts/google-cloud-opentelemetry-collector/otelcol-google:0.133.0 \
    --config=/etc/config/config.yaml