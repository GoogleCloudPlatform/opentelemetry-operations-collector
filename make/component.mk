OTEL_VER ?= latest
OTEL_STABLE_VER ?= latest

STABLE_COMPONENTS_PATTERN = -E "^go.opentelemetry.io/collector/pdata|^go.opentelemetry.io/collector/featuregate"
LIST_DIRECT_MODULES = go list -m -f '{{if not (or .Indirect .Main)}}{{.Path}}{{end}}' all
INCLUDE_COLLECTOR_BASE_COMPONENTS = grep "^go.opentelemetry.io" | grep -v "^go.opentelemetry.io/otel"
INCLUDE_COLLECTOR_STABLE_BASE_COMPONENTS = grep $(STABLE_COMPONENTS_PATTERN)
EXCLUDE_COLLECTOR_STABLE_BASE_COMPONENTS = grep -v $(STABLE_COMPONENTS_PATTERN)
INCLUDE_CONTRIB_COMPONENTS = grep "^github.com/open-telemetry/opentelemetry-collector-contrib"
GET_ALL_AT_OTEL_VER = xargs -t -I '{}' go get {}@$(OTEL_VER)
GET_ALL_AT_OTEL_STABLE_VER = xargs -t -I '{}' go get {}@$(OTEL_STABLE_VER)

# TODO: This could be better
.PHONY: test
test:
	go test ./...

.PHONY: update-components
update-components: stable-components base-components contrib-components

.PHONY: base-components
base-components:
	$(LIST_DIRECT_MODULES) | \
		$(INCLUDE_COLLECTOR_BASE_COMPONENTS) | \
		$(EXCLUDE_COLLECTOR_STABLE_BASE_COMPONENTS) | \
		$(GET_ALL_AT_OTEL_VER)

.PHONY: base-stable-components
stable-components:
	$(LIST_DIRECT_MODULES) | \
		$(INCLUDE_COLLECTOR_STABLE_BASE_COMPONENTS) | \
		$(GET_ALL_AT_OTEL_STABLE_VER)

.PHONY: contrib-components
contrib-components:
	$(LIST_DIRECT_MODULES) | \
		$(INCLUDE_CONTRIB_COMPONENTS) | \
		$(GET_ALL_AT_OTEL_VER)
