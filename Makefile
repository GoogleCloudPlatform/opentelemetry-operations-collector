# read PKG_VERSION from VERSION file
include VERSION

# if GOOS is not supplied, set default value based on user's system, will be overridden for OS specific packaging commands
ifeq ($(GOOS),)
GOOS=$(shell go env GOOS)
endif
ifeq ($(GOOS),windows)
EXTENSION = .exe
endif

# if ARCH is not supplied, set default value based on user's system
ifeq ($(ARCH),)
ARCH = $(shell if [ `getconf LONG_BIT` == "64" ]; then echo "x86_64"; else echo "x86"; fi)
endif

# set GOARCH based on ARCH
ifeq ($(ARCH),x86_64)
GOARCH=amd64
else ifeq ($(ARCH),x86)
GOARCH=386
else
$(error "ARCH must be set to one of: x86, x86_64")
endif

# set docker build image name
ifeq ($(BUILD_IMAGE_NAME),)
BUILD_IMAGE_NAME=otelopscol-build
endif

OTELCOL_BINARY=google-cloudops-opentelemetry-collector_$(GOOS)_$(GOARCH)$(EXTENSION)
.EXPORT_ALL_VARIABLES:

# --------------------------
#  Build / Package Commands
# --------------------------

.PHONY: build
build:
	go build -o ./bin/$(OTELCOL_BINARY) ./cmd/otelopscol

# googet (Windows)

.PHONY: build-goo
build-goo: export GOOS=windows
build-goo: export EXTENSION=.exe
build-goo: build package-goo

.PHONY: package-goo
package-goo: export GOOS=windows
package-goo: export EXTENSION=.exe
package-goo: SHELL:=/bin/bash
package-goo:
	mkdir -p dist
	# goopack doesn't support variable replacement or command line args so just use envsubst
	goopack -output_dir ./dist <(envsubst < ./.build/googet/google-cloudops-opentelemetry-collector.goospec)
	chmod -R 777 ./dist/

# exporters
.PHONY: build-exporters
build-exporters:
	bash ./.build/tar/build_exporters.sh

# tarball
.PHONY: clean-dist
clean-dist:
	rm -rf dist/

.PHONY: package-tarball
package-tarball:
	bash ./.build/tar/generate_tar.sh
	chmod -R 777 ./dist/

.PHONY: build-tarball
build-tarball: clean-dist build package-tarball

.PHONY: build-tarball-exporters
build-tarball-exporters: clean-dist build build-exporters package-tarball

# --------------------
#  Create build image
# --------------------

.PHONY: docker-build-image
docker-build-image:
	docker build -t $(BUILD_IMAGE_NAME) ./.build

# -------------------------------------------
#  Run targets inside the docker build image
# -------------------------------------------

# Usage:   make TARGET=<target> docker-run
# Example: make TARGET=package-googet docker-run
.PHONY: docker-run
docker-run:
ifndef TARGET
	$(error "TARGET is undefined")
endif
	docker run -e PKG_VERSION -e GOOS -e ARCH -e GOARCH -v $(CURDIR):/mnt -w /mnt $(BUILD_IMAGE_NAME) /bin/bash -c "make $(TARGET)"
