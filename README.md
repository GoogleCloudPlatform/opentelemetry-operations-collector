# OpenTelemetry Operations Collector Agent

<p>
  <strong>
    <a href="docs/contributing.md">Contributing<a/>
    &nbsp;&nbsp;&bull;&nbsp;&nbsp;
    <a href="docs/code-of-conduct.md">Code of Conduct<a/>
  </strong>
</p>


### :exclamation: This product is currently in ALPHA and not officially supported by Google.

This repository hosts packaging and configuration code for generating builds of the OpenTelemetry Collector that can be used to collect system & application metrics from GCE or EC2 virtual machines and send these to Google Cloud Monitoring.

## Running the Agent

### Linux

#### :warning: This product is not officially supported on Linux.

You can experiment with custom builds, but for the official Linux agent, see https://cloud.google.com/monitoring/agent.

### Windows

#### To install the agent via MSI:

1. Download the latest MSI package from the [Releases](https://github.com/GoogleCloudPlatform/opentelemetry-operations-collector/releases) page.
2. Copy the MSI package to your Virtual Machine.
3. Double click the MSI or run the following command in an administrative Powershell console:
```ps
msiexec /i google-cloudops-opentelemetry-collector.msi /qn
```
4. This will install the agent as a Windows Service and start running immediately.

Within a couple of minutes you should see agent metrics appearing in Cloud Monitoring. The monitoring agent status should change to ":white_check_mark:&nbsp;&nbsp;Latest" in the VM Instances dashboard: https://console.cloud.google.com/monitoring/dashboards/resourceList/gce_instance.

#### To uninstall the agent via MSI:

1. Right click the MSI and select uninstall or run the following command in an administrative Powershell console:
```ps
msiexec /x google-cloudops-opentelemetry-collector.msi /qn
```
2. Alternatively, you can uninstall the agent from the **Programs & Features** page in the **Control Panel**. The agent will appear as "Google Cloud Operations OpenTelemetry Collector".
3. This will uninstall the agent and remove the windows service.

#### Troubleshooting:

- If the MSI fails to install, you can generate installation logs for debugging purposes by adding the flag `/l* msi.log` to the [msiexec command](https://docs.microsoft.com/en-us/windows/win32/msi/command-line-options).

- Application logs can be used to debug why the service failed to install or start, as well to debug general issues. The agent logs will appear in the Event Viewer under the source "google-cloudops-opentelemetry-collector".

- You can view metrics related to the health of the agent itself in Cloud Monitoring under the `agent` prefix as documented [here](https://cloud.google.com/monitoring/api/metrics_agent#agent-agent).

- The agent reports additional Prometheus style self observability metrics that can be accessed locally via the endpoint http://0.0.0.0:8888/metrics as documented [here](https://github.com/open-telemetry/opentelemetry--llector/blob/master/docs/observability.md).

- The agent exposes additional debug information locally via the endpoint http://0.0.0.0:55679/debug/tracez. This debug information can be used to debug errors related to collecting metrics or sending them to cloud monitoring. Find our more about zpages [here](https://github.com/open-telemetry/opentelemetry-specification/blob/master/experimental/trace/zpages.md).

- If you encounter an issue related to running the agent or using it with Cloud Monitoring, please create a GitHub issue in this repository and include relevant debug information. If you encounter an issue or have a feature request related to the core OpenTelemetry Collector application, consider creating an issue [here](https://github.com/open-telemetry/opentelemetry-collector/issues) instead.

## Configuration

Configuration is stored in a `yaml` file located in the installation directory. The default configuration file for system metrics includes two pipelines for collecting system metrics and agent self-observability metrics. To understand OpenTelemetry Collector pipelines in more detail, see the [OpenTelemetry Collector design document](https://github.com/open-telemetry/opentelemetry-collector/blob/master/docs/design.md).

Pipelines:

1. Ingest system metrics:

    - `hostmetrics` receiver: Scrapes system metrics.
    - `agentmetrics` processor: Performs some custom processing of these metrics to make them compatible with Cloud Monitoring.
    - `filter` processor: Filters out several metrics that are not supported by Cloud Monitoring.
    - `metricstransform` processor: Converts the metrics from OpenTelemetry format, as defined in the [semantic conventions](https://github.com/open-telemetry/opentelemetry-specification/tree/master/specification/metrics/semantic_conventions), to Cloud Monitoring format, as defined in the [agent metrics](https://cloud.google.com/monitoring/api/metrics_agent) documentation.
    - `resourcedetection` processor: Appends resource information detected from the host.
    - `stackdriver` exporter: Exports the metrics to Cloud Monitoring using the `agent.googleapis.com` prefix.

2. Ingest agent self-observability metrics:
    - `prometheus` receiver: The OpenTelemetry Collector exports self-observability metrics in Prometheus format on port 8888 by default. This receiver scrapes those metrics at the configured frequency.
    - `filter` processor: Retains only the “agent” metrics that are supported by Cloud Monitoring.
    - `metricstransform` processor: Converts the internal self-observability metrics to Cloud Monitoring format, as defined in [agent metrics](https://cloud.google.com/monitoring/api/metrics_agent).
    - `resourcedetection` processor: Appends resource information detected from the host using the resourcedetection processor.
    - `stackdriver` exporter: Exports the metrics to Cloud Monitoring using the `agent.googleapis.com` prefix.
 
Common configuration that you may want to change:

- Under the `hostmetrics` receiver you can configure which kinds of metrics to scrape, and can also filter devices. For more details, see the [Host Metrics receiver](https://github.com/open-telemetry/opentelemetry-collector/tree/master/receiver/hostmetricsreceiver).
    - You can configure the `collection_interval` to the granularity at which you would like to view metric data in Cloud Monitoring. The minimum interval supported by Cloud Monitoring is 10s.
- Under the `filter/system` processor you can configure which host metrics to include or exclude. For more details, see the [Filter processor](https://github.com/open-telemetry/opentelemetry-collector/tree/master/processor/filterprocessor).
- Metrics ingested by Cloud Monitoring under the `agent.googleapis.com` prefix must conform to the format specified in Agent Metrics. If you would like to generate any metrics in a non-standard format, you should submit Custom Metrics using the `custom.googleapis.com` prefix instead. To do this, specifying the full metric name, including this prefix, in the `metricstransform` processor rename configuration. If the domain is specified in the metric name, the `agent.googleapis.com` prefix will not be added to the metric when exported.

## Build / Package from source

### Linux

To generate a tarball archive that includes the OpenTelemetry binary and a configuration file compatible with Google Cloud Monitoring:

1. Run `make build-tarball`.
    - To also include JVM, Apache, MySQL and StatsD support, run `make build-tarball-exporters` instead.
2. The tarball file will be generated in the `dist` folder.

### Windows

To generate an MSI that will install OpenTelemetry as a Windows service using a configuration file compatible with Google Cloud Monitoring:

1. Run `.build\msi\make.ps1 Install-Tools` to install the open source [WIX Toolset](https://wixtoolset.org).
2. Run `.build\msi\make.ps1 New-MSI`.
3. The MSI file will be generated in the `dist` folder.

Alternatively, you can generate a [googet](https://github.com/google/googet) package by running `make build-googet`. This is the packaging method used to install the Collector on Windows GCE VMs.

### Other Operating Systems

The Collector Agent is compatible with, but has not been tested on, other operating systems. You can experiment with custom builds for other systems if desired.

### Running build commands in Docker

You can also run the build commands inside docker:

1. Run `make docker-build-image` to build the docker image. This will generate an image called `otelopscol-build`.
2. Run `make TARGET=build-<package> docker-run`.
3. The specified package will be generated in the `dist` folder.
