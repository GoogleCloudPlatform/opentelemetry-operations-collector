module github.com/GoogleCloudPlatform/opentelemetry-operations-collector

go 1.14

require (
	github.com/hashicorp/go-uuid v1.0.2 // indirect
	github.com/mitchellh/go-testing-interface v1.0.3 // indirect
	github.com/open-telemetry/opentelemetry-collector-contrib/exporter/googlecloudexporter v0.38.1-0.20211112150048-46aeefc84532
	github.com/open-telemetry/opentelemetry-collector-contrib/processor/filterprocessor v0.38.1-0.20211112150048-46aeefc84532
	github.com/open-telemetry/opentelemetry-collector-contrib/processor/metricstransformprocessor v0.38.1-0.20211112150048-46aeefc84532
	github.com/open-telemetry/opentelemetry-collector-contrib/processor/resourcedetectionprocessor v0.38.1-0.20211112150048-46aeefc84532
	github.com/open-telemetry/opentelemetry-collector-contrib/processor/resourceprocessor v0.38.1-0.20211112150048-46aeefc84532
	github.com/open-telemetry/opentelemetry-collector-contrib/receiver/hostmetricsreceiver v0.38.1-0.20211112150048-46aeefc84532
	github.com/open-telemetry/opentelemetry-collector-contrib/receiver/httpdreceiver v0.38.0
	github.com/open-telemetry/opentelemetry-collector-contrib/receiver/jmxreceiver v0.38.1-0.20211112150048-46aeefc84532
	github.com/open-telemetry/opentelemetry-collector-contrib/receiver/mysqlreceiver v0.38.1-0.20211112150048-46aeefc84532
	github.com/open-telemetry/opentelemetry-collector-contrib/receiver/nginxreceiver v0.38.1-0.20211112150048-46aeefc84532
	github.com/open-telemetry/opentelemetry-collector-contrib/receiver/prometheusexecreceiver v0.38.1-0.20211112150048-46aeefc84532
	github.com/open-telemetry/opentelemetry-collector-contrib/receiver/prometheusreceiver v0.38.1-0.20211112150048-46aeefc84532
	github.com/open-telemetry/opentelemetry-collector-contrib/receiver/redisreceiver v0.38.1-0.20211112150048-46aeefc84532
	github.com/open-telemetry/opentelemetry-collector-contrib/receiver/windowsperfcountersreceiver v0.38.1-0.20211112150048-46aeefc84532
	github.com/shirou/gopsutil v3.21.10+incompatible
	github.com/stretchr/testify v1.7.0
	github.com/tidwall/wal v0.1.6 // indirect
	go.opentelemetry.io/collector v0.38.1-0.20211103215828-cffbecb2ac9e
	go.opentelemetry.io/collector/model v0.38.1-0.20211103215828-cffbecb2ac9e
	go.uber.org/multierr v1.7.0
	go.uber.org/zap v1.19.1
	golang.org/x/sys v0.0.0-20211025201205-69cdffdb9359
)
