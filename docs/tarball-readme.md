# OpenTelemetry Collector for Google Cloud Monitoring Tarball

Thank you for using this community build of the OpenTelemetry Collector for Google Cloud Monitoring. This packaged tarball contains tools to setup the Collector with some default host metrics for your Google Cloud Monitoring: memory, cpu, disk, filesystem, network, swap, process, load metrics. More details of these host metrics can be viewed [here](https://cloud.google.com/monitoring/api/metrics_agent). There metrics are scraped by the [host metric receiver](https://github.com/open-telemetry/opentelemetry-collector/tree/master/receiver/hostmetricsreceiver) in the collector.

## Getting Started
1. Run `./google-cloudops-opentelemetry-collector_linux_amd64 --config config.yaml` to run the collector with the default configuration file
2. Search for metrics in the Google Cloud Monitoring project, there should already be values populated!

## Custom Metrics

Modify the configuration file `config.yaml` to experiement with different metrics