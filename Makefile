# read PKG_VERSION from VERSION file
include VERSION

# if GOOS is not supplied, set default value based on user's system, will be overridden for OS specific packaging commands
GOOS ?= $(shell go env GOOS)
ifeq ($(GOOS),windows)
EXTENSION := .exe
endif

# if ARCH is not supplied, set default value based on user's system
ARCH ?= $(shell if [ `getconf LONG_BIT` -eq "64" ]; then echo "x86_64"; else echo "x86"; fi)

# set GOARCH based on ARCH
ifeq ($(ARCH),x86_64)
GOARCH := amd64
else ifeq ($(ARCH),x86)
GOARCH := 386
else
$(error "ARCH must be set to one of: x86, x86_64")
endif

ALL_SRC := $(shell find . -name '*.go' -type f | sort)
ALL_DOC := $(shell find . \( -name "*.md" -o -name "*.yaml" \) -type f | sort)
GIT_SHA := $(shell git rev-parse --short HEAD)

BUILD_INFO_IMPORT_PATH := github.com/GoogleCloudPlatform/opentelemetry-operations-collector/internal/version
BUILD_X1 := -X $(BUILD_INFO_IMPORT_PATH).GitHash=$(GIT_SHA)
BUILD_X2 := -X $(BUILD_INFO_IMPORT_PATH).Version=$(PKG_VERSION)
ifdef JMX_HASH
JMX_RECEIVER_IMPORT_PATH := github.com/open-telemetry/opentelemetry-collector-contrib/receiver/jmxreceiver
BUILD_X_JMX := -X $(JMX_RECEIVER_IMPORT_PATH).MetricsGathererHash=$(JMX_HASH)
LD_FLAGS := -ldflags "${BUILD_X1} ${BUILD_X2} ${BUILD_X_JMX}"
else
LD_FLAGS := -ldflags "${BUILD_X1} ${BUILD_X2}"
endif

TOOLS_DIR := internal/tools

.EXPORT_ALL_VARIABLES:

.DEFAULT_GOAL := presubmit

# --------------------------
#  Helper Commands
# --------------------------

.PHONY: update-components
update-components:
	grep -o github.com/open-telemetry/opentelemetry-collector-contrib/[[:lower:]]*/[[:lower:]]* go.mod | xargs -I '{}' go get {}
	go mod tidy
	cd $(TOOLS_DIR) && go get -u github.com/open-telemetry/opentelemetry-collector-contrib/cmd/mdatagen
	cd $(TOOLS_DIR) && go mod tidy

update-opentelemetry:
	$(MAKE) update-components
	$(MAKE) install-tools
	$(MAKE) GO_BUILD_TAGS=gpu generate

# --------------------------
#  Tools
# --------------------------

.PHONY: install-tools
install-tools:
	cd $(TOOLS_DIR) && \
		go install \
			github.com/client9/misspell/cmd/misspell \
			github.com/golangci/golangci-lint/cmd/golangci-lint \
			github.com/google/addlicense \
			github.com/open-telemetry/opentelemetry-collector-contrib/cmd/mdatagen \
			github.com/google/googet/goopack

.PHONY: addlicense
addlicense:
	addlicense -c "Google LLC" -l apache $(ALL_SRC)

.PHONY: checklicense
checklicense:
	@output=`addlicense -check $(ALL_SRC)` && echo checklicense finished successfully || (echo checklicense errors: $$output && exit 1)

.PHONY: lint
lint:
	golangci-lint run --allow-parallel-runners --build-tags=$(GO_BUILD_TAGS) --timeout=20m

.PHONY: misspell
misspell:
	@output=`misspell -error $(ALL_DOC)` && echo misspell finished successfully || (echo misspell errors:\\n$$output && exit 1)

# --------------------------
#  CI
# --------------------------

# Adds license headers to files that are missing it, quiet tests
# so full output is visible at a glance.
.PHONY: precommit
precommit: addlicense lint misspell test

# Checks for the presence of required license headers, runs verbose
# tests for complete information in CI job.
.PHONY: presubmit
presubmit: checklicense lint misspell test_verbose

# --------------------------
#  Build and Test
# --------------------------

GO_BUILD_OUT ?= ./bin/otelopscol
.PHONY: build
build:
	go build -tags=$(GO_BUILD_TAGS) -o $(GO_BUILD_OUT) $(LD_FLAGS) -buildvcs=false ./cmd/otelopscol

OTELCOL_BINARY = google-cloud-metrics-agent_$(GOOS)_$(GOARCH)$(EXTENSION)
.PHONY: build_full_name
build_full_name:
	$(MAKE) GO_BUILD_OUT=./bin/$(OTELCOL_BINARY) build

.PHONY: test
test:
	go test -tags=$(GO_BUILD_TAGS) $(GO_TEST_VERBOSE) -race ./...

.PHONY: test_quiet
test_verbose:
	$(MAKE) GO_TEST_VERBOSE=-v test

.PHONY: generate
generate:
	go generate ./...

# --------------------
#  Docker
# --------------------

# set default docker build image name
BUILD_IMAGE_NAME ?= otelopscol-build
BUILD_IMAGE_REPO ?= gcr.io/stackdriver-test-143416/opentelemetry-operations-collector:test

.PHONY: docker-build-image
docker-build-image:
	docker build -t $(BUILD_IMAGE_NAME) .build

.PHONY: docker-push-image
docker-push-image:
	docker tag $(BUILD_IMAGE_NAME) $(BUILD_IMAGE_REPO)
	docker push $(BUILD_IMAGE_REPO)

.PHONY: docker-build-and-push
docker-build-and-push: docker-build-image docker-push-image

# Usage:   make TARGET=<target> docker-run
# Example: make TARGET=build-goo docker-run
.PHONY: docker-run
docker-run:
ifndef TARGET
	$(error "TARGET is undefined")
endif
	docker run -e PKG_VERSION -e ARCH -v $(CURDIR):/mnt -w /mnt $(BUILD_IMAGE_NAME) /bin/bash -c "make $(TARGET)"

# --------------------
#  (DEPRECATED) Packaging
# --------------------
#
# These targets are kept here due to their use in internal Google build jobs.
# These packaging formats are not maintained and likely contain issues. Do
# not use directly.

# googet (Windows)
.PHONY: build-goo
build-goo:
	make GOOS=windows build_full_name package-goo

.PHONY: package-goo
package-goo: export GOOS=windows
package-goo: SHELL:=/bin/bash
package-goo:
	mkdir -p dist
	# goopack doesn't support variable replacement or command line args so just use envsubst
	goopack -output_dir ./dist <(envsubst < ./.build/googet/google-cloud-metrics-agent.goospec)
	chmod -R a+rwx ./dist/

# tarball
.PHONY: clean-dist
clean-dist:
	rm -rf dist/
