# Modify Scope Processor

Supported pipeline types: metrics

This is a processor to override the `InstrumentationScope` field in OTLP data.

## Configuration

The empty configuration does not change the `InstrumentationScope`. If
`override_scope_name` and/or `override_scope_version` are specified, they
override the existing value of the corresponding field in any signals passing
through the processor.

```yaml
modifyscope:
  override_scope_name: new_name
  override_scope_version: new_version
```
