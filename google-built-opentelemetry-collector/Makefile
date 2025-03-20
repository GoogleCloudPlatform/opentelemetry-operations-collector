# Variables filled in from distro specification
VERSION ?= 0.121.0
OTEL_VERSION ?= 0.121.0
OTEL_VERSION_TAG = v$(OTEL_VERSION)
COLLECTOR_BINARY_NAME = otelcol-google
COLLECTOR_BUILD_TEMP_DIR = $(PWD)/_build
COLLECTOR_CGO = 0
COLLECTOR_BUILDVCS ?= false
COLLECTOR_BUILD_TAGS ?= 

DOCKER_LOCAL_IMAGE_TAG ?= $(COLLECTOR_BINARY_NAME)
DOCKER_IMAGE_TAG = $(DOCKER_REPO)/$(COLLECTOR_BINARY_NAME):$(VERSION)

# Tools Variables

TOOLS_DIR = $(PWD)/.tools

OCB_BIN ?= $(TOOLS_DIR)/ocb

GO_VERSION = 1.24.0
GO_BIN_DIR ?= $(TOOLS_DIR)/go/bin
GO_BIN ?= $(GO_BIN_DIR)/go
SET_GO_BIN_PATH ?= PATH="$(GO_BIN_DIR):$$PATH"

.PHONY: build
build: $(COLLECTOR_BINARY_NAME)

.PHONY: image
image: image-build image-push

.PHONY: clean
clean: clean-tools clean-collector

.PHONY: install-tools
install-tools: go ocb

###################
# Image Build
###################
# These targets are for building a collector image
# and pushing it to a Docker repo.

# Using a local collector binary, build an image
# that uses this collector.
.PHONY: image-build
image-build: $(COLLECTOR_BINARY_NAME)
	docker buildx build -t $(DOCKER_LOCAL_IMAGE_TAG) .

# Build an image by leveraging Dockerfile.build, which will
# build the collector binary within Docker as well.
.PHONY: image-build-full
image-build-full:
	docker buildx build -f Dockerfile.build -t $(DOCKER_LOCAL_IMAGE_TAG) ..

# After an image is built, pushes it to the specified docker repository.
.PHONY: image-push
image-push:
ifndef DOCKER_REPO
	$(error "Must set DOCKER_REPO to push")
endif
	docker tag $(DOCKER_LOCAL_IMAGE_TAG) $(DOCKER_IMAGE_TAG)
	docker push $(DOCKER_IMAGE_TAG)

###################
# Collector Build
###################
# These targets are for building a collector binary.

# Unconditionally build a new collector.
.PHONY: collector-build
collector-build: clean-collector $(COLLECTOR_BINARY_NAME)

# Using OCB and the local manifest file, build a collector.
$(COLLECTOR_BINARY_NAME): $(OCB_BIN)
	$(SET_GO_BIN_PATH) $(OCB_BIN) --skip-compilation --verbose --config manifest.yaml
	cd $(COLLECTOR_BUILD_TEMP_DIR) && \
		GOWORK=off \
		CGO_ENABLED=$(COLLECTOR_CGO) \
		$(GO_BIN) build \
			-buildvcs=$(COLLECTOR_BUILDVCS) \
			-tags=$(COLLECTOR_BUILD_TAGS) \
			--ldflags="-s -w $(COLLECTOR_LD_FLAGS)" \
			-o ../otelcol-google \
			.
	$(MAKE) clean-build-dir

.PHONY: collector-package-list
collector-package-list: $(OCB_BIN)
	PATH="$(GO_BIN_DIR):${PATH}" GOWORK=off CGO_ENABLED=$(COLLECTOR_CGO) $(OCB_BIN) --ldflags="$(COLLECTOR_LD_FLAGS)" --verbose --skip-compilation --config manifest.yaml
	cd $(COLLECTOR_BUILD_TEMP_DIR) && \
		GOWORK=off go list -deps -f '{{ .ImportPath }} {{ .Module }}' ./... > ../packages.txt
	grep -v '<nil>' packages.txt | sort -u > packages.txt
	rm -rf $(COLLECTOR_BUILD_TEMP_DIR)

# Delete any existing built collector binary.
.PHONY: clean-collector
clean-collector: clean-build-dir
	rm -f $(COLLECTOR_BINARY_NAME)

.PHONY: clean-build-dir
clean-build-dir:
	rm -rf $(COLLECTOR_BUILD_TEMP_DIR)

###################
# Tools
###################
# These targets are for installing separate tools
# required for the build process.

.PHONY: ocb
ocb: $(OCB_BIN)

.PHONY: go
go: $(GO_BIN)

# Install the required version of Go.
$(GO_BIN): GO_ARCHIVE = go$(GO_VERSION).linux-amd64.tar.gz
$(GO_BIN):
	@{ \
	set -e ;\
	mkdir -p $(TOOLS_DIR) ;\
	echo "Installing go$(GO_VERSION) at $(TOOLS_DIR)" ;\
	curl -LO https://go.dev/dl/$(GO_ARCHIVE) ;\
	tar -C $(TOOLS_DIR) -xzf $(GO_ARCHIVE) ;\
	rm $(GO_ARCHIVE) ;\
	}

$(OCB_BIN): $(GO_BIN)
	@{ \
	set -e ;\
	mkdir -p $(TOOLS_DIR) ;\
	echo "Installing ocb at $(TOOLS_DIR)" ;\
	GOBIN=$(TOOLS_DIR) CGO_ENABLED=0 $(GO_BIN) install -trimpath -ldflags="-s -w" go.opentelemetry.io/collector/cmd/builder@$(OTEL_VERSION_TAG) ;\
	mv $(TOOLS_DIR)/builder $(OCB_BIN) ;\
	}

.PHONY: clean-tools
clean-tools:
	rm -rf $(TOOLS_DIR)
