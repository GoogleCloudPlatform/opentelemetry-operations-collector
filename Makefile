include ./make/common.mk

#############
# Development
#############

.PHONY: dev-setup
dev-setup: install-tools workspace setup-hooks

.PHONY: setup-hooks
setup-hooks:
	git config core.hooksPath $(PWD)/hooks

.PHONY: precommit
precommit: checklicense misspell lint

##########################
# Updating OTel Components
##########################

OTEL_VERSION = v0.121.0
OTEL_CONTRIB_VERSION = v0.121.0

.PHONY: update-otel-components
update-otel-components: export OTEL_VERSION := $(OTEL_VERSION)
update-otel-components: export OTEL_CONTRIB_VERSION := $(OTEL_CONTRIB_VERSION)
update-otel-components: update-otel-components-deps tidy-all update-mdatagen generate-components

.PHONY: update-mdatagen
update-mdatagen:
	go get -u go.opentelemetry.io/collector/cmd/mdatagen@$(OTEL_VERSION)
	TOOL_LIST=go.opentelemetry.io/collector/cmd/mdatagen $(MAKE) install-tools

.PHONY: update-otel-components-deps
update-otel-components-deps:
	PATH="$(TOOLS_DIR):${PATH}" TARGET="update-components" $(MAKE) target-all-otel-components

.PHONY: generate-components
generate-components:
	PATH="$(TOOLS_DIR):${PATH}" TARGET="generate" $(MAKE) target-all-otel-components

###################
# Distro Generation
###################

RUN_DISTROGEN=go run ./cmd/distrogen

.PHONY: gen-all
gen-all: gen-google-otel gen-otelopscol

.PHONY: regen-all
regen-all: regen-google-otel regen-otelopscol

GEN_GOOGLE_OTEL=$(RUN_DISTROGEN) -registry ./registries/operations-collector-registry.yaml -spec ./specs/google-otel.yaml -custom_templates ./templates/google-otel
.PHONY: gen-google-otel
gen-google-otel:
	@$(GEN_GOOGLE_OTEL)

.PHONY: regen-google-otel
regen-google-otel:
	@$(GEN_GOOGLE_OTEL) -force

GEN_GOOGLE_OTEL_CONTRIB=$(RUN_DISTROGEN) -registry ./registries/operations-collector-registry.yaml -spec ./specs/google-otel-contrib.yaml -custom_templates ./templates/google-otel
.PHONY: gen-google-otel
gen-google-otel-contrib:
	@$(GEN_GOOGLE_OTEL_CONTRIB)

.PHONY: regen-google-otel
regen-google-otel-contrib:
	@$(GEN_GOOGLE_OTEL_CONTRIB) -force

GEN_OTELOPSCOL=$(RUN_DISTROGEN) -registry ./registries/operations-collector-registry.yaml -spec ./specs/otelopscol.yaml -custom_templates ./templates/otelopscol
.PHONY: gen-otelopscol
gen-otelopscol:
	@$(GEN_OTELOPSCOL)

.PHONY: regen-otelopscol
regen-otelopscol:
	@$(GEN_OTELOPSCOL) -force

#########
# Testing
#########

.PHONY: test-all
test-all:
	TARGET="test" $(MAKE) target-all-modules

.PHONY: tidy-all
tidy-all:
	TARGET="tidy" $(MAKE) target-all-modules

###########
# Workspace
###########

ALL_DIRECTORIES = find . -type d  -print0
EXCLUDE_TOOLS_DIRS = grep -z -v ".*\.tools.*"

.PHONY: workspace
workspace: go.work
	$(ALL_DIRECTORIES) |\
	$(EXCLUDE_TOOLS_DIRS) |\
	xargs -0 go work use

go.work:
	go work init

.PHONY: clean-workspace
clean-workspace:
	rm -f go.work
	rm -f go.work.sum

#######
# Tools
#######

TOOLS_DIR = $(PWD)/.tools

ADDLICENSE = $(TOOLS_DIR)/addlicense
GOLANGCI_LINT = $(TOOLS_DIR)/golangci-lint
MISSPELL = $(TOOLS_DIR)/misspell

# This is a PHONY target cause if you make it as a normal recipe
# it gets very confused because the creation date of the .tools
# directory is newer than the tools inside it.
.PHONY: tools-dir
tools-dir:
	@mkdir -p $(TOOLS_DIR)

TOOL_LIST ?= github.com/google/addlicense \
			 go.opentelemetry.io/collector/cmd/mdatagen \
			 github.com/client9/misspell/cmd/misspell \
			 github.com/golangci/golangci-lint/cmd/golangci-lint \
			 golang.org/x/tools/cmd/goimports \
			 ./cmd/otel_component_versions

.PHONY: install-tools
install-tools: tools-dir
	cd internal/tools && \
	GOBIN=$(TOOLS_DIR) go install \
	$(TOOL_LIST)

ADDLICENSE_IGNORES = -ignore "**/.tools/*" -ignore "**/otelopscol/**/*" -ignore "**/google-otel/**/*" -ignore "**/docs/**/*" -ignore "**/*.md" -ignore "**/testdata/*"
.PHONY: addlicense
addlicense:
	$(ADDLICENSE) -c "Google LLC" -l apache $(ADDLICENSE_IGNORES) .

.PHONY: checklicense
checklicense:
	@output=`$(ADDLICENSE) $(ADDLICENSE_IGNORES) -check .` && echo checklicense finished successfully || (echo checklicense errors, run make addlicense to resolve: $$output && exit 1)

.PHONY: lint
lint:
	$(GOLANGCI_LINT) run --allow-parallel-runners --build-tags=$(GO_BUILD_TAGS) --timeout=20m

.PHONY: lint-fix
lint-fix:
	$(GOLANGCI_LINT) run --fix --allow-parallel-runners --build-tags=$(GO_BUILD_TAGS) --timeout=20m

.PHONY: misspell
misspell:
	@output=`$(MISSPELL) -error ./docs` && echo misspell finished successfully || (echo misspell errors:\\n$$output && exit 1)

#########
# Utility
#########

LIST_LOCAL_MODULES = go list -f "{{ .Dir }}" -m
INCLUDE_OTEL_COMPONENTS = grep -e ".*receiver.*" -e ".*processor.*"

.PHONY: target-all-modules
target-all-modules:
ifndef TARGET
	@echo "No TARGET defined."
else
	$(LIST_LOCAL_MODULES) |\
	GOWORK=off xargs -t -I '{}' $(MAKE) -C {} $(TARGET)
endif

.PHONY: target-all-otel-components
target-all-otel-components:
ifndef TARGET
	@echo "No TARGET defined."
else
	$(LIST_LOCAL_MODULES) |\
	$(INCLUDE_OTEL_COMPONENTS) |\
	GOWORK=off xargs -t -I '{}' $(MAKE) -C {} $(TARGET)
endif
