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

# set CONFIG_FILE to be included in the tarball file. Default to the example one
ifeq ($(CONFIG_FILE),)
CONFIG_FILE=config-example.yaml
endif

# set docker Image and Container names
IMAGE_NAME=otelopscol-build

# set collector binary name
OTELCOL_BINARY=google-cloudops-opentelemetry-collector_$(GOOS)_$(GOARCH)$(EXTENSION)

.EXPORT_ALL_VARIABLES:

# --------------------------
#  Build / Package Commands
# --------------------------

.PHONY: build
build:
	go build -o ./bin/$(OTELCOL_BINARY) ./cmd/otelopscol

# googet (Windows)
.PHONY: build-googet
build-googet:
	GOOS=windows
	build package-googet

.PHONY: package-googet
package-googet: SHELL:=/bin/bash
package-googet:
	GOOS=windows
	# goopack doesn't support variable replacement or command line args so just use envsubst
	goopack -output_dir ./dist <(envsubst < ./.build/googet/google-cloudops-opentelemetry-collector.goospec)

# tarball
# Usage: CONFIG_FILE=<custom config file in the config directory> make build-tarball
# CONFIG_FILE is not supplied, default to config-example.yaml
.PHONY: build-tarball
build-tarball:
	make build
	make package-tarball

.PHONY: package-tarball
package-tarball:
	./tar/generate_tar.sh

# --------------------
#  Create build image
# --------------------

.PHONY: docker-build-image
docker-build-image:
	docker build -t $(IMAGE_NAME) ./.build

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
	docker run -e PKG_VERSION -e GOOS -e ARCH -e GOARCH -v $(CURDIR):/mnt -w /mnt $(IMAGE_NAME) /bin/bash -c "make $(TARGET)"
