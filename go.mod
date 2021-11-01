module github.com/GoogleCloudPlatform/opentelemetry-operations-collector

go 1.14

require (
	github.com/hashicorp/go-uuid v1.0.2 // indirect
	github.com/mitchellh/go-testing-interface v1.0.3 // indirect
	github.com/open-telemetry/opentelemetry-collector-contrib/exporter/googlecloudexporter v0.37.1-0.20211028205244-e6fab4102b84
	github.com/open-telemetry/opentelemetry-collector-contrib/processor/filterprocessor v0.37.1-0.20211028205244-e6fab4102b84
	github.com/open-telemetry/opentelemetry-collector-contrib/processor/metricstransformprocessor v0.37.1-0.20211028205244-e6fab4102b84
	github.com/open-telemetry/opentelemetry-collector-contrib/processor/resourcedetectionprocessor v0.37.1-0.20211028205244-e6fab4102b84
	github.com/open-telemetry/opentelemetry-collector-contrib/processor/resourceprocessor v0.37.1-0.20211028205244-e6fab4102b84
	github.com/open-telemetry/opentelemetry-collector-contrib/receiver/hostmetricsreceiver v0.37.1-0.20211028205244-e6fab4102b84
	github.com/open-telemetry/opentelemetry-collector-contrib/receiver/httpdreceiver v0.37.1-0.20211028205244-e6fab4102b84
	github.com/open-telemetry/opentelemetry-collector-contrib/receiver/jmxreceiver v0.37.1-0.20211028205244-e6fab4102b84
	github.com/open-telemetry/opentelemetry-collector-contrib/receiver/mysqlreceiver v0.37.1-0.20211028205244-e6fab4102b84
	github.com/open-telemetry/opentelemetry-collector-contrib/receiver/nginxreceiver v0.37.1-0.20211028205244-e6fab4102b84
	github.com/open-telemetry/opentelemetry-collector-contrib/receiver/prometheusexecreceiver v0.37.1-0.20211028205244-e6fab4102b84
	github.com/open-telemetry/opentelemetry-collector-contrib/receiver/prometheusreceiver v0.37.1
	github.com/open-telemetry/opentelemetry-collector-contrib/receiver/redisreceiver v0.37.1-0.20211028205244-e6fab4102b84
	github.com/open-telemetry/opentelemetry-collector-contrib/receiver/windowsperfcountersreceiver v0.37.1-0.20211028205244-e6fab4102b84
	github.com/shirou/gopsutil v3.21.9+incompatible
	github.com/stretchr/testify v1.7.0
	github.com/tidwall/wal v0.1.6 // indirect
	go.opentelemetry.io/collector v0.37.1-0.20211026180946-46c8e2290e45
	go.opentelemetry.io/collector/model v0.37.1-0.20211026180946-46c8e2290e45
	go.uber.org/multierr v1.7.0
	go.uber.org/zap v1.19.1
	golang.org/x/sys v0.0.0-20211007075335-d3039528d8ac
)
