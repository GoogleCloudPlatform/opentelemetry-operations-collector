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

# This is the same as precommit for now but this is
# futureproofing against this changing in the future.
.PHONY: presubmit
presubmit: checklicense misspell lint

##########################
# Updating OTel Components
##########################

OTEL_VERSION ?= v0.121.0
OTEL_CONTRIB_VERSION ?= v0.121.0

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

# This target will tag the git repo using the OTel version. Eventually this may be
# more sophisticated if we want to supply separate tags for every subcomponent. For
# now it is pretty simply.
.PHONY: tag-repo
tag-repo:
	git tag -a $(OTEL_VERSION) -m "Update to OpenTelemetry Collector version $(OTEL_VERSION)"
	@echo "Created git tag $(OTEL_VERSION). If it looks good, push it to the remote by running:\ngit push origin $(OTEL_VERSION)"

###################
# Distro Generation
###################

RUN_DISTROGEN=go run ./cmd/distrogen

.PHONY: gen-all
gen-all: gen-google-built-otel gen-otelopscol

.PHONY: regen-all
regen-all: regen-google-built-otel regen-otelopscol

GEN_GOOGLE_BUILT_OTEL=$(RUN_DISTROGEN) -spec ./specs/google-built-opentelemetry-collector.yaml \
								 -registry ./registries/operations-collector-registry.yaml \
								 -custom_templates ./templates/google-built-opentelemetry-collector
.PHONY: gen-google-built-otel
gen-google-built-otel:
	@$(GEN_GOOGLE_BUILT_OTEL)
	@$(MAKE) addlicense

.PHONY: regen-google-built-otel
regen-google-built-otel:
	@$(GEN_GOOGLE_BUILT_OTEL) -force
	@$(MAKE) addlicense

.PHONY: regen-google-built-otel-v
regen-google-built-otel-v:
	@$(GEN_GOOGLE_BUILT_OTEL) -force -v
	@$(MAKE) addlicense

GEN_OTELOPSCOL=$(RUN_DISTROGEN) -spec ./specs/otelopscol.yaml \
								-registry ./registries/operations-collector-registry.yaml \
								-custom_templates ./templates/otelopscol
.PHONY: gen-otelopscol
gen-otelopscol:
	@$(GEN_OTELOPSCOL)
	@$(MAKE) addlicense

.PHONY: regen-otelopscol
regen-otelopscol:
	@$(GEN_OTELOPSCOL) -force
	@$(MAKE) addlicense

.PHONY: regen-otelopscol
regen-otelopscol-v:
	@$(GEN_OTELOPSCOL) -force -v
	@$(MAKE) addlicense

#########
# Testing
#########

.PHONY: test-all
test-all:
	TARGET="test" $(MAKE) target-all-modules

.PHONY: tidy-all
tidy-all:
	TARGET="tidy" $(MAKE) target-all-modules

.PHONY: distrogen-golden-update
distrogen-golden-update:
	go test ./cmd/distrogen -update

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

ADDLICENSE_IGNORES = -ignore "**/.tools/*" \
					-ignore "**/docs/**/*" \
					-ignore "**/*.md" \
					-ignore "**/testdata/*" \
					-ignore "**/golden/*" \
					-ignore "**/spec.yaml"
.PHONY: addlicense
addlicense:
	@$(ADDLICENSE) -c "Google LLC" -l apache $(ADDLICENSE_IGNORES) .

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

LIST_LOCAL_MODULES = go list -f "{{ .Dir }}" -m | grep -v ".*internal/tools.*"
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
