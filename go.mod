module github.com/GoogleCloudPlatform/opentelemetry-operations-collector

go 1.14

require (
	github.com/hashicorp/go-uuid v1.0.2 // indirect
	github.com/mitchellh/go-testing-interface v1.0.3 // indirect
	github.com/open-telemetry/opentelemetry-collector-contrib/exporter/googlecloudexporter v0.39.0
	github.com/open-telemetry/opentelemetry-collector-contrib/processor/filterprocessor v0.39.0
	github.com/open-telemetry/opentelemetry-collector-contrib/processor/metricstransformprocessor v0.39.0
	github.com/open-telemetry/opentelemetry-collector-contrib/processor/resourcedetectionprocessor v0.39.0
	github.com/open-telemetry/opentelemetry-collector-contrib/processor/resourceprocessor v0.39.0
	github.com/open-telemetry/opentelemetry-collector-contrib/receiver/hostmetricsreceiver v0.39.0
	github.com/open-telemetry/opentelemetry-collector-contrib/receiver/httpdreceiver v0.38.0
	github.com/open-telemetry/opentelemetry-collector-contrib/receiver/jmxreceiver v0.39.0
	github.com/open-telemetry/opentelemetry-collector-contrib/receiver/mysqlreceiver v0.39.0
	github.com/open-telemetry/opentelemetry-collector-contrib/receiver/nginxreceiver v0.39.0
	github.com/open-telemetry/opentelemetry-collector-contrib/receiver/prometheusexecreceiver v0.39.0
	github.com/open-telemetry/opentelemetry-collector-contrib/receiver/prometheusreceiver v0.39.0
	github.com/open-telemetry/opentelemetry-collector-contrib/receiver/redisreceiver v0.39.0
	github.com/open-telemetry/opentelemetry-collector-contrib/receiver/windowsperfcountersreceiver v0.39.0
	github.com/shirou/gopsutil v3.21.10+incompatible
	github.com/stretchr/testify v1.7.0
	go.opentelemetry.io/collector v0.39.0
	go.opentelemetry.io/collector/model v0.39.0
	go.uber.org/multierr v1.7.0
	go.uber.org/zap v1.19.1
	golang.org/x/sys v0.0.0-20211025201205-69cdffdb9359
)
