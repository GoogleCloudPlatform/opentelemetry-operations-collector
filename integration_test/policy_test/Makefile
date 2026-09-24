.PHONY: test tidy update-golden

OTELCOL ?= $(abspath ../../google-built-opentelemetry-collector/otelcol-google)
OTELCOL_FLAG := $(if $(wildcard $(OTELCOL)),-otelcol=$(OTELCOL),)

test:
	GOWORK=off go test -v ./... $(OTELCOL_FLAG)

tidy:
	GOWORK=off go mod tidy

update-golden:
	GOWORK=off go test -v ./... $(OTELCOL_FLAG) -update


