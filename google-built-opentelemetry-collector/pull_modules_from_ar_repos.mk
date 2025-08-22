include ./Makefile

AR_AUTH_BIN = $(TOOLS_DIR)/auth
USE_GO_PROXY ?= https://us-go.pkg.dev/access-aoss/assuredoss-go

$(AR_AUTH_BIN): $(GO_BIN)
	@{ \
	set -e ;\
	mkdir -p $(TOOLS_DIR) ;\
	echo "Installing AR Auth tools at $(TOOLS_DIR)" ;\
	GOBIN=$(TOOLS_DIR) GOPROXY=https://us-go.pkg.dev/artifact-foundry-prod/golang-3p-trusted CGO_ENABLED=0 $(GO_BIN) install -trimpath -ldflags="-s -w" github.com/GoogleCloudPlatform/artifact-registry-go-tools/cmd/auth@v0.4.0 ;\
	}

.PHONY: aoss-build
aoss-build: $(AR_AUTH_BIN)
	$(AR_AUTH_BIN) add-locations --locations=us && \
	$(AR_AUTH_BIN) refresh && \
	$(MAKE) USE_GO_PROXY=https://us-go.pkg.dev/access-aoss/assuredoss-go build
