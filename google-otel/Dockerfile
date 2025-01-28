FROM alpine:3.19 as certs
RUN apk --update add ca-certificates

FROM scratch

ARG USER_UID=10001
USER ${USER_UID}

COPY --from=certs /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --chmod=755 otelcol-google /otelcol-google
COPY config.yaml /etc/otelcol-google/config.yaml
ENTRYPOINT ["/otelcol-google" , "--feature-gates", "exporter.googlemanagedprometheus.intToDouble"]
CMD ["--config", "/etc/otelcol-google/config.yaml"]
