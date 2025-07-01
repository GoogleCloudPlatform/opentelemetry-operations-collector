.PHONY: generate-third-party-deps
generate-third-party-deps:
	git clone git@github.com:go-enry/go-oniguruma.git
	cd go-oniguruma && \
	git checkout v1.2.1 && \
	git apply ../go-oniguruma.patch && \
	rm -rf .git  && \
	rm -rf .github

	git clone git@github.com:kkos/oniguruma.git
	cd oniguruma && \
	git checkout v6.9.10 && \
	git apply ../oniguruma.patch && \
	rm -rf .git && \
	rm -rf .github

.PHONY: clean-third-party-deps
clean-third-party-deps:
	rm -rf go-oniguruma
	rm -rf oniguruma