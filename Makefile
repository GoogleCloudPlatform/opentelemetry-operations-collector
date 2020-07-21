IMAGE_NAME=otelopscol-build
CONTAINER_NAME=otelopscol-build-container

.PHONY: build-image
build-image:
	docker build -t $(IMAGE_NAME) ./.build

# all build commands are run using the docker build image
DOCKER_RUN=docker run -v $(CURDIR):/mnt $(IMAGE_NAME)

.PHONY: package-googet
package-googet:
	$(DOCKER_RUN) /bin/bash -c "cd /mnt; make -f .build/Makefile VERSION=$(VERSION) ARCH=$(ARCH) package-googet"

.PHONY: gcpcol
gcpcol:
	docker run -v $(CURDIR):/mnt -w /mnt --name=$(CONTAINER_NAME) $(IMAGE_NAME) ./tar/generate_tar.sh
	docker rm $(CONTAINER_NAME)
