module github.com/GoogleCloudPlatform/opentelemetry-operations-collector

go 1.14

require (
	github.com/StackExchange/wmi v0.0.0-20181212234831-e0a55b97c705 // indirect
	github.com/go-ole/go-ole v1.2.4 // indirect
	github.com/mitchellh/go-testing-interface v1.0.3 // indirect
	github.com/open-telemetry/opentelemetry-collector-contrib/exporter/stackdriverexporter v0.15.1-0.20201119200044-5843cc66428d
	github.com/open-telemetry/opentelemetry-collector-contrib/processor/metricstransformprocessor v0.15.1-0.20201119200044-5843cc66428d
	github.com/open-telemetry/opentelemetry-collector-contrib/processor/resourcedetectionprocessor v0.15.1-0.20201119200044-5843cc66428d
	github.com/open-telemetry/opentelemetry-collector-contrib/receiver/prometheusexecreceiver v0.15.1-0.20201119200044-5843cc66428d
	github.com/open-telemetry/opentelemetry-collector-contrib/receiver/windowsperfcountersreceiver v0.15.1-0.20201119200044-5843cc66428d
	github.com/shirou/gopsutil v3.20.10+incompatible
	github.com/stretchr/testify v1.6.1
	go.opentelemetry.io/collector v0.15.0
	go.uber.org/zap v1.16.0
	golang.org/x/sys v0.0.0-20201015000850-e3ed0017c211
)
