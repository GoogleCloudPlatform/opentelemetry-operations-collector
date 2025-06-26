MAKEFLAGS += --no-print-directory

SPEC_FILE = google-built-opentelemetry-collector.yaml

DISTROGEN_BIN ?= distrogen2

###########
# Generate
###########

GENERATE_CMD = $(DISTROGEN_BIN) generate -spec $(SPEC_FILE) -templates ./templates

.PHONY: generate
generate:
	$(GENERATE_CMD)

.PHONY: regenerate
regenerate:
	$(GENERATE_CMD) -force

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

############
# Components
############

DISTROGEN_QUERY := $(DISTROGEN_BIN) generate -spec $(SPEC_FILE) -query

.PHONY: update-components
update-components: export OTEL_VERSION = v$(shell $(DISTROGEN_QUERY) opentelemetry_version)
update-components: export OTEL_CONTRIB_VERSION = v$(shell $(DISTROGEN_QUERY) opentelemetry_contrib_version)
update-components:
	cd $(COMPONENT_DIR) && PATH="$(TOOLS_DIR):${PATH}" GOWORK=off $(MAKE) update-components

