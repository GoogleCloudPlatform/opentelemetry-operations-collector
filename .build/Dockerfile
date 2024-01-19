FROM golang:1.21.6-bullseye

RUN apt-get update && apt-get install gettext-base

# This is to fix https://github.com/golangci/golangci-lint/issues/4033
RUN git config --global --add safe.directory '*'

RUN go install github.com/client9/misspell/cmd/misspell@v0.3.4 \
    && go install github.com/golangci/golangci-lint/cmd/golangci-lint@v1.55.2 \
    && go install github.com/google/addlicense@v1.0.0 \
    && go install github.com/google/googet/goopack@latest
