SHELL := /bin/bash
DISTROGEN_BIN ?= distrogen

OTEL_VERSION ?= latest
OTEL_CONTRIB_VERSION ?= latest

# -mod=mod allows `go list -m` to succeed even when go.mod/go.sum needs updates
# or a vendor/ directory is present. GOWORK=off is required because Go rejects
# -mod=mod in workspace mode (e.g. when running `make update-components`
# directly inside a component directory while a root go.work file exists).
LIST_DIRECT_MODULES = GOWORK=off go list -mod=mod -m -f '{{if not (or .Indirect .Main)}}{{.Path}}{{end}}' all
# Pre-populate target modules in GOMODCACHE via `go mod download` before `go get`:
# when the root `go.opentelemetry.io/collector@v0.X.Y` module is already in
# GOMODCACHE, `go get <submodule>@v0.X.Y` treats its argument as a package path,
# matches the cached root module, and fails with "does not contain package".
GO_GET_ALL = xargs --no-run-if-empty -t sh -c 'go mod download "$$@" && go get -tags=gpu "$$@"' _

# Upstream modules are resolved through otel_component_versions because a
# release publishes several module sets at once, so a module may be at 1.x
# while the release itself is 0.x. Non-OpenTelemetry modules on stdin are
# ignored by otel_component_versions.
# Core and Contrib modules are updated together in a single `go get` call so Go
# resolves interdependent module upgrades atomically.
.PHONY: update-components
update-components:
	set -o pipefail; \
	$(LIST_DIRECT_MODULES) | \
		$(DISTROGEN_BIN) otel_component_versions \
			--otel_version $(OTEL_VERSION) \
			--otel_contrib_version $(OTEL_CONTRIB_VERSION) | \
		$(GO_GET_ALL)

.PHONY: core-components
core-components:
	set -o pipefail; \
	$(LIST_DIRECT_MODULES) | \
		$(DISTROGEN_BIN) otel_component_versions --otel_version $(OTEL_VERSION) | \
		$(GO_GET_ALL)

.PHONY: contrib-components
contrib-components:
	set -o pipefail; \
	$(LIST_DIRECT_MODULES) | \
		$(DISTROGEN_BIN) otel_component_versions --otel_contrib_version $(OTEL_CONTRIB_VERSION) | \
		$(GO_GET_ALL)
