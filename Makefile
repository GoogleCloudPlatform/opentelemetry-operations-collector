OTEL_VERSION ?= v0.111.0
TOOLS_DIR = $(PWD)/.tools
ALL_DIRECTORIES = find . -type d  -print0
EXCLUDE_TOOLS_DIRS = grep -z -v ".*\.tools.*"
LIST_LOCAL_MODULES = go list -f "{{ .Dir }}" -m
PRIVATE_COMPONENT_PATTERN = ".*privatecomponents.*"
EXCLUDE_PRIVATE_MODULES = grep -v $(PRIVATE_COMPONENT_PATTERN)
INCLUDE_PRIVATE_MODULES = grep $(PRIVATE_COMPONENT_PATTERN)

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

.PHONY: clean
clean:
	rm go.work
	rm go.work.sum

.PHONY: go-work
go-work: go.work
	$(ALL_DIRECTORIES) |\
	$(EXCLUDE_TOOLS_DIRS) |\
	xargs -0 go work use

go.work:
	go work init
