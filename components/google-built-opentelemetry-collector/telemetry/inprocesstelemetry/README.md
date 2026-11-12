# In-process telemetry factory

`inprocesstelemetry.NewFactory()` is a Collector
[`telemetry.Factory`](https://pkg.go.dev/go.opentelemetry.io/collector/service/telemetry#Factory)
for distributions that want to consume the Collector's own internal metrics
and logs in-process, without exposing any host endpoint (port, socket, named pipe or
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
* **One extra metric reader (opt-in).** When the `telemetry.inProcessReader`
  feature gate is enabled, the internal-telemetry `MeterProvider` is built the
  same way upstream builds it, plus an
  `sdkmetric.ManualReader`. That reader is exposed via `Reader()`; it is only
  collected when a consumer (the `selftelemetry` receiver) asks for it.
* **One extra log destination (opt-in).** When the `telemetry.inProcessReader`
  feature gate is enabled, the logger built by upstream is wrapped with one more
  zap core that copies every entry the logger accepts (after level filtering and
  sampling) to an OpenTelemetry `LoggerProvider` whose only processor is an
  in-memory `LogBuffer`, exposed via `Logs()`. Upstream's logger construction
  is not forked: stderr output and any configured
  `service::telemetry::logs::processors` are unchanged. The buffer is bounded
  (8192 records by default) and drops the oldest record when full.
* **Relaxed validation (opt-in).** With the feature gate enabled: upstream requires at least one metric reader when
  `service::telemetry::metrics::level` is not `none`. Because the in-process
  reader always exists, `service::telemetry::metrics::readers: []` is valid,
  i.e. the default `localhost:8888` Prometheus endpoint can be removed
  entirely.

## Opting in

The in-process reader is controlled by the `telemetry.inProcessReader`
feature gate, which is **alpha, i.e. disabled by default**. A Collector built
with this factory therefore behaves exactly like one built with upstream
`otelconftelemetry` unless the user explicitly opts in:

```sh
otelcol-google --feature-gates=+telemetry.inProcessReader --config ...
```

With the gate disabled (the default), the factory delegates meter provider
creation and config validation to upstream unchanged: no in-process reader is
created, `Reader()` returns nil, and upstream validation applies (at least one
metric reader is required when metrics are enabled).

With the gate enabled, the in-process reader is attached and
`service::telemetry::metrics::readers: []` becomes a valid configuration.

## API

```go
// Build time (generated components.go):
factories.Telemetry = inprocesstelemetry.NewFactory()

// Run time (any in-process consumer):
if r := inprocesstelemetry.Reader(); r != nil {
	var rm metricdata.ResourceMetrics
	_ = r.Collect(ctx, &rm)
}
if b := inprocesstelemetry.Logs(); b != nil {
	records, dropped := b.Drain()
	_, _ = records, dropped
}
```

Look the reader and the log buffer up on every collection rather than caching them: the Collector
re-creates its telemetry providers whenever the service is rebuilt (e.g. on
config reload). `Reader()` returns nil when internal metrics are disabled.

## Keeping in sync with upstream

`factory.go` contains a small fork of unexported upstream code
(`createMeterProvider`, `createFixedResourceConfig`, `noopMeterProvider`, and
`Config.Validate`) from `service/telemetry/otelconftelemetry`. `logger_tee.go`
recognizes the field upstream uses to carry component attributes
(`componentattribute.scopeAttributesField`, an unexported type in an internal
package) by its type name; if upstream renames it, the selftelemetry
receiver's `TestEndToEndLogsNoHostEndpoint` fails. When bumping the
Collector version, diff `metrics.go`, `resource.go` and `config.go` in that
package between the old and new tags and port any changes.
