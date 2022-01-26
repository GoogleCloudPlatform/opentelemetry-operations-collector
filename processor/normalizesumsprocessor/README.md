# Normalize Sums Processor

Supported pipeline types: metrics

This is a processor for supporting normalization from sum metrics
where an appropriate start time is not available to a sum metric using
the first data point as the start. The first metric of this type will not
be emitted, and future metrics will be transformed into a sum metric
normalized to treat the first data point (or a subsequent data point
where a reset occurred) as the start.

Additionally, any time a metric value decreases, it will be assumed to
have rolled over, at which point another data point will be skipped in
favor of providing only accurate data representing the sum from a known point

## Configuration

By default, all sum metrics with a 0 (unset) start timestamp will be normalized.

Optionally, one can specify a set of gauge metric names to also normalize.
The resulting metrics will be sum metrics.

```yaml
normalizesums:
  include_gauges: ["a", "b", "c"]
```
