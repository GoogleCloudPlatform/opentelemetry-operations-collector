COMMON_MK_DIR := $(dir $(abspath $(lastword $(MAKEFILE_LIST))))
FUZZTIME ?= 5s

# TODO: This could be better
.PHONY: test
test:
	go test ./...

.PHONY: fuzz
fuzz:
	@FUZZTIME="$(FUZZTIME)" bash "$(COMMON_MK_DIR)/../internal/tools/scripts/fuzz.sh"

.PHONY: tidy
tidy:
	go mod tidy

.PHONY: generate
generate:
	go generate ./...
