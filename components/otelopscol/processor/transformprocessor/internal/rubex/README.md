# Rubex package

This package contains the `oniguruma` regex library and Go bindings to be able to use as a Go package. This is a combination of the following github repositories:

- https://github.com/kkos/oniguruma (which is a fork of https://github.com/moovweb/rubex/tree/go1)
    - License : [COPYING](COPYING)
- https://github.com/go-enry/go-oniguruma/
    - License : [LICENSE](LICENSE)

The sources of both projects are both added in the same repository to make sure that `go build` (with `cgo` enabled) will be able to find all the sources and build them into a go binary.