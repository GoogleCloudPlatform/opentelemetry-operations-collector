# Google-Built OpenTelemetry Collector

The Google-Built OpenTelemetry Collector is an open-source, production-ready build of the upstream OpenTelemetry Collector that is built with upstream OpenTelemetry components.

# Components

## Receivers

| Component Name | Documentation |
| -------------- | ------------- |
| dockerstats | [docs](https://www.github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/receiver/dockerstatsreceiver/README.md) |
| filelog | [docs](https://www.github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/receiver/filelogreceiver/README.md) |
| fluentforward | [docs](https://www.github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/receiver/fluentforwardreceiver/README.md) |
| googlecloudmonitoring | [docs](https://www.github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/receiver/googlecloudmonitoringreceiver/README.md) |
| hostmetrics | [docs](https://www.github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/receiver/hostmetricsreceiver/README.md) |
| httpcheck | [docs](https://www.github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/receiver/httpcheckreceiver/README.md) |
| jaeger | [docs](https://www.github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/receiver/jaegerreceiver/README.md) |
| jmx | [docs](https://www.github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/receiver/jmxreceiver/README.md) |
| journald | [docs](https://www.github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/receiver/journaldreceiver/README.md) |
| k8scluster | [docs](https://www.github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/receiver/k8sclusterreceiver/README.md) |
| k8sevents | [docs](https://www.github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/receiver/k8seventsreceiver/README.md) |
| k8sobjects | [docs](https://www.github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/receiver/k8sobjectsreceiver/README.md) |
| kubeletstats | [docs](https://www.github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/receiver/kubeletstatsreceiver/README.md) |
| otelarrow | [docs](https://www.github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/receiver/otelarrowreceiver/README.md) |
| otlp | [docs](https://www.github.com/open-telemetry/opentelemetry-collector/tree/main/receiver/otlpreceiver/README.md) |
| otlpjsonfile | [docs](https://www.github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/receiver/otlpjsonfilereceiver/README.md) |
| prometheus | [docs](https://www.github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/receiver/prometheusreceiver/README.md) |
| receivercreator | [docs](https://www.github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/receiver/receivercreator/README.md) |
| redis | [docs](https://www.github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/receiver/redisreceiver/README.md) |
| statsd | [docs](https://www.github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/receiver/statsdreceiver/README.md) |
| syslog | [docs](https://www.github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/receiver/syslogreceiver/README.md) |
| tcplog | [docs](https://www.github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/receiver/tcplogreceiver/README.md) |
| zipkin | [docs](https://www.github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/receiver/zipkinreceiver/README.md) |


## Processors

| Component Name | Documentation |
| -------------- | ------------- |
| attributes | [docs](https://www.github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/processor/attributesprocessor/README.md) |
| batch | [docs](https://www.github.com/open-telemetry/opentelemetry-collector/tree/main/processor/batchprocessor/README.md) |
| cumulativetodelta | [docs](https://www.github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/processor/cumulativetodeltaprocessor/README.md) |
| deltatocumulative | [docs](https://www.github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/processor/deltatocumulativeprocessor/README.md) |
| deltatorate | [docs](https://www.github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/processor/deltatorateprocessor/README.md) |
| filter | [docs](https://www.github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/processor/filterprocessor/README.md) |
| groupbyattrs | [docs](https://www.github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/processor/groupbyattrsprocessor/README.md) |
| groupbytrace | [docs](https://www.github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/processor/groupbytraceprocessor/README.md) |
| interval | [docs](https://www.github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/processor/intervalprocessor/README.md) |
| k8sattributes | [docs](https://www.github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/processor/k8sattributesprocessor/README.md) |
| logdedup | [docs](https://www.github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/processor/logdedupprocessor/README.md) |
| memorylimiter | [docs](https://www.github.com/open-telemetry/opentelemetry-collector/tree/main/processor/memorylimiterprocessor/README.md) |
| metricsgeneration | [docs](https://www.github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/processor/metricsgenerationprocessor/README.md) |
| metricstarttime | [docs](https://www.github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/processor/metricstarttimeprocessor/README.md) |
| metricstransform | [docs](https://www.github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/processor/metricstransformprocessor/README.md) |
| probabilisticsampler | [docs](https://www.github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/processor/probabilisticsamplerprocessor/README.md) |
| redaction | [docs](https://www.github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/processor/redactionprocessor/README.md) |
| remotetap | [docs](https://www.github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/processor/remotetapprocessor/README.md) |
| resource | [docs](https://www.github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/processor/resourceprocessor/README.md) |
| resourcedetection | [docs](https://www.github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/processor/resourcedetectionprocessor/README.md) |
| tailsampling | [docs](https://www.github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/processor/tailsamplingprocessor/README.md) |
| transform | [docs](https://www.github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/processor/transformprocessor/README.md) |


## Exporters

| Component Name | Documentation |
| -------------- | ------------- |
| debug | [docs](https://www.github.com/open-telemetry/opentelemetry-collector/tree/main/exporter/debugexporter/README.md) |
| file | [docs](https://www.github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/exporter/fileexporter/README.md) |
| googlecloud | [docs](https://www.github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/exporter/googlecloudexporter/README.md) |
| googlemanagedprometheus | [docs](https://www.github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/exporter/googlemanagedprometheusexporter/README.md) |
| googleservicecontrol | [docs](No docs linked for component) |
| loadbalancing | [docs](https://www.github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/exporter/loadbalancingexporter/README.md) |
| nop | [docs](https://www.github.com/open-telemetry/opentelemetry-collector/tree/main/exporter/nopexporter/README.md) |
| otelarrow | [docs](https://www.github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/exporter/otelarrowexporter/README.md) |
| otlp | [docs](https://www.github.com/open-telemetry/opentelemetry-collector/tree/main/exporter/otlpexporter/README.md) |
| otlphttp | [docs](https://www.github.com/open-telemetry/opentelemetry-collector/tree/main/exporter/otlphttpexporter/README.md) |


## Extensions

| Component Name | Documentation |
| -------------- | ------------- |
| ack | [docs](https://www.github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/extension/ackextension/README.md) |
| basicauth | [docs](https://www.github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/extension/basicauthextension/README.md) |
| bearertokenauth | [docs](https://www.github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/extension/bearertokenauthextension/README.md) |
| filestorage | [docs](https://www.github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/extension/storage/README.md) |
| googleclientauth | [docs](https://www.github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/extension/googleclientauthextension/README.md) |
| headerssetter | [docs](https://www.github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/extension/headerssetterextension/README.md) |
| healthagent | [docs](No docs linked for component) |
| healthcheck | [docs](https://www.github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/extension/healthcheckextension/README.md) |
| healthcheckv2 | [docs](https://github.com/open-telemetry/opentelemetry-collector-contrib/blob/main/extension/healthcheckv2extension/README.md) |
| hostobserver | [docs](https://www.github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/extension/observer/README.md) |
| httpforwarder | [docs](https://www.github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/extension/httpforwarderextension/README.md) |
| k8sobserver | [docs](https://www.github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/extension/observer/README.md) |
| oauth2clientauth | [docs](No docs linked for component) |
| oidcauth | [docs](https://www.github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/extension/oidcauthextension/README.md) |
| opamp | [docs](https://www.github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/extension/opampextension/README.md) |
| pprof | [docs](https://www.github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/extension/pprofextension/README.md) |
| zpages | [docs](https://www.github.com/open-telemetry/opentelemetry-collector/tree/main/extension/zpagesextension/README.md) |


## Connectors

| Component Name | Documentation |
| -------------- | ------------- |
| count | [docs](https://www.github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/connector/countconnector/README.md) |
| exceptions | [docs](https://www.github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/connector/exceptionsconnector/README.md) |
| failover | [docs](https://www.github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/connector/failoverconnector/README.md) |
| forward | [docs](https://www.github.com/open-telemetry/opentelemetry-collector/tree/main/connector/forwardconnector/README.md) |
| otlpjson | [docs](https://www.github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/connector/otlpjsonconnector/README.md) |
| roundrobin | [docs](https://www.github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/connector/roundrobinconnector/README.md) |
| routing | [docs](https://www.github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/connector/routingconnector/README.md) |
| servicegraph | [docs](https://www.github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/connector/servicegraphconnector/README.md) |
| spanmetrics | [docs](https://www.github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/connector/spanmetricsconnector/README.md) |


## Providers

| Component Name | Documentation |
| -------------- | ------------- |
| env | [docs](https://www.github.com/open-telemetry/opentelemetry-collector/tree/main/confmap/provider/envprovider) |
| file | [docs](https://www.github.com/open-telemetry/opentelemetry-collector/tree/main/confmap/provider/fileprovider) |
| googlesecretmanager | [docs](https://www.github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/confmap/provider/googlesecretmanagerprovider) |
| http | [docs](https://www.github.com/open-telemetry/opentelemetry-collector/tree/main/confmap/provider/httpprovider) |
| https | [docs](https://www.github.com/open-telemetry/opentelemetry-collector/tree/main/confmap/provider/httpsprovider) |
| yaml | [docs](https://www.github.com/open-telemetry/opentelemetry-collector/tree/main/confmap/provider/yamlprovider) |

