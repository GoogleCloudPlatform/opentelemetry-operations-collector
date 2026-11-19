FUZZTIME ?= 5s

# TODO: This could be better
.PHONY: test
test:
	go test ./...

.PHONY: fuzz
fuzz:
	@set -eu; \
	dirs=$$(find . \( -name '.git' -o -name '.tools' \) -prune -o \( ! -path . -type d -exec test -f '{}/go.mod' \; -prune \) -o \( -name '*_test.go' -type f -exec grep -l '^func Fuzz' {} + \) | while IFS= read -r f; do dirname "$$f"; done | sort -u); \
	for dir in $$dirs; do \
		list=$$(go test -list='^Fuzz' "$$dir"); \
		for target in $$(printf '%s\n' "$$list" | grep '^Fuzz' || true); do \
			echo "=== Fuzzing $$dir: $$target (fuzztime=$(FUZZTIME)) ==="; \
			go test -run='^$$' -fuzz="^$${target}$$" -fuzztime=$(FUZZTIME) "$$dir"; \
		done; \
	done

.PHONY: tidy
tidy:
	go mod tidy

.PHONY: generate
generate:
	go generate ./...
