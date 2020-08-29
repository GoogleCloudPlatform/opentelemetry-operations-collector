module github.com/GoogleCloudPlatform/opentelemetry-operations-collector

go 1.14

require (
	github.com/open-telemetry/opentelemetry-collector-contrib/exporter/stackdriverexporter v0.9.0
	github.com/open-telemetry/opentelemetry-collector-contrib/processor/metricstransformprocessor v0.9.0
	github.com/open-telemetry/opentelemetry-collector-contrib/processor/resourcedetectionprocessor v0.9.0
	github.com/open-telemetry/opentelemetry-collector-contrib/receiver/prometheusexecreceiver v0.9.0
	github.com/stretchr/testify v1.6.1
	go.opentelemetry.io/collector v0.9.0
	go.uber.org/zap v1.15.0
	golang.org/x/sys v0.0.0-20200828194041-157a740278f4
)

replace k8s.io/client-go => k8s.io/client-go v0.0.0-20190620085101-78d2af792bab
