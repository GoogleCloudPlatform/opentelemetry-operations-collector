FROM golang:1.20.2-bullseye

RUN go install github.com/client9/misspell/cmd/misspell@v0.3.4 \
    && go install github.com/golangci/golangci-lint/cmd/golangci-lint@v1.52.1 \
    && go install github.com/google/addlicense@v1.0.0 \
    && go install github.com/google/googet/goopack@latest
