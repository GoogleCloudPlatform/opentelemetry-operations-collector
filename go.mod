module github.com/GoogleCloudPlatform/opentelemetry-operations-collector

go 1.14

require (
	github.com/mitchellh/go-testing-interface v1.0.3 // indirect
	github.com/onsi/ginkgo v1.14.1 // indirect
	github.com/onsi/gomega v1.10.2 // indirect
	github.com/open-telemetry/opentelemetry-collector-contrib/exporter/googlecloudexporter v0.28.0
	github.com/open-telemetry/opentelemetry-collector-contrib/processor/metricstransformprocessor v0.28.0
	github.com/open-telemetry/opentelemetry-collector-contrib/processor/resourcedetectionprocessor v0.28.0
	github.com/open-telemetry/opentelemetry-collector-contrib/receiver/nginxreceiver v0.29.0
	github.com/open-telemetry/opentelemetry-collector-contrib/receiver/prometheusexecreceiver v0.28.0
	github.com/open-telemetry/opentelemetry-collector-contrib/receiver/windowsperfcountersreceiver v0.28.0
	github.com/shirou/gopsutil v3.21.5+incompatible
	github.com/stretchr/testify v1.7.0
	go.opentelemetry.io/collector v0.29.0
	go.uber.org/zap v1.17.0
	golang.org/x/sys v0.0.0-20210611083646-a4fc73990273
)
