FUZZTIME ?= 5s

# TODO: This could be better
.PHONY: test
test:
	go test ./...

.PHONY: tidy
tidy:
	go mod tidy

.PHONY: generate
generate:
	go generate ./...

# Fuzz each target individually (`go test -fuzz` allows one package and one target per run).
.PHONY: fuzz
FUZZ_FIND := find .
FUZZ_FIND += \( -name .git -o -name .tools \) -prune                  # skip .git and .tools
FUZZ_FIND += -o ! -path . -type d -exec test -f '{}/go.mod' \; -prune # skip nested go.mod
FUZZ_FIND += -o -name '*_test.go' -exec grep -l '^func Fuzz' {} +     # find Fuzz tests
fuzz:
	@set -eu; \
	dirs=$$($(FUZZ_FIND) | xargs -r dirname | sort -u); \
	for dir in $$dirs; do \
		list=$$(go test -list='^Fuzz' "$$dir"); \
		for target in $$(printf '%s\n' "$$list" | grep '^Fuzz' || true); do \
			echo "=== Fuzzing $$dir: $$target (fuzztime=$(FUZZTIME)) ==="; \
			go test -run='^$$' -fuzz="^$${target}$$" -fuzztime=$(FUZZTIME) "$$dir"; \
		done; \
	done
