FROM golang:1.14-stretch

RUN apt-get -y update \
    && apt-get -y install \
        gettext-base \
    && apt-get -y clean \
    && rm -rf /var/lib/apt/lists/*

ADD "generate_tar.sh" "."
ADD "gcp-config.yaml" "."

CMD ["./generate_tar.sh"]