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

## Development Guides

[Build and Test](./docs/dev/build-and-test.md)

[Upgrade OpenTelemetry Version](./docs/dev/upgrade-opentelemetry.md)

[Using Go Build Tags](./docs/dev/using-go-build-tags.md)

[ops-agent]: https://github.com/GoogleCloudPlatform/ops-agent