FROM alpine:3.19 as certs
RUN apk --update add ca-certificates

FROM scratch

ARG USER_UID=10001
USER ${USER_UID}

COPY --from=certs /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --chmod=755 otelopscol /otelopscol
COPY config.yaml /etc/otelopscol/config.yaml
ENTRYPOINT ["/otelopscol"]
CMD ["--config", "/etc/otelopscol/config.yaml"]
