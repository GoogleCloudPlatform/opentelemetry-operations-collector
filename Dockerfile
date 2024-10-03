#ARG GO_VERSION=1.23.0
#
#FROM gcr.io/google.com/cloudsdktool/google-cloud-cli:alpine as build
#
## Install git to clone the repo
#RUN apk update && apk add --no-cache git && apk add --no-cache make
#
## Install golang
#ARG GO_VERSION
#ADD https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz /tmp/go${GO_VERSION}.tar.gz
#RUN set -xe; \
#    tar -xf /tmp/go${GO_VERSION}.tar.gz -C /usr/local
#ENV PATH="${PATH}:/usr/local/go/bin"
#
#RUN git clone https://github.com/GoogleCloudPlatform/opentelemetry-operations-collector.git
#
#WORKDIR /opentelemetry-operations-collector
#
#RUN make build
#
#FROM scratch
#
#COPY --from=build /opentelemetry-operations-collector/bin/otelopscol /
#COPY config.yaml /config.yaml
#ENTRYPOINT ["/otelopscol", "--config=config.yaml"]
#EXPOSE 4317

#
#ARG GO_VERSION=1.21.0
#
#FROM gcr.io/google.com/cloudsdktool/google-cloud-cli:debian as build
#
## Install git to clone the repo
#RUN apk update && apk add --no-cache git && apk add --no-cache make
#
## Install golang
#ARG GO_VERSION
#ADD https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz /tmp/go${GO_VERSION}.tar.gz
#RUN set -xe; \
#    tar -xf /tmp/go${GO_VERSION}.tar.gz -C /usr/local
#ENV PATH="${PATH}:/usr/local/go/bin"
#
#ENTRYPOINT ["go.bash"]


FROM otel/opentelemetry-collector-contrib
COPY config.yaml /etc/otelcol-contrib/config.yaml
ENTRYPOINT ["/otelcol-contrib"]
CMD ["--config", "/etc/otelcol-contrib/config.yaml"]
EXPOSE 4317 55678 55679
