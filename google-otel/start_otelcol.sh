#!/bin/sh

CONFIG_LOCATION="/etc/otelcol-google/config-standard.yaml"
if [ ! -z "$KUBERNETES_SERVICE_HOST" ]; then
  CONFIG_LOCATION="/etc/otelcol-google/config-k8s.yaml"
fi

cp $CONFIG_LOCATION /etc/otelcol-google/config.yaml
/otelcol-google $@