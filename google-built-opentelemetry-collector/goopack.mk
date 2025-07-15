include ./Makefile

GOOPACK_BIN ?= $(TOOLS_DIR)/goopack
GOOPACK_ARCH ?= x86_64
GOOPACK_GOARCH ?= amd64
GOOPACK_DEST ?= googet

COLLECTOR_WINDOWS ?= dist/otelcol-google-windows_windows_amd64_v1/otelcol-google.exe

.PHONY: goo-package
goo-package: $(GOOPACK_BIN) $(COLLECTOR_WINDOWS)
	mkdir -p $(GOOPACK_DEST) && \
		$(GOOPACK_BIN) -output_dir $(GOOPACK_DEST) \
			-var:PKG_VERSION=0.130.0 \
			-var:ARCH=$(GOOPACK_ARCH) \
			-var:GOOS=windows \
			-var:GOARCH=$(GOOPACK_GOARCH) \
			goo/otelcol.goospec

$(COLLECTOR_WINDOWS):
	$(MAKE) goreleaser-release

.PHONY: goopack
goopack: $(GOOPACK_BIN)

$(GOOPACK_BIN): $(GO_BIN)
	@{ \
	set -e ;\
	mkdir -p $(TOOLS_DIR) ;\
	echo "Installing goopack at $(TOOLS_DIR)" ;\
	GOBIN=$(TOOLS_DIR) CGO_ENABLED=0 $(GO_BIN) install -trimpath -ldflags="-s -w" github.com/google/googet/v2/goopack@latest ;\
	}
