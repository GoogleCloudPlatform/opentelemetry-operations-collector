# Cast To Sum Processor

Supported pipeline types: metrics

## Description

The cast to sum processor (`casttosumprocessor`) converts (primarily gauge)
metrics to cumulative sum metrics.

Each value remains unchanged, but is transformed to a cumulative sum.

## Configuration

Configuration is specified through a list of metrics. The processor uses metric
names to identify a set of input metrics and casts each of them to a cumulative
monotonic sum.

```yaml
casttosum:
  # list input metrics to convert to sums. This is a required field.
  metrics:
    - <metric_1_name>
    - <metric_2_name>
    .
    .
    - <metric_n_name>
```
