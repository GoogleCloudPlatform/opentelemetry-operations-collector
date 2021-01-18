module github.com/GoogleCloudPlatform/opentelemetry-operations-collector

go 1.14

require (
	github.com/StackExchange/wmi v0.0.0-20181212234831-e0a55b97c705 // indirect
	github.com/mitchellh/go-testing-interface v1.0.3 // indirect
	github.com/open-telemetry/opentelemetry-collector-contrib/exporter/stackdriverexporter v0.18.0
	github.com/open-telemetry/opentelemetry-collector-contrib/processor/metricstransformprocessor v0.18.0
	github.com/open-telemetry/opentelemetry-collector-contrib/processor/resourcedetectionprocessor v0.18.0
	github.com/open-telemetry/opentelemetry-collector-contrib/receiver/prometheusexecreceiver v0.18.0
	github.com/open-telemetry/opentelemetry-collector-contrib/receiver/windowsperfcountersreceiver v0.18.0
	github.com/shirou/gopsutil v3.20.12-0.20201210134652-afe0c04c5d5a+incompatible
	github.com/stretchr/testify v1.6.1
	go.opentelemetry.io/collector v0.18.0
	go.uber.org/zap v1.16.0
	golang.org/x/sys v0.0.0-20201214210602-f9fddec55a1e
)
