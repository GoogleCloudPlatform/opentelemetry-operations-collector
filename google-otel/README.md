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
| debug | [docs](https://github.com/open-telemetry/opentelemetry-collector-contrib/) |
| file | [docs](https://github.com/open-telemetry/opentelemetry-collector-contrib/) |
| googlecloud | [docs](https://github.com/open-telemetry/opentelemetry-collector-contrib/) |
| googlemanagedprometheus | [docs](https://github.com/open-telemetry/opentelemetry-collector-contrib/) |
| loadbalancing | [docs](https://github.com/open-telemetry/opentelemetry-collector-contrib/) |
| nop | [docs](https://github.com/open-telemetry/opentelemetry-collector-contrib/) |
| otelarrow | [docs](https://github.com/open-telemetry/opentelemetry-collector-contrib/) |
| otlp | [docs](https://github.com/open-telemetry/opentelemetry-collector-contrib/) |
| otlphttp | [docs](https://github.com/open-telemetry/opentelemetry-collector-contrib/) |


## Extensions

| Component Name | Documentation |
| -------------- | ------------- |
| ack | [docs](https://github.com/open-telemetry/opentelemetry-collector-contrib/) |
| basicauth | [docs](https://github.com/open-telemetry/opentelemetry-collector-contrib/) |
| bearertokenauth | [docs](https://github.com/open-telemetry/opentelemetry-collector-contrib/) |
| filestorage | [docs](https://github.com/open-telemetry/opentelemetry-collector-contrib/) |
| headerssetter | [docs](https://github.com/open-telemetry/opentelemetry-collector-contrib/) |
| healthcheck | [docs](https://github.com/open-telemetry/opentelemetry-collector-contrib/) |
| hostobserver | [docs](https://github.com/open-telemetry/opentelemetry-collector-contrib/) |
| httpforwarder | [docs](https://github.com/open-telemetry/opentelemetry-collector-contrib/) |
| k8sobserver | [docs](https://github.com/open-telemetry/opentelemetry-collector-contrib/) |
| oauth2clientauth | [docs](https://github.com/open-telemetry/opentelemetry-collector-contrib/) |
| oidcauth | [docs](https://github.com/open-telemetry/opentelemetry-collector-contrib/) |
| opamp | [docs](https://github.com/open-telemetry/opentelemetry-collector-contrib/) |
| pprof | [docs](https://github.com/open-telemetry/opentelemetry-collector-contrib/) |
| zpages | [docs](https://github.com/open-telemetry/opentelemetry-collector-contrib/) |


## Connectors

| Component Name | Documentation |
| -------------- | ------------- |
| count | [docs](https://github.com/open-telemetry/opentelemetry-collector-contrib/) |
| exceptions | [docs](https://github.com/open-telemetry/opentelemetry-collector-contrib/) |
| failover | [docs](https://github.com/open-telemetry/opentelemetry-collector-contrib/) |
| forward | [docs](https://github.com/open-telemetry/opentelemetry-collector-contrib/) |
| otlpjson | [docs](https://github.com/open-telemetry/opentelemetry-collector-contrib/) |
| roundrobin | [docs](https://github.com/open-telemetry/opentelemetry-collector-contrib/) |
| routing | [docs](https://github.com/open-telemetry/opentelemetry-collector-contrib/) |
| servicegraph | [docs](https://github.com/open-telemetry/opentelemetry-collector-contrib/) |
| spanmetrics | [docs](https://github.com/open-telemetry/opentelemetry-collector-contrib/) |


## Providers

| Component Name | Documentation |
| -------------- | ------------- |
| env | [docs](https://github.com/open-telemetry/opentelemetry-collector-contrib/) |
| file | [docs](https://github.com/open-telemetry/opentelemetry-collector-contrib/) |
| http | [docs](https://github.com/open-telemetry/opentelemetry-collector-contrib/) |
| https | [docs](https://github.com/open-telemetry/opentelemetry-collector-contrib/) |
| yaml | [docs](https://github.com/open-telemetry/opentelemetry-collector-contrib/) |
