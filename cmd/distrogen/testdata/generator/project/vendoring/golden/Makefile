include ./make/common.mk
include ./make/maintenance.mk

MAKEFLAGS += --no-print-directory

SPEC_FILE = spec.yaml

#######
# Tools
#######

TOOLS_DIR ?= $(PWD)/.tools
DISTROGEN_BIN ?= $(TOOLS_DIR)/distrogen
DISTROGEN_QUERY = $(if $(wildcard $(DISTROGEN_BIN)),$(DISTROGEN_BIN),distrogen) query --spec $(SPEC_FILE) --field
DISTROGEN_VERSION ?= $(shell $(DISTROGEN_QUERY) distrogen_version)
MDATAGEN_BIN ?= $(TOOLS_DIR)/mdatagen
DISTRO_DIR = $(PWD)/

$(DISTROGEN_BIN): $(SPEC_FILE)
	$(MAKE) tools-dir
	@CURRENT_VERSION=$$(go version -m $(DISTROGEN_BIN) 2>/dev/null | awk '$$1 == "mod" {print $$3}'); \
	if [ "$$CURRENT_VERSION" != "$(DISTROGEN_VERSION)" ]; then \
		echo "Updating distrogen to $(DISTROGEN_VERSION)..."; \
		GOBIN=$(TOOLS_DIR) go install github.com/GoogleCloudPlatform/opentelemetry-operations-collector/cmd/distrogen@$(DISTROGEN_VERSION); \
	fi

$(MDATAGEN_BIN):
	$(MAKE) tools-dir
	GOBIN=$(TOOLS_DIR) bash ./scripts/download_mdatagen.sh $${OTEL_VERSION}

# This is a PHONY target cause if you make it as a normal recipe
# it gets very confused because the creation date of the .tools
# directory is newer than the tools inside it.
.PHONY: tools-dir
tools-dir:
	@mkdir -p $(TOOLS_DIR)

.PHONY: distrogen
distrogen: $(DISTROGEN_BIN)

##########################
# Updating OTel Components
##########################

COMPONENT_DIR = components


.PHONY: update-otel-components
update-otel-components: $(DISTROGEN_BIN) $(MDATAGEN_BIN) workspace-update
	export OTEL_VERSION = v$(shell $(DISTROGEN_QUERY) opentelemetry_version) && \
	export OTEL_CONTRIB_VERSION = v$(shell $(DISTROGEN_QUERY) opentelemetry_contrib_version) && \
	export PATH="$(TOOLS_DIR):${PATH}" && \
	cd components && \
	$(MAKE) update-components
	$(MAKE) gen

.PHONY: test-otel-components
test-otel-components: go.work
	cd $(COMPONENT_DIR) && $(MAKE) test-components

.PHONY: tidy-otel-components
tidy-otel-components: go.work
	cd $(COMPONENT_DIR) && $(MAKE) tidy-components

###################
# Distro Generation
###################

GEN_OTEL = $(DISTROGEN_BIN) generate --spec $(SPEC_FILE) \
								 --registry ./components/registry.yaml \
								 --templates ./templates

.PHONY: gen
gen: distrogen
	@$(GEN_OTEL)


.PHONY: regen
regen: distrogen
	@$(GEN_OTEL) --force


regen-v: distrogen
	@$(GEN_OTEL) --force -v


###########
# Workspace
###########

ALL_DIRECTORIES = find . -type d  -print0
EXCLUDE_TOOLS_DIRS = grep -z -v ".*\.tools.*"
EXCLUDE_BUILD_DIRS = grep -z -v -e ".*_build.*" -e ".*dist.*"

go.work:
	go work init
	$(MAKE) workspace-update

.PHONY: workspace-update
workspace-update: go.work
	$(ALL_DIRECTORIES) |\
	$(EXCLUDE_TOOLS_DIRS) |\
	$(EXCLUDE_BUILD_DIRS) |\
	xargs -0 go work use

.PHONY: clean-workspace
clean-workspace:
	rm -f go.work
	rm -f go.work.sum
