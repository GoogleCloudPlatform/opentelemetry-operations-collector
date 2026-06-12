FROM golang:1.26 AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -o /distrogen ./cmd/distrogen

FROM gcr.io/distroless/static:nonroot

COPY --from=builder /distrogen /distrogen

ENTRYPOINT ["/distrogen"]
