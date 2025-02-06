FROM alpine:3.19 as certs
RUN apk --update add ca-certificates

FROM alpine:3.19

ARG USER_UID=10001

RUN mkdir -p /config
RUN chown ${USER_UID}:${USER_UID} /config

USER ${USER_UID}

COPY --from=certs /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --chmod=755 otelcol-google /otelcol-google
COPY config-standard.yaml /etc/otelcol-google/config-standard.yaml
COPY config-k8s.yaml /etc/otelcol-google/config-k8s.yaml
COPY --chmod=755 start_otelcol.sh /start_otelcol.sh
ENTRYPOINT ["sh", "-c", "/start_otelcol.sh", "$@"]
CMD ["--config=/etc/otelcol-google/config.yaml"]
