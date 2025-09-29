docker build google-built-opentelemetry-collector \
--file google-built-opentelemetry-collector/Dockerfile.image_with_gcloud.build \
--platform linux/amd64,linux/arm64 \
--build-arg PROJECT_ROOT='git/otelcol-google' \
--build-arg BUILD_CONTAINER='us-docker.pkg.dev/google.com/api-project-999119582588/go-boringcrypto-internal/golang@sha256:5e292c54d2d37534a367761cbc0b69b81d717c730f824e0f7abdcd54133e43f1' \
--build-arg CERT_CONTAINER='us-docker.pkg.dev/artifact-foundry-prod/docker-3p-trusted/golang@sha256:6e867e7a9b18808f61e7f1e8815535199f526bb227be340be6547f239a94228b' \
--output=type=docker,dest=$KOKORO_ARTIFACTS_DIR/container.tar .
