module github.com/GoogleCloudPlatform/opentelemetry-operations-collector

go 1.14

require (
	github.com/hashicorp/go-hclog v0.16.1 // indirect
	github.com/hashicorp/go-uuid v1.0.2 // indirect
	github.com/mitchellh/go-testing-interface v1.0.3 // indirect
	github.com/onsi/ginkgo v1.14.1 // indirect
	github.com/open-telemetry/opentelemetry-collector-contrib/exporter/googlecloudexporter v0.36.0
	github.com/open-telemetry/opentelemetry-collector-contrib/processor/filterprocessor v0.36.0 // indirect
	github.com/open-telemetry/opentelemetry-collector-contrib/processor/metricstransformprocessor v0.36.0
	github.com/open-telemetry/opentelemetry-collector-contrib/processor/resourcedetectionprocessor v0.36.0
	github.com/open-telemetry/opentelemetry-collector-contrib/processor/resourceprocessor v0.36.0 // indirect
	github.com/open-telemetry/opentelemetry-collector-contrib/receiver/hostmetricsreceiver v0.36.0 // indirect
	github.com/open-telemetry/opentelemetry-collector-contrib/receiver/jmxreceiver v0.36.0
	github.com/open-telemetry/opentelemetry-collector-contrib/receiver/nginxreceiver v0.36.0
	github.com/open-telemetry/opentelemetry-collector-contrib/receiver/prometheusexecreceiver v0.36.0
	github.com/open-telemetry/opentelemetry-collector-contrib/receiver/prometheusreceiver v0.36.0 // indirect
	github.com/open-telemetry/opentelemetry-collector-contrib/receiver/windowsperfcountersreceiver v0.36.0
	github.com/shirou/gopsutil v3.21.8+incompatible
	github.com/stretchr/testify v1.7.0
	github.com/tklauser/go-sysconf v0.3.6 // indirect
	go.opentelemetry.io/collector v0.36.0
	go.opentelemetry.io/collector/model v0.36.0
	go.uber.org/zap v1.19.1
	golang.org/x/crypto v0.0.0-20210421170649-83a5a9bb288b // indirect
	golang.org/x/sys v0.0.0-20210908233432-aa78b53d3365
)
