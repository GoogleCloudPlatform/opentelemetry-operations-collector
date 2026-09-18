.PHONY: test update-golden

OTELCOL ?= $(abspath ../../google-built-opentelemetry-collector/otelcol-google)

test:
	GOWORK=off go test -v ./... -otelcol=$(OTELCOL)

update-golden:
	GOWORK=off go test -v ./... -otelcol=$(OTELCOL) -update
