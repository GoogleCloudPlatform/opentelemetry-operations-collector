# Google Built OpenTelemetry Collector

A curated distribution of the OpenTelemetry Collector for use in GCP.

# Components

## Receivers

| Component Name | Documentation |
| -------------- | ------------- |
| filelog | [docs](https://github.com/open-telemetry/opentelemetry-collector-contrib/) |
| fluentforward | [docs](https://github.com/open-telemetry/opentelemetry-collector-contrib/) |
| hostmetrics | [docs](https://github.com/open-telemetry/opentelemetry-collector-contrib/) |
| httpcheck | [docs](https://github.com/open-telemetry/opentelemetry-collector-contrib/) |
| jaeger | [docs](https://github.com/open-telemetry/opentelemetry-collector-contrib/) |
| journald | [docs](https://github.com/open-telemetry/opentelemetry-collector-contrib/) |
| k8scluster | [docs](https://github.com/open-telemetry/opentelemetry-collector-contrib/) |
| k8sevents | [docs](https://github.com/open-telemetry/opentelemetry-collector-contrib/) |
| k8sobjects | [docs](https://github.com/open-telemetry/opentelemetry-collector-contrib/) |
| kubeletstats | [docs](https://github.com/open-telemetry/opentelemetry-collector-contrib/) |
| opencensus | [docs](https://github.com/open-telemetry/opentelemetry-collector-contrib/) |
| otelarrow | [docs](https://github.com/open-telemetry/opentelemetry-collector-contrib/) |
| otlp | [docs](https://github.com/open-telemetry/opentelemetry-collector-contrib/) |
| prometheus | [docs](https://github.com/open-telemetry/opentelemetry-collector-contrib/) |
| receivercreator | [docs](https://github.com/open-telemetry/opentelemetry-collector-contrib/) |
| zipkin | [docs](https://github.com/open-telemetry/opentelemetry-collector-contrib/) |


## Processors

| Component Name | Documentation |
| -------------- | ------------- |
| attributes | [docs](https://github.com/open-telemetry/opentelemetry-collector-contrib/) |
| batch | [docs](https://github.com/open-telemetry/opentelemetry-collector-contrib/) |
| cumulativetodelta | [docs](https://github.com/open-telemetry/opentelemetry-collector-contrib/) |
| deltatocumulative | [docs](https://github.com/open-telemetry/opentelemetry-collector-contrib/) |
| deltatorate | [docs](https://github.com/open-telemetry/opentelemetry-collector-contrib/) |
| filter | [docs](https://github.com/open-telemetry/opentelemetry-collector-contrib/) |
| groupbyattrs | [docs](https://github.com/open-telemetry/opentelemetry-collector-contrib/) |
| groupbytrace | [docs](https://github.com/open-telemetry/opentelemetry-collector-contrib/) |
| k8sattributes | [docs](https://github.com/open-telemetry/opentelemetry-collector-contrib/) |
| logdedup | [docs](https://github.com/open-telemetry/opentelemetry-collector-contrib/) |
| memorylimiter | [docs](https://github.com/open-telemetry/opentelemetry-collector-contrib/) |
| metricstransform | [docs](https://github.com/open-telemetry/opentelemetry-collector-contrib/) |
| probabilisticsampler | [docs](https://github.com/open-telemetry/opentelemetry-collector-contrib/) |
| redaction | [docs](https://github.com/open-telemetry/opentelemetry-collector-contrib/) |
| remotetap | [docs](https://github.com/open-telemetry/opentelemetry-collector-contrib/) |
| resource | [docs](https://github.com/open-telemetry/opentelemetry-collector-contrib/) |
| resourcedetection | [docs](https://github.com/open-telemetry/opentelemetry-collector-contrib/) |
| tailsampling | [docs](https://github.com/open-telemetry/opentelemetry-collector-contrib/) |
| transform | [docs](https://github.com/open-telemetry/opentelemetry-collector-contrib/) |


## Exporters

| Component Name | Documentation |
| -------------- | ------------- |
| debug | https://github.com/open-telemetry/opentelemetry-collector-contrib/ |
| file | https://github.com/open-telemetry/opentelemetry-collector-contrib/ |
| googlecloud | https://github.com/open-telemetry/opentelemetry-collector-contrib/ |
| googlemanagedprometheus | https://github.com/open-telemetry/opentelemetry-collector-contrib/ |
| loadbalancing | https://github.com/open-telemetry/opentelemetry-collector-contrib/ |
| nop | https://github.com/open-telemetry/opentelemetry-collector-contrib/ |
| otelarrow | https://github.com/open-telemetry/opentelemetry-collector-contrib/ |
| otlp | https://github.com/open-telemetry/opentelemetry-collector-contrib/ |
| otlphttp | https://github.com/open-telemetry/opentelemetry-collector-contrib/ |


## Extensions

| Component Name | Documentation |
| -------------- | ------------- |
| ack | https://github.com/open-telemetry/opentelemetry-collector-contrib/ |
| basicauth | https://github.com/open-telemetry/opentelemetry-collector-contrib/ |
| bearertokenauth | https://github.com/open-telemetry/opentelemetry-collector-contrib/ |
| filestorage | https://github.com/open-telemetry/opentelemetry-collector-contrib/ |
| headerssetter | https://github.com/open-telemetry/opentelemetry-collector-contrib/ |
| healthcheck | https://github.com/open-telemetry/opentelemetry-collector-contrib/ |
| hostobserver | https://github.com/open-telemetry/opentelemetry-collector-contrib/ |
| httpforwarder | https://github.com/open-telemetry/opentelemetry-collector-contrib/ |
| k8sobserver | https://github.com/open-telemetry/opentelemetry-collector-contrib/ |
| oauth2clientauth | https://github.com/open-telemetry/opentelemetry-collector-contrib/ |
| oidcauth | https://github.com/open-telemetry/opentelemetry-collector-contrib/ |
| opamp | https://github.com/open-telemetry/opentelemetry-collector-contrib/ |
| pprof | https://github.com/open-telemetry/opentelemetry-collector-contrib/ |
| zpages | https://github.com/open-telemetry/opentelemetry-collector-contrib/ |


## Connectors

| Component Name | Documentation |
| -------------- | ------------- |
| count | https://github.com/open-telemetry/opentelemetry-collector-contrib/ |
| exceptions | https://github.com/open-telemetry/opentelemetry-collector-contrib/ |
| failover | https://github.com/open-telemetry/opentelemetry-collector-contrib/ |
| forward | https://github.com/open-telemetry/opentelemetry-collector-contrib/ |
| otlpjson | https://github.com/open-telemetry/opentelemetry-collector-contrib/ |
| roundrobin | https://github.com/open-telemetry/opentelemetry-collector-contrib/ |
| routing | https://github.com/open-telemetry/opentelemetry-collector-contrib/ |
| servicegraph | https://github.com/open-telemetry/opentelemetry-collector-contrib/ |
| spanmetrics | https://github.com/open-telemetry/opentelemetry-collector-contrib/ |


## Providers

| Component Name | Documentation |
| -------------- | ------------- |
| env | https://github.com/open-telemetry/opentelemetry-collector-contrib/ |
| file | https://github.com/open-telemetry/opentelemetry-collector-contrib/ |
| http | https://github.com/open-telemetry/opentelemetry-collector-contrib/ |
| https | https://github.com/open-telemetry/opentelemetry-collector-contrib/ |
| yaml | https://github.com/open-telemetry/opentelemetry-collector-contrib/ |
