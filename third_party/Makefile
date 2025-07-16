GO_ONIGURUMA_RELEASE = v1.2.1
ONIGURUMA_RELEASE = v6.9.10

# Clone release v1.2.1 of github.com:go-enry/go-oniguruma
go-oniguruma:
	DIR=$$(mktemp -d) && \
	echo "$(abspath $@)" && \
	cd $$DIR && \
	git clone --branch $(GO_ONIGURUMA_RELEASE) git@github.com:go-enry/go-oniguruma.git go-oniguruma && \
	cd go-oniguruma && \
	git apply $(abspath go-oniguruma.patch) && \
	rm -rf "$(abspath $@)" && \
	mkdir "$(abspath $@)" && \
	cp -a $$DIR"/go-oniguruma"/* "$(abspath $@)"

# Clone release v6.9.10 of github.com:kkos/oniguruma
oniguruma:
	DIR=$$(mktemp -d) && \
	echo "$(abspath $@)" && \
	cd $$DIR && \
	git clone --branch $(ONIGURUMA_RELEASE) git@github.com:kkos/oniguruma.git oniguruma && \
	cd oniguruma && \
	git apply $(abspath oniguruma.patch) && \
	rm -rf "$(abspath $@)" && \
	mkdir "$(abspath $@)" && \
	cp -a $$DIR"/oniguruma"/* "$(abspath $@)"

# Copy all files in go-oniguruma/* folder to a cloned repo and "git diff" into go-oniguruma.patch
go-oniguruma.patch: go-oniguruma/*
	DIR=$$(mktemp -d) && \
	cd $$DIR && \
	git clone --branch $(GO_ONIGURUMA_RELEASE) git@github.com:go-enry/go-oniguruma.git go-oniguruma && \
	rm -rf $$DIR"/go-oniguruma"/* && \
	cp -rf "$(abspath go-oniguruma)"/* $$DIR"/go-oniguruma" && \
	cd $$DIR"/go-oniguruma" && \
	git add --all --force && \
	git diff --staged $(GO_ONIGURUMA_RELEASE) > $(abspath $@)

# Copy all files in oniguruma/* folder to a cloned repo and "git diff" into oniguruma.patch
oniguruma.patch: oniguruma/*
	DIR=$$(mktemp -d) && \
	cd $$DIR && \
	git clone --branch $(ONIGURUMA_RELEASE) git@github.com:kkos/oniguruma.git oniguruma && \
	rm -rf $$DIR"/oniguruma"/* && \
	cp -rf "$(abspath oniguruma)"/* $$DIR"/oniguruma" && \
	cd $$DIR"/oniguruma" && \
	git add --all --force && \
	git diff --staged $(ONIGURUMA_RELEASE) > $(abspath $@)

.PHONY: get_dependencies
get_dependencies: go-oniguruma oniguruma

.PHONY: clean
clean:
	rm -rf go-oniguruma
	rm -rf oniguruma