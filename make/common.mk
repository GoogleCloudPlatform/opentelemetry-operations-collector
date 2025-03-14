# TODO: This could be better
.PHONY: test
test:
	go test -tags=$(GO_BUILD_TAGS) ./...

.PHONY: tidy
tidy:
	go mod tidy

.PHONY: generate
generate:
	go generate -tags=gpu ./...