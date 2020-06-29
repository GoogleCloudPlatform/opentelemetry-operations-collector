.PHONY: build-image
build-image:
	docker build -t otelopscol-build ./.build

# all build commands are run using the docker build image
DOCKER_RUN=docker run -v $(CURDIR):/mnt otelopscol-build

.PHONY: package-googet
package-googet:
	$(DOCKER_RUN) /bin/bash -c "cd /mnt; make -f .build/Makefile VERSION=$(VERSION) ARCH=$(ARCH) package-googet"
