# Self-telemetry receiver (in-process)

| Status    |                                         |
| --------- | --------------------------------------- |
| Stability | development: metrics, logs              |
| Distros   | google-built-opentelemetry-collector    |

The `selftelemetry` receiver emits the Collector's own internal metrics
(`otelcol_*`) into a regular metrics pipeline, and the Collector's own internal
logs into a regular logs pipeline, **without exposing any host endpoint**. There is no TCP port, Unix socket, named pipe or file involved:
the metrics are handed over in memory inside the Collector process.

This is an alternative to the common pattern of having the Collector expose
its internal metrics on a Prometheus endpoint (e.g. `localhost:8888`) and
scraping that endpoint with a `prometheus` receiver.

## How it works

The receiver requires the Collector binary to be built with the
[`inprocesstelemetry`](../../telemetry/inprocesstelemetry/README.md) telemetry
factory, **and** the Collector to be started with the (alpha, disabled by
default) `telemetry.inProcessReader` feature gate enabled:
`--feature-gates=+telemetry.inProcessReader`. That factory behaves exactly like upstream `otelconftelemetry` (all
`service::telemetry` config is unchanged), but additionally attaches an
in-memory `sdkmetric.ManualReader` to the internal-telemetry `MeterProvider`
and exposes it via `inprocesstelemetry.Reader()`.

On every `collection_interval` the receiver collects from that reader and
converts the result to `pmetric.Metrics`. The conversion reuses the OTel Go
SDK's OTLP/HTTP exporter with an in-memory `http.RoundTripper`, so no request
ever leaves the process.

If the binary was not built with `inprocesstelemetry`, if the
`telemetry.inProcessReader` feature gate is not enabled, or if
`service::telemetry::metrics::level` is `none`, the receiver logs a warning
once and emits nothing.

## Configuration

| Field                 | Default | Description                                                     |
| --------------------- | ------- | --------------------------------------------------------------- |
| `collection_interval` | `1m`    | How often internal metrics are read and delivered               |
| `initial_delay`       | `1s`    | Delay before the first collection                               |
| `timeout`             | `30s`   | Bound on each collection, including delivery downstream (0 = none) |

On shutdown the receiver performs one final collection and delivers it before
the downstream processors and exporters are shut down, so values recorded
since the last periodic collection are not lost. Only values recorded while
downstream components drain (after this receiver has stopped) cannot be
delivered in-process.

```yaml
receivers:
  selftelemetry:
    collection_interval: 60s

exporters:
  googlecloud:

service:
  telemetry:
    metrics:
      # Optional: drop the Prometheus pull endpoint entirely.
      readers: []
  pipelines:
    metrics/self:
      receivers: [selftelemetry]
      exporters: [googlecloud]
```

## Differences from scraping the Prometheus endpoint

* The resource is the Collector's telemetry resource (`service.name`,
  `service.instance.id`, `service.version`, ...) rather than the
  scrape-target-derived resource produced by the `prometheus` receiver.
* Metric names are the original instrument names, histograms keep their
  native OTLP form, and there are no `scrape_*` / `up` metrics and no
  `promhttp_*` handler metrics.
* Instrumentation scope name/version is preserved.

## Internal logs

Used in a `logs` pipeline, the receiver delivers the Collector's own log
records (the same entries written to stderr, after level filtering and
sampling) in-process. This replaces the pattern of exporting internal logs
with a `service::telemetry::logs::processors` OTLP exporter pointed at a local
`otlp` receiver.

With the `telemetry.inProcessReader` feature gate enabled, the
`inprocesstelemetry` factory tees the Collector's logger into an in-memory
buffer. On every `collection_interval` the receiver drains that buffer and
converts the records to `plog.Logs`, again through the OTel Go SDK's own OTLP
exporter with an in-memory transport. Per-component attributes
(`otelcol.component.id`, `otelcol.component.kind`, `otelcol.signal`, ...) are
instrumentation scope attributes, and the resource is the Collector's telemetry
resource, the same as upstream's OTLP log export produces.

The buffer holds up to 8192 records between two collections. If it fills up,
the oldest records are dropped and the receiver logs a warning with the number
of dropped records. On shutdown, the receiver drains the buffer one last time.

```yaml
# otelcol-google --feature-gates=+telemetry.inProcessReader --config ...
receivers:
  selftelemetry:
service:
  telemetry:
    logs:
      processors: []   # no OTLP log export to a local port
    metrics:
      readers: []      # no Prometheus pull endpoint or OTLP push
  pipelines:
    metrics/self:
      receivers: [selftelemetry]
      exporters: [googlecloud]
    logs/self:
      receivers: [selftelemetry]
      exporters: [googlecloud]
```

Only one `logs` pipeline should use the receiver, because each collection
drains the buffer. To send internal logs to several destinations, fan out from
that one pipeline.
