FROM golang:1.17-stretch

RUN apt-get -y update \
    && apt-get -y install \
        gettext-base \
    && apt-get -y clean \
    && rm -rf /var/lib/apt/lists/*

ENV GO111MODULE=on

RUN go get github.com/client9/misspell/cmd/misspell \
    && go get github.com/golangci/golangci-lint/cmd/golangci-lint \
    && go get github.com/google/addlicense \
    && go get github.com/google/googet/goopack \
    && go get github.com/pavius/impi/cmd/impi
