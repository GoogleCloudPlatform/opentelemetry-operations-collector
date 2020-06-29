FROM golang:1.14-stretch

RUN apt-get -y update \
    && apt-get -y install \
        gettext-base \
    && apt-get -y clean \
    && rm -rf /var/lib/apt/lists/*

RUN go get github.com/google/googet/goopack
