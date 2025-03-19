.PHONY: local-container-goreleaser
local-container-goreleaser:
	docker buildx build \
		--progress=plain \
		-t otelopscol-build \
		-f Dockerfile.goreleaser_releaser \
		..
	CONTAINER_ID=$$(docker create otelopscol-build) && \
		docker cp $$CONTAINER_ID:/otelopscol/dist . &&\
		docker rm --force $$CONTAINER_ID
