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

# set default docker build image name
BUILD_IMAGE_NAME ?= otelopscol-build

OTELCOL_BINARY = google-cloud-metrics-agent_$(GOOS)_$(GOARCH)$(EXTENSION)

ALL_SRC := $(shell find . -name '*.go' -type f | sort)
ALL_DOC := $(shell find . \( -name "*.md" -o -name "*.yaml" \) -type f | sort)
GIT_SHA := $(shell git rev-parse --short HEAD)

BUILD_INFO_IMPORT_PATH := github.com/GoogleCloudPlatform/opentelemetry-operations-collector/internal/version
BUILD_X1 := -X $(BUILD_INFO_IMPORT_PATH).GitHash=$(GIT_SHA)
BUILD_X2 := -X $(BUILD_INFO_IMPORT_PATH).Version=$(PKG_VERSION)
BUILD_JMX := -X github.com/open-telemetry/opentelemetry-collector-contrib/receiver/jmxreceiver.MetricsGathererHash=$(JMX_JAR_SHA)
ifdef JMX_JAR_SHA
LD_FLAGS := -ldflags "${BUILD_X1} ${BUILD_X2} ${BUILD_JMX}"
else
LD_FLAGS := -ldflags "${BUILD_X1} ${BUILD_X2}"
endif

TOOLS_DIR := internal/tools

.EXPORT_ALL_VARIABLES:

.DEFAULT_GOAL := presubmit

# --------------------------
#  Tools
# --------------------------

.PHONY: install-tools
install-tools:
	cd $(TOOLS_DIR) && go install github.com/client9/misspell/cmd/misspell
	cd $(TOOLS_DIR) && go install github.com/golangci/golangci-lint/cmd/golangci-lint
	cd $(TOOLS_DIR) && go install github.com/google/addlicense
	cd $(TOOLS_DIR) && go install github.com/google/googet/goopack
	cd $(TOOLS_DIR) && go install github.com/pavius/impi/cmd/impi
	cd $(TOOLS_DIR) && go install github.com/open-telemetry/opentelemetry-collector-contrib/cmd/mdatagen@v0.67.0

# --------------------------
#  Helper Commands
# --------------------------

.PHONY: update-components
update-components:
	grep -o github.com/open-telemetry/opentelemetry-collector-contrib/[[:lower:]]*/[[:lower:]]* go.mod | xargs -I '{}' go get {}
	go mod tidy

# --------------------------
#  Build / Package Commands
# --------------------------

# lint / build / test

.PHONY: presubmit_tool_checks
presubmit_tool_checks: checklicense impi lint misspell

.PHONY: presubmit
presubmit: presubmit_tool_checks test

.PHONY: presubmit_gpu_support
presubmit_gpu_support: presubmit_tool_checks test_gpu_support

.PHONY: checklicense
checklicense:
	@output=`addlicense -check $(ALL_SRC)` && echo checklicense finished successfully || (echo checklicense errors: $$output && exit 1)

.PHONY: impi
impi:
	@output=`impi --local github.com/GoogleCloudPlatform/opentelemetry-operations-collector --scheme stdThirdPartyLocal ./...` && echo impi finished successfully || (echo impi errors:\\n$$output && exit 1)

.PHONY: lint
lint:
	golangci-lint run --allow-parallel-runners --timeout=20m

.PHONY: misspell
misspell:
	@output=`misspell -error $(ALL_DOC)` && echo misspell finished successfully || (echo misspell errors:\\n$$output && exit 1)

# --------------------------
#  Helper Commands
# --------------------------

.PHONY: update-components
update-components:
	grep -o github.com/open-telemetry/opentelemetry-collector-contrib/[[:lower:]]*/[[:lower:]]* go.mod | xargs -I '{}' go get {}
	go mod tidy

# --------------------------
#  Build and Test 
# --------------------------

.PHONY: build
build:
	go build -tags=$(GO_TAGS) -o ./bin/otelopscol $(LD_FLAGS) ./cmd/otelopscol

.PHONY: build_full_name
build_full_name:
	go build -tags=$(GO_TAGS) -o ./bin/$(OTELCOL_BINARY) $(LD_FLAGS) ./cmd/otelopscol

.PHONY: test
test:
	go test -tags=$(GO_TAGS) -v -race ./...

.PHONY: test_quiet
test_quiet:
	go test -tags=$(GO_TAGS) -race ./...

.PHONY: generate
generate:
	go generate ./...
