# Google Cloud Ops OpenTelemetry Collector

<p>
  <strong>
    <a href="docs/contributing.md">Contributing</a>
    &nbsp;&nbsp;&bull;&nbsp;&nbsp;
    <a href="docs/code-of-conduct.md">Code of Conduct</a>
  </strong>
</p>


### :exclamation: This product is currently only supported by Google as a component of the [Ops Agent][ops-agent]. The collector is usable on its own, however that method of usage is unsupported. If you have issues with this collector while using it through the Ops Agent, please go through Ops Agent support channels.

The Google Cloud Ops OpenTelemetry Collector is a distribution of the collector tooled specifically for exporting telemetry data to Google Cloud. It facilitates the monitoring portion of the [Google Cloud Ops Agent][ops-agent], and includes some custom receivers and processors built to support Ops Agent features.

## Build and Test

All commands documented here will reference the `make` targets. If you don't have `make` installed, most of the targets are simple enough to copy and run manually.

All builds require Go at version 1.18 or greater.

### Base Collector

To build the base collector with no optional features, run the following command:
```
make build
```
To run base collector tests:
```
make test
```

### GPU Support

Additional requirements:
* CGO support (having C build tools in your path should do it)

To build the collector with GPU receiver support added, you can use the build tag `gpu`:
```
GO_TAGS=gpu make build
```
Building with GPU support on platforms other than `linux` or without CGO enabled will fail.

To run tests with GPU support:
```
GO_TAGS=gpu make test
```

### JMX Receiver Support

Additional requirements:
* A valid JMX Jar and its sha256 hash

To build the collector with JMX receiver support, you can provide the environment variable JMX_JAR_SHA:
```
JMX_JAR_SHA=<sha256 of JMX jar> make build
```
Testing with a JMX Jar SHA currently does not affect tests.

[ops-agent]: https://github.com/GoogleCloudPlatform/ops-agent