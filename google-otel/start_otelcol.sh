#!/bin/sh

CONFIG_LOCATION="/etc/otelcol-google/config-standard.yaml"
if [[ ! -z "$KUBERNETES_SERVICE_HOST" ]]; then
  CONFIG_LOCATION="/etc/otelcol-google/config-k8s.yaml"
fi

ln $CONFIG_LOCATION /config/config.yaml
echo "args are: $@"
/otelcol-google --feature-gates=exporter.googlemanagedprometheus.intToDouble $@