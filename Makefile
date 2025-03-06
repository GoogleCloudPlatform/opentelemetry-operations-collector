OTEL_VERSION ?= v0.111.0
ALL_DIRECTORIES = find . -type d  -print0
EXCLUDE_TOOLS_DIRS = grep -z -v ".*\.tools.*"
LIST_LOCAL_MODULES = go list -f "{{ .Dir }}" -m
PRIVATE_COMPONENT_PATTERN = ".*privatecomponents.*"
EXCLUDE_PRIVATE_MODULES = grep -v $(PRIVATE_COMPONENT_PATTERN)
INCLUDE_PRIVATE_MODULES = grep $(PRIVATE_COMPONENT_PATTERN)

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

###################
# Distro Generation
###################

RUN_DISTROGEN=go run ./cmd/distrogen

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

GEN_OTELOPSCOL=$(RUN_DISTROGEN) -registry ./registries/operations-collector-registry.yaml -spec ./specs/otelopscol.yaml
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

.PHONY: target-all-modules
target-all-modules:
ifndef TARGET
	@echo "No TARGET defined."
else
	$(LIST_LOCAL_MODULES) |\
	xargs -t -I '{}' $(MAKE) -C {} $(TARGET)
endif

.PHONY: target-public-modules
target-public-modules:
ifndef TARGET
	echo "No TARGET defined."
else
	$(LIST_LOCAL_MODULES) |\
	$(EXCLUDE_PRIVATE_MODULES) |\
	xargs -t -I '{}' $(MAKE) -C {} $(TARGET)
endif

.PHONY: target-private-modules
target-private-modules:
ifndef TARGET
	echo "No TARGET defined."
else
	$(LIST_LOCAL_MODULES) |\
	$(INCLUDE_PRIVATE_MODULES) |\
	xargs -t -I '{}' $(MAKE) -C {} $(TARGET)
endif

.PHONY: test
test:
	go test ./...

###########
# Workspace
###########

.PHONY: clean-workspace
clean-workspace:
	rm -f go.work
	rm -f go.work.sum

.PHONY: workspace
workspace: go.work
	$(ALL_DIRECTORIES) |\
	$(EXCLUDE_TOOLS_DIRS) |\
	xargs -0 go work use

go.work:
	go work init

.PHONY: update-mdatagen
update-mdatagen:
	go get -u go.opentelemetry.io/collector/cmd/mdatagen@$(OTEL_VERSION)

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

.PHONY: install-tools
install-tools: tools-dir
	cd internal/tools && \
	GOBIN=$(TOOLS_DIR) go install \
		github.com/google/addlicense \
		go.opentelemetry.io/collector/cmd/mdatagen \
		github.com/client9/misspell/cmd/misspell \
		github.com/golangci/golangci-lint/cmd/golangci-lint \
		golang.org/x/tools/cmd/goimports

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