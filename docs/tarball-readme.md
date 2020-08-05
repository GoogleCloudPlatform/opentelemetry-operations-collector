# OpenTelemetry Collector for Google Cloud Monitoring Tarball

Thank you for using this community build of the OpenTelemetry Collector for Google Cloud Monitoring. This tarball package contains the Collector configured to scrape system metrics from the host agent and report these to [Cloud Monitoring](https://cloud.google.com/monitoring/api/metrics_agent).

## Getting Started
1. Run `./google-cloudops-opentelemetry-collector_linux_amd64 --config config.yaml` to run the collector with the default configuration file
2. Search for metrics in the Google Cloud Monitoring project, there should already be values populated!

## Custom Metrics

Modify the configuration file `config.yaml` to experiement with different metrics
