include ./make/common.mk
include ./make/maintenance.mk

MAKEFLAGS += --no-print-directory

#############
# Development
#############

.PHONY: dev-setup
dev-setup: install-tools workspace setup-hooks

.PHONY: setup-hooks
setup-hooks:
	git config core.hooksPath $(PWD)/hooks

.PHONY: precommit
precommit: checklicense misspell lint compare-all test-distrogen

.PHONY: presubmit
presubmit: checklicense misspell lint compare-all


#######################
# Update Distributions
#######################

.PHONY: update-google-built-otel
update-google-built-otel: update-google-otel-components test-google-otel-components gen-google-built-otel

.PHONY: update-otelopscol
update-otelopscol: update-otelopscol-components test-otelopscol-components gen-otelopscol

##########################
# Updating OTel Components
##########################

.PHONY: update-google-otel-components update-otelopscol-components

update-google-otel-components: SPEC_FILE := specs/google-built-opentelemetry-collector.yaml
update-google-otel-components: COMPONENT_DIR := components/google-built-opentelemetry-collector

update-otelopscol-components: SPEC_FILE := specs/otelopscol.yaml
update-otelopscol-components: COMPONENT_DIR := components/otelopscol

update-google-otel-components update-otelopscol-components: DISTROGEN_QUERY := go run ./cmd/distrogen query --spec $(SPEC_FILE) --field
update-google-otel-components update-otelopscol-components: export OTEL_VERSION = v$(shell $(DISTROGEN_QUERY) opentelemetry_version)
update-google-otel-components update-otelopscol-components: export OTEL_CONTRIB_VERSION = v$(shell $(DISTROGEN_QUERY) opentelemetry_contrib_version)
update-google-otel-components update-otelopscol-components: go.work install-tools
	cd $(COMPONENT_DIR) && PATH="$(TOOLS_DIR):${PATH}" $(MAKE) update-components

.PHONY: test-google-otel-components test-otelopscol-components

test-google-otel-components: COMPONENT_DIR := components/google-built-opentelemetry-collector

test-otelopscol-components: COMPONENT_DIR := components/otelopscol

test-google-otel-components test-otelopscol-components: go.work
	cd $(COMPONENT_DIR) && $(MAKE) test-components

###################
# Distro Generation
###################

RUN_DISTROGEN=go run ./cmd/distrogen

.PHONY: gen-all
gen-all: distrogen-golden-update gen-google-built-otel gen-otelopscol

.PHONY: regen-all
regen-all: distrogen-golden-update regen-google-built-otel regen-otelopscol

.PHONY: compare-all
compare-all:
	@./internal/tools/scripts/compare.sh

GEN_GOOGLE_BUILT_OTEL=$(RUN_DISTROGEN) generate --spec ./specs/google-built-opentelemetry-collector.yaml \
								 --registry ./components/google-built-opentelemetry-collector/registry.yaml \
								 --templates ./templates/google-built-opentelemetry-collector
.PHONY: gen-google-built-otel
gen-google-built-otel:
	@$(GEN_GOOGLE_BUILT_OTEL)

.PHONY: regen-google-built-otel
regen-google-built-otel:
	@$(GEN_GOOGLE_BUILT_OTEL) --force

.PHONY: regen-google-built-otel-v
regen-google-built-otel-v:
	@$(GEN_GOOGLE_BUILT_OTEL) --force -v

.PHONY: compare-google-built-otel
compare-google-built-otel:
	@$(GEN_GOOGLE_BUILT_OTEL) --force --compare

GEN_OTELOPSCOL=$(RUN_DISTROGEN) generate --spec ./specs/otelopscol.yaml \
								--registry ./components/otelopscol/registry.yaml \
								--templates ./templates/otelopscol
.PHONY: gen-otelopscol
gen-otelopscol:
	@$(GEN_OTELOPSCOL)

.PHONY: regen-otelopscol
regen-otelopscol:
	@$(GEN_OTELOPSCOL) -f

.PHONY: regen-otelopscol-v
regen-otelopscol-v:
	@$(GEN_OTELOPSCOL) -f -v

.PHONY: compare-otelopscol
compare-otelopscol:
	@$(GEN_OTELOPSCOL) --force --compare

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

#######
# Tools
#######

TOOLS_DIR = $(PWD)/.tools

ADDLICENSE = $(TOOLS_DIR)/addlicense
GOLANGCI_LINT = $(TOOLS_DIR)/golangci-lint
MISSPELL = $(TOOLS_DIR)/misspell

$(ADDLICENSE): install-tools

# This is a PHONY target cause if you make it as a normal recipe
# it gets very confused because the creation date of the .tools
# directory is newer than the tools inside it.
.PHONY: tools-dir
tools-dir:
	@mkdir -p $(TOOLS_DIR)

TOOL_LIST ?= github.com/google/addlicense \
			 github.com/client9/misspell/cmd/misspell \
			 github.com/golangci/golangci-lint/cmd/golangci-lint \
			 golang.org/x/tools/cmd/goimports \
			 ./cmd/otel_component_versions

.PHONY: install-tools
install-tools: tools-dir
	cd internal/tools && \
	GOBIN=$(TOOLS_DIR) go install \
	$(TOOL_LIST)

ADDLICENSE_IGNORES = -ignore "**/.tools/**/*" \
					-ignore "**/dist/**/*" \
					-ignore "**/docs/**/*" \
					-ignore "**/*.md" \
					-ignore "**/testdata/*" \
					-ignore "**/golden/*" \
					-ignore "**/google-built-opentelemetry-collector/*" \
					-ignore "**/otelopscol/*" \
					-ignore "**/spec.yaml" \
					-ignore "**/third_party/**/*"
.PHONY: addlicense
addlicense: $(ADDLICENSE)
	@$(ADDLICENSE) -c "Google LLC" -l apache $(ADDLICENSE_IGNORES) .

.PHONY: checklicense
checklicense: $(ADDLICENSE)
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

# This target will tag the git repo using the OTel version. Eventually this may be
# more sophisticated if we want to supply separate tags for every subcomponent. For
# now it is pretty simply.
.PHONY: tag-repo
tag-repo: GBOC_TAG = v$(shell go run ./cmd/distrogen query --spec specs/google-built-opentelemetry-collector.yaml --field version)
tag-repo:
	bash ./internal/tools/scripts/tag.sh $(GBOC_TAG)

.PHONY: target-all-modules
target-all-modules: go.work
ifndef TARGET
	@echo "No TARGET defined."
else
	go list -f "{{ .Dir }}" -m | grep -v -e ".*internal/tools.*" -e ".*integration_test/smoke_test.*" |\
	GOWORK=off xargs -t -I '{}' $(MAKE) -C {} $(TARGET)
endif

.PHONY: update-go-module-in-all
update-go-module-in-all:
ifndef GO_MOD
	@echo "must specify a GO_MOD"
else
	TARGET=update-go-module $(MAKE) target-all-modules GO_MOD=$(GO_MOD)$(if "$(GO_MOD_VERSION), GO_MOD_VERSION=$(GO_MOD_VERSION),)
endif
