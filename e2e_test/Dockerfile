#ARG GO_VERSION=1.21.0

FROM gcr.io/cloud-builders/go:debian-1.21 as go

FROM gcr.io/google.com/cloudsdktool/cloud-sdk:debian_component_based

COPY --from=go /builder /builder
COPY --from=go /usr/local/go /usr/local/go
WORKDIR /builder/bin
RUN mkdir src
ENV PATH=/builder/bin:$PATH
ENV PATH=/usr/local/go/bin:$PATH
ENTRYPOINT ["go.bash"]

#RUN apt-get update -qqy && apt-get dist-upgrade -yq && apt-get install -qqy git mercurial subversion gcc
#
## Install golang
#ARG GO_VERSION
#ADD https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz /tmp/go${GO_VERSION}.tar.gz
#RUN set -xe; \
#    tar -xf /tmp/go${GO_VERSION}.tar.gz -C /usr/local
#
#ENV GOPATH=
#
#RUN mkdir /builder
#
#COPY go_workspace.go prepare_workspace.inc /builder/
#
#COPY go.bash /builder/bin/
#ENV PATH=/builder/bin:$PATH
#
#RUN go build -o /builder/go_workspace /builder/go_workspace.go
#
#ENTRYPOINT ["go.bash"]
