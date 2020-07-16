module github.com/GoogleCloudPlatform/opentelemetry-operations-collector

go 1.14

require (
	github.com/StackExchange/wmi v0.0.0-20181212234831-e0a55b97c705 // indirect
	github.com/go-ole/go-ole v1.2.4 // indirect
	github.com/hashicorp/consul/api v1.4.0 // indirect
	github.com/hashicorp/serf v0.9.2 // indirect
	github.com/mitchellh/go-testing-interface v1.0.3 // indirect
	github.com/open-telemetry/opentelemetry-collector-contrib/exporter/stackdriverexporter v0.5.0
	github.com/open-telemetry/opentelemetry-collector-contrib/processor/metricstransformprocessor v0.6.1-0.20200723171718-a2ff1aa6779e
	github.com/philhofer/fwd v1.0.0 // indirect
	github.com/pkg/errors v0.9.1
	github.com/tinylib/msgp v1.1.2 // indirect
	go.opencensus.io v0.22.4 // indirect
	go.opentelemetry.io/collector v0.6.0
	go.uber.org/zap v1.15.0 // indirect
	golang.org/x/sys v0.0.0-20200610111108-226ff32320da
	google.golang.org/api v0.29.0 // indirect
	k8s.io/client-go v8.0.0+incompatible // indirect
)

replace k8s.io/client-go => k8s.io/client-go v0.0.0-20190620085101-78d2af792bab
