include ./Makefile

AR_AUTH_BIN = $(TOOLS_DIR)/auth

$(AR_AUTH_BIN): $(GO_BIN)
	@{ \
	set -e ;\
	mkdir -p $(TOOLS_DIR) ;\
	echo "Installing AR Auth tools at $(TOOLS_DIR)" ;\
	GOBIN=$(TOOLS_DIR) CGO_ENABLED=0 $(GO_BIN) install -trimpath -ldflags="-s -w" github.com/GoogleCloudPlatform/artifact-registry-go-tools/cmd/auth@v0.4.0 ;\
	} 

.PHONY: aoss-build
aoss-build: $(AR_AUTH_BIN)
	$(AR_AUTH_BIN) add-locations --locations=us && \
	$(AR_AUTH_BIN) refresh && \
	$(MAKE) build
