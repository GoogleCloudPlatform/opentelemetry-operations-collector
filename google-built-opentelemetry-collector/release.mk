.PHONY: local-container-goreleaser
local-container-goreleaser:
	docker buildx build \
		--progress=plain \
		-t otelcol-google-build \
		-f Dockerfile.goreleaser_releaser \
		..
	CONTAINER_ID=$$(docker create otelcol-google-build) && \
		docker cp $$CONTAINER_ID:/google-built-opentelemetry-collector/dist . &&\
		docker rm --force $$CONTAINER_ID