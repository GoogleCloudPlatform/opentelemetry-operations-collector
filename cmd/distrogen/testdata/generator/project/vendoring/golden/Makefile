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
DISTRO_DIR = $(PWD)/basic-distro

$(DISTROGEN_BIN):
	$(MAKE) tools-dir
	GOBIN=$(TOOLS_DIR) go install github.com/GoogleCloudPlatform/opentelemetry-operations-collector/cmd/distrogen@$(DISTROGEN_VERSION)

$(MDATAGEN_BIN):
	$(MAKE) tools-dir
	export OTEL_VERSION=v$(shell $(DISTROGEN_QUERY) opentelemetry_version) && \
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
	export OTEL_VERSION=v$(shell $(DISTROGEN_QUERY) opentelemetry_version) && \
	export OTEL_CONTRIB_VERSION=v$(shell $(DISTROGEN_QUERY) opentelemetry_contrib_version) && \
	export PATH="$(TOOLS_DIR):${PATH}" && \
	cd components && \
	$(MAKE) update-components
	$(MAKE) gen-update

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
	cd $(DISTRO_DIR) && make ocb-generate

.PHONY: gen-update
gen-update: distrogen
	@$(GEN_OTEL)
	cd $(DISTRO_DIR) && make update-ocb-directory

.PHONY: regen
regen: distrogen
	@$(GEN_OTEL) --force
	cd $(DISTRO_DIR) && make ocb-generate

regen-v: distrogen
	@$(GEN_OTEL) --force -v
	cd $(DISTRO_DIR) && make ocb-generate

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
