go-oniguruma:
	DIR=$$(mktemp -d) && \
	echo "$(abspath $@)" && \
	cd $$DIR && \
	git clone --branch v1.2.1 git@github.com:go-enry/go-oniguruma.git go-oniguruma && \
	cd go-oniguruma && \
	git apply $(abspath go-oniguruma.patch) && \
	rm -rf "$(abspath $@)" && \
	cp -a $$DIR"/go-oniguruma" "$(abspath $@)"

oniguruma:
	DIR=$$(mktemp -d) && \
	echo "$(abspath $@)" && \
	cd $$DIR && \
	git clone --branch v6.9.10 git@github.com:kkos/oniguruma.git oniguruma && \
	cd oniguruma && \
	git apply $(abspath oniguruma.patch) && \
	rm -rf "$(abspath $@)" && \
	cp -a $$DIR"/oniguruma" "$(abspath $@)"

.PHONY: get_dependencies
get_dependencies: go-oniguruma oniguruma

.PHONY: clean
clean:
	rm -rf go-oniguruma
	rm -rf oniguruma