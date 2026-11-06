# In-process telemetry factory

`inprocesstelemetry.NewFactory()` is a Collector
[`telemetry.Factory`](https://pkg.go.dev/go.opentelemetry.io/collector/service/telemetry#Factory)
for distributions that want to consume the Collector's own internal metrics
in-process, without exposing any host endpoint (port, socket, named pipe or
file).

It is selected at build time via the `telemetry:` stanza of an OCB manifest
(or `components.telemetry` in a distrogen spec), and is consumed at runtime by
the [`selftelemetry` receiver](../../receiver/selftelemetryreceiver/README.md).

## Behaviour

* **Same configuration as upstream.** `service::telemetry` has exactly the
  schema of upstream
  [`otelconftelemetry`](https://github.com/open-telemetry/opentelemetry-collector/tree/main/service/telemetry/otelconftelemetry).
  Logs, traces, resource and default config are delegated to upstream
  unchanged.
* **One extra metric reader.** The internal-telemetry `MeterProvider` is
  built the same way upstream builds it, plus an
  `sdkmetric.ManualReader`. That reader is exposed via `Reader()`; it is only
  collected when a consumer (the `selftelemetry` receiver) asks for it.
* **Relaxed validation.** Upstream requires at least one metric reader when
  `service::telemetry::metrics::level` is not `none`. Because the in-process
  reader always exists, `service::telemetry::metrics::readers: []` is valid,
  i.e. the default `localhost:8888` Prometheus endpoint can be removed
  entirely.

## Turning it off

The in-process reader is controlled by the `telemetry.inProcessReader`
feature gate (beta, enabled by default). To get exactly the upstream
behaviour, start the Collector with:

```sh
otelcol-google --feature-gates=-telemetry.inProcessReader --config ...
```

With the gate disabled, the factory delegates meter provider creation to
upstream `otelconftelemetry` unchanged: no in-process reader is created,
`Reader()` returns nil, and upstream validation applies again (at least one
metric reader is required when metrics are enabled). Combine this with the
previous self-telemetry configuration (e.g. a `periodic` OTLP reader or a
`pull` Prometheus reader) to self-observe exactly as before.

## API

```go
// Build time (generated components.go):
factories.Telemetry = inprocesstelemetry.NewFactory()

// Run time (any in-process consumer):
if r := inprocesstelemetry.Reader(); r != nil {
	var rm metricdata.ResourceMetrics
	_ = r.Collect(ctx, &rm)
}
```

Look the reader up on every collection rather than caching it: the Collector
re-creates its telemetry providers whenever the service is rebuilt (e.g. on
config reload). `Reader()` returns nil when internal metrics are disabled.

## Keeping in sync with upstream

`factory.go` contains a small fork of unexported upstream code
(`createMeterProvider`, `createFixedResourceConfig`, `noopMeterProvider`, and
`Config.Validate`) from `service/telemetry/otelconftelemetry`. When bumping the
Collector version, diff `metrics.go`, `resource.go` and `config.go` in that
package between the old and new tags and port any changes.
