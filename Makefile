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

.EXPORT_ALL_VARIABLES:

# --------------------------
#  Build / Package Commands
# --------------------------

.PHONY: build
build:
	go build -o ./bin/google-cloudops-opentelemetry-collector_$(GOOS)_$(GOARCH)$(EXTENSION) ./cmd/otelopscol

# googet (Windows)

.PHONY: package-googet
package-googet:
	GOOS=windows
	build pack-googet

.PHONY: pack-googet
pack-googet: SHELL:=/bin/bash
pack-googet:
	GOOS=windows
	# goopack doesn't support variable replacement or command line args so just use envsubst
	goopack -output_dir ./dist <(envsubst < ./.build/googet/google-cloudops-opentelemetry-collector.goospec)

# tarball (Linux)

# TODO

# --------------------
#  Create build image
# --------------------

.PHONY: docker-build-image
build-image:
	docker build -t otelopscol-build ./.build

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
	docker run -e PKG_VERSION -e GOOS -e ARCH -e GOARCH -v $(CURDIR):/mnt otelopscol-build /bin/bash -c "cd /mnt; make $(TARGET)"
