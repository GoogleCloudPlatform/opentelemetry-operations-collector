# These targets are for convenience when working with other Dockerfiles in this folder
# using a local docker setup.

DOCKER_LOCAL_IMAGE_TAG ?= otelcol-google

.PHONY: local-container-build
local-container-build:
	docker buildx build \
		-f Dockerfile.build \
		-t $(DOCKER_LOCAL_IMAGE_TAG) \
		..