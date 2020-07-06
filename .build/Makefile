# use "0.0.0" as default version if not supplied
ifndef VERSION
override VERSION=0.0.0
endif

# set GOARCH based on ARCH
ifeq ($(ARCH),x86_64)
GOARCH=amd64
else ifeq ($(ARCH),x86)
GOARCH=386
else
$(error "ARCH must be set to one of: x86, x86_64")
endif

export ARCH
export GOARCH
export VERSION

.PHONY: package-googet
package-googet: build pack-googet

.PHONY: build
build:
	GOOS=windows go build -o ./bin/otelopscol_windows_$(GOARCH).exe ./cmd/otelopscol

.PHONY: pack-googet
pack-googet: SHELL:=/bin/bash
pack-googet:
	# goopack doesn't support variable replacement or command line args so just use envsubst
	goopack -output_dir ./bin <(envsubst < ./packaging/googet/google-cloudops-opentelemetry-collector.goospec)
