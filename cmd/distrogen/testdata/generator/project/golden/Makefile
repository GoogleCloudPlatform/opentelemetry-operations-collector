include ./make/common.mk
include ./make/maintenance.mk

MAKEFLAGS += --no-print-directory

SPEC_FILE = spec.yaml

#############
# Development
#############

.PHONY: dev-setup
dev-setup: install-distrogen workspace setup-hooks

.PHONY: setup-hooks
setup-hooks:
	git config core.hooksPath $(PWD)/hooks

.PHONY: precommit
precommit: misspell lint test-distrogen

.PHONY: presubmit
presubmit: misspell lint

#######
# Tools
#######

TOOLS_DIR ?= $(PWD)/.tools
DISTROGEN_BIN ?= $(TOOLS_DIR)/distrogen

$(DISTROGEN_BIN): tools-dir
	GOBIN=$(TOOLS_DIR) go install github.com/GoogleCloudPlatform/opentelemetry-operations-collector/cmd/distrogen@$(shell cat .distrogen/VERSION)

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

.PHONY: update-otel-components

update-otel-components: COMPONENT_DIR := components
update-otel-components: DISTROGEN_QUERY := $(DISTROGEN_BIN) query --spec $(SPEC_FILE) --field
update-otel-components: export OTEL_VERSION = v$(shell $(DISTROGEN_QUERY) opentelemetry_version)
update-otel-components: export OTEL_CONTRIB_VERSION = v$(shell $(DISTROGEN_QUERY) opentelemetry_contrib_version)
update-otel-components: go.work
	cd $(COMPONENT_DIR) && PATH="$(TOOLS_DIR):${PATH}" $(MAKE) update-components

.PHONY: test-otel-components

test-otel-components: COMPONENT_DIR := components
test-otel-components: go.work
	cd $(COMPONENT_DIR) && $(MAKE) test-components

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

#########
# Testing
#########

.PHONY: test-all
test-all:
	TARGET="test" $(MAKE) target-all-modules

.PHONY: tidy-all
tidy-all:
	TARGET="tidy" $(MAKE) target-all-modules

.PHONY: test-distrogen
test-distrogen:
	@go test ./cmd/distrogen

.PHONY: distrogen-golden-update
distrogen-golden-update:
	@go test ./cmd/distrogen -update

###########
# Workspace
###########

ALL_DIRECTORIES = find . -type d  -print0
EXCLUDE_TOOLS_DIRS = grep -z -v ".*\.tools.*"
EXCLUDE_BUILD_DIRS = grep -z -v -e ".*_build.*" -e ".*dist.*"

.PHONY: workspace
workspace: go.work

go.work:
	go work init
	$(ALL_DIRECTORIES) |\
	$(EXCLUDE_TOOLS_DIRS) |\
	$(EXCLUDE_BUILD_DIRS) |\
	xargs -0 go work use

.PHONY: clean-workspace
clean-workspace:
	rm -f go.work
	rm -f go.work.sum

#########
# Utility
#########

# This target will tag the git repo using the OTel version. Eventually this may be
# more sophisticated if we want to supply separate tags for every subcomponent. For
# now it is pretty simply.
.PHONY: tag-repo
tag-repo: VERSION = v$(shell go run ./cmd/distrogen query -spec $(SPEC_FILE) -field version)
tag-repo:
	git tag -a $(VERSION) -m "Update to OpenTelemetry Collector version $(OTEL_VERSION)"
	@echo "Created git tag $(VERSION). If it looks good, push it to the remote by running: git push origin $(VERSION)"

.PHONY: target-all-modules
target-all-modules: go.work
ifndef TARGET
	@echo "No TARGET defined."
else
	go list -f '{{ .Dir }}' -m | grep -v -e ".*/.tools.*" -e ".*integration_test/smoke_test.*" |\
	GOWORK=off xargs -t -I '{}' $(MAKE) -C {} $(TARGET)
endif

.PHONY: update-go-module-in-all
update-go-module-in-all:
ifndef GO_MOD
	@echo "must specify a GO_MOD"
else
	TARGET=update-go-module $(MAKE) target-all-modules GO_MOD=$(GO_MOD)$(if "$(GO_MOD_VERSION), GO_MOD_VERSION=$(GO_MOD_VERSION),)
endif
