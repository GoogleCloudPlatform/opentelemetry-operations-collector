.PHONY: build-image
build-image:
	docker build -t otelopscol-build ./.build

# all build commands are run using the docker build image
DOCKER_RUN=docker run -v $(CURDIR):/mnt otelopscol-build

.PHONY: package-googet
package-googet:
	$(DOCKER_RUN) /bin/bash -c "cd /mnt; make -f .build/Makefile VERSION=$(VERSION) ARCH=$(ARCH) package-googet"

# gcp collector related variables
IMAGE_NAME=gcpcol_image
CONTAINER_NAME=gcpcol_container

.PHONY: gcpcol
gcpcol:
	docker build -t $(IMAGE_NAME) ./linux
	docker run -i --name=$(CONTAINER_NAME) $(IMAGE_NAME)
	docker cp $(CONTAINER_NAME):/go/gcp-otel.tar.gz .
	docker rmi -f $(IMAGE_NAME)
	docker rm $(CONTAINER_NAME)
