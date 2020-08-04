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

.DEFAULT_GOAL := all

# --------------------------
#  Build / Package Commands
# --------------------------

ALL_SRC := $(shell find . -name '*.go' -type f | sort)

.PHONY: installtools
installtools:
	go install github.com/google/addlicense

.PHONY: all
all: checklicense test build

.PHONY: checklicense
checklicense:
	@ADDLICENSEOUT=`addlicense -check $(ALL_SRC) 2>&1`; \
		if [ "$$ADDLICENSEOUT" ]; then \
			echo "addlicense FAILED => add License errors:"; \
			echo "$$ADDLICENSEOUT"; \
			exit 1; \
		else \
			echo "Check License finished successfully"; \
		fi

.PHONY: build
build:
	@echo "Building collector binary..."
	go build -o ./bin/$(OTELCOL_BINARY) ./cmd/otelopscol

.PHONY: test
test:
	go test ./...

# googet (Windows)

.PHONY: build-goo
build-goo: export GOOS=windows
build-goo: export EXTENSION=.exe
build-goo: test build package-goo

.PHONY: package-goo
package-goo: export GOOS=windows
package-goo: export EXTENSION=.exe
package-goo: SHELL:=/bin/bash
package-goo:
	mkdir -p dist
	# goopack doesn't support variable replacement or command line args so just use envsubst
	goopack -output_dir ./dist <(envsubst < ./.build/googet/google-cloudops-opentelemetry-collector.goospec)
	chmod -R 777 ./dist/

# tarball

.PHONY: build-tarball
build-tarball: test build package-tarball

.PHONY: package-tarball
package-tarball:
	@echo "Packaging tarball..."
	bash ./.build/tar/generate_tar.sh
	chmod -R 777 ./dist/

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
	@echo "Running $(TARGET) in docker container $(BUILD_IMAGE_NAME)"
	docker run -e PKG_VERSION -e GOOS -e ARCH -e GOARCH -v $(CURDIR):/mnt -w /mnt $(BUILD_IMAGE_NAME) /bin/bash -c "make $(TARGET)"
