include ./Makefile

GOOPACK_BIN ?= $(TOOLS_DIR)/goopack
GOOPACK_ARCH ?= x86_64
GOOPACK_GOARCH ?= amd64
GOOPACK_DEST ?= googet
COLON := :

.PHONY: goo-package
goo-package: $(GOOPACK_BIN)
	mkdir -p $(GOOPACK_DEST) && \
    $(GOOPACK_BIN) -output_dir $(GOOPACK_DEST) -var$(COLON)PKG_VERSION=0.121.0 -var$(COLON)ARCH=$(GOOPACK_ARCH) -var$(COLON)GOOS=windows -var$(COLON)GOARCH=$(GOOPACK_GOARCH) goo/install.goospec

.PHONY: goopack
goopack: $(GOOPACK_BIN)

$(GOOPACK_BIN): $(GO_BIN)
	@{ \
	set -e ;\
	mkdir -p $(TOOLS_DIR) ;\
	echo "Installing goopack at $(TOOLS_DIR)" ;\
	GOBIN=$(TOOLS_DIR) CGO_ENABLED=0 $(GO_BIN) install -trimpath -ldflags="-s -w" github.com/google/googet/v2/goopack@latest ;\
	}