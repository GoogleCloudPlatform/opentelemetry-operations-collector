DISTROGEN_BIN ?= distrogen

OTEL_VERSION ?= latest
OTEL_CONTRIB_VERSION ?= latest

LIST_DIRECT_MODULES = go list -m -f '{{if not (or .Indirect .Main)}}{{.Path}}{{end}}' all
INCLUDE_COLLECTOR_CORE_COMPONENTS = grep "^go.opentelemetry.io" | grep -v "^go.opentelemetry.io/otel"
INCLUDE_CONTRIB_COMPONENTS = grep "^github.com/open-telemetry/opentelemetry-collector-contrib"
GO_GET_ALL = xargs --no-run-if-empty -t -I '{}' go get -tags=gpu {}

.PHONY: update-components
update-components: core-components contrib-components

# Upstream modules are resolved through otel_component_versions because a
# release publishes several module sets at once, so a module may be at 1.x
# while the release itself is 0.x.

.PHONY: core-components
core-components:
	$(LIST_DIRECT_MODULES) | \
		$(INCLUDE_COLLECTOR_CORE_COMPONENTS) | \
		$(DISTROGEN_BIN) otel_component_versions --otel_version $(OTEL_VERSION) | \
		$(GO_GET_ALL)

.PHONY: contrib-components
contrib-components:
	$(LIST_DIRECT_MODULES) | \
		$(INCLUDE_CONTRIB_COMPONENTS) | \
		$(DISTROGEN_BIN) otel_component_versions --otel_contrib_version $(OTEL_CONTRIB_VERSION) | \
		$(GO_GET_ALL)
