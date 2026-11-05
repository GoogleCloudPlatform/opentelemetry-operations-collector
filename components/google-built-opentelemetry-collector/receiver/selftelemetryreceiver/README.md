# Self-telemetry receiver (in-process)

| Status    |                                         |
| --------- | --------------------------------------- |
| Stability | development: metrics                    |
| Distros   | google-built-opentelemetry-collector    |

The `selftelemetry` receiver emits the Collector's own internal metrics
(`otelcol_*`) into a regular metrics pipeline **without exposing any host
endpoint**. There is no TCP port, Unix socket, named pipe or file involved:
the metrics are handed over in memory inside the Collector process.

This is an alternative to the common pattern of having the Collector expose
its internal metrics on a Prometheus endpoint (e.g. `localhost:8888`) and
scraping that endpoint with a `prometheus` receiver.

## How it works

The receiver requires the Collector binary to be built with the
[`inprocesstelemetry`](../../telemetry/inprocesstelemetry/README.md) telemetry
factory. That factory behaves exactly like upstream `otelconftelemetry` (all
`service::telemetry` config is unchanged), but additionally attaches an
in-memory `sdkmetric.ManualReader` to the internal-telemetry `MeterProvider`
and exposes it via `inprocesstelemetry.Reader()`.

On every `collection_interval` the receiver collects from that reader and
converts the result to `pmetric.Metrics`. The conversion reuses the OTel Go
SDK's OTLP/HTTP exporter with an in-memory `http.RoundTripper`, so no request
ever leaves the process.

If the binary was not built with `inprocesstelemetry`, or if
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
