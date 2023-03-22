FROM golang:1.20-bullseye

RUN apt-get -y update \
    && apt-get -y install \
    gettext-base \
    && apt-get -y clean \
    && rm -rf /var/lib/apt/lists/*

ENV GO111MODULE=on

RUN go install github.com/client9/misspell/cmd/misspell@v0.3.4 \
    && go install github.com/golangci/golangci-lint/cmd/golangci-lint@v1.52.1 \
    && go install github.com/google/addlicense@v1.0.0 \
    && go install github.com/google/googet/goopack@latest \
    && go install github.com/pavius/impi/cmd/impi@v0.0.3
