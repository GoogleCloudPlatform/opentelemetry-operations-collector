---
name: event
description: Instructions and semantic convention guidelines for working with the pkg/event package and OpenTelemetry Weaver code generation. Use this skill when adding or modifying events, updating pkg/event/schema/events.yaml, or modifying the Jinja2 templates in pkg/event/templates.
---

# `pkg/event` — OpenTelemetry Event Generation with Weaver

This skill documents the design, semantic convention rules, and developer workflow for the `pkg/event` package (`github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/event`).

## Overview

The `pkg/event` package provides type-safe Go functions for recording structured OpenTelemetry Events using `go.uber.org/zap`.

Rather than hand-writing logging helpers or scattering ad-hoc attribute keys across the codebase, all events and their attributes are formally defined in an **OpenTelemetry Weaver v2 schema** (`pkg/event/schema/events.yaml`). OpenTelemetry Weaver (`otel/weaver` Docker image) validates the schema and renders Jinja2 templates (`pkg/event/templates/go/`) into `pkg/event/generated_events.go`.

---

## Package Structure

```text
pkg/event/
├── doc.go                        # Package documentation and //go:generate directive
├── Makefile                      # Local convenience targets delegating to root Makefile
├── go.mod / go.sum               # Standalone Go module (included in root go.work)
├── generated_events.go           # Generated Go code (DO NOT EDIT MANUALLY)
├── events_test.go                # Unit tests using zaptest/observer and otelzap bridge
├── schema/
│   ├── manifest.yaml             # Weaver registry manifest (name, version, schema_url)
│   └── events.yaml               # Weaver v2 schema (file_format: definition/2)
└── templates/
    └── go/
        ├── weaver.yaml           # Weaver template config, filters, and Go/Zap type maps
        └── events.go.j2          # Jinja2 template generating Go types, options, and functions
```

---

## OpenTelemetry Semantic Convention Rules for Events

When designing or reviewing events in `pkg/event/schema/events.yaml`, strictly follow the [OpenTelemetry Semantic Conventions for Events](https://opentelemetry.io/docs/specs/semconv/general/events/):

### 1. NEVER Define a Message or Body Attribute
* **Wrong:** Adding an attribute like `component.message` or `policy.error.message`.
* **Why:** In OpenTelemetry, Events are specialized `LogRecord`s that already have a top-level `Body` field specifically intended for human-readable string display messages.
* **How it works in Go:**
  * Every generated `Record<Event>Event` function accepts either `body string` or `err error` (when `annotations.severity: error`) as its required second parameter (immediately after `logger *zap.Logger`), which populates `LogRecord.Body` via `logger.Log(level, body, fields...)`.
  * Document what `body` represents (e.g., the human-readable failure message) in the event's `note` field in `events.yaml`.
  * The event identity is always recorded in the `event.name` attribute (`zap.String("event.name", <Event>EventName)`).

### 2. Event Naming
* Name events after **meaningful domain occurrences** using dot-separated namespaces (e.g., `gcp.policy.evaluate.error`, `gcp.collector.config.reload`), not generic nouns or log lines. Custom conventions should be namespaced under `gcp.`.

### 3. Encoding Default Severity in Annotations
Events default to `zapcore.InfoLevel` unless specified otherwise via `annotations.severity` in `events.yaml`:
```yaml
events:
  - name: gcp.policy.evaluate.error
    annotations:
      severity: error # Supported values: debug, info, warn, error
```
This sets the generated default `zapcore.Level` in `Record<Event>Event`. Additionally, when `severity: error` is specified, the generated function accepts `err error` instead of `body string` and extracts `err.Error()` as the log message body.

### 4. Required Documentation in `note`
Every event definition **must** include a `note` field documenting:
1. **Trigger condition:** Under what circumstances the event is recorded.
2. **Body semantics:** What the human-readable `body` message represents.
3. **Timestamp semantics:** What the event's timestamp represents.
4. **Default severity:** The default log severity level (`INFO`, `WARN`, `ERROR`).

### 5. Structured Attributes & Requirement Levels
Define reusable attributes under the top-level `attributes:` section of `events.yaml` and reference them in `events:` via `ref`:
* **`requirement_level: required`**: Becomes a required positional parameter on the generated `Record<Event>Event(...)` function.
* **`requirement_level: recommended` / `opt_in` / `conditionally_required`**: Becomes a functional option `With<Event><Attr>(val <Type>)`.
* **Enums**: Attribute definitions with `type: members: [...]` automatically generate strongly-typed Go string enums (`type <AttrName> string`) and constants.

---

## How Generated Code Works

For an event named `gcp.policy.evaluate.error` with `annotations.severity: error` and required attributes `gcp.policy.id` (string) and `gcp.policy.set.revision.id` (string), Weaver generates:

1. **Schema URL and Event Name Constants:**
   ```go
   const SchemaURL = "https://googlecloudplatform.github.io/opentelemetry-operations-collector/schemas/0.1.0"
   const PolicyEvaluateErrorEventName = "gcp.policy.evaluate.error"
   ```
2. **Functional Options (`PolicyEvaluateErrorEventOption`):**
   * `WithPolicyEvaluateErrorEventContext(ctx context.Context)`: Attaches `zap.Any("context", ctx)` so the OpenTelemetry `otelzap` bridge correlates `TraceID` and `SpanID` on the emitted `log.Record`.
   * `WithPolicyEvaluateErrorEventLevel(level zapcore.Level)`: Overrides the default level (`zapcore.ErrorLevel`).
   * `WithPolicyEvaluateErrorEventFields(fields ...zap.Field)`: Appends custom `zap.Field`s.
3. **Recording Function:**
   ```go
   func RecordPolicyEvaluateErrorEvent(
       logger *zap.Logger,
       err error,
       policyID string,
       policySetRevisionID string,
       opts ...PolicyEvaluateErrorEventOption,
   )
   ```

---

## Step-by-Step Guide: Adding a New Event

### Step 1: Define Attributes and Event in `pkg/event/schema/events.yaml`

Ensure the file starts with `file_format: definition/2`. Add any new attributes under `attributes:` and your new event under `events:`:

```yaml
file_format: definition/2

attributes:
  - key: gcp.policy.id
    type: string
    stability: development
    brief: Unique identifier of the policy being evaluated.
    examples: ["policy-123"]

events:
  - name: gcp.policy.evaluate.error
    stability: development
    requirement_level: recommended
    brief: Recorded when an error occurs while evaluating a policy.
    note: |
      This event is recorded whenever policy evaluation fails.
      The event body MUST contain the human-readable failure message describing the evaluation error.
      Timestamp MUST be set to the time when the evaluation failure occurred.
      Default severity level is ERROR.
    annotations:
      severity: error
    attributes:
      - ref: gcp.policy.id
        requirement_level: required
```

### Step 2: Validate the Schema

Run the Weaver schema check from the repository root (uses Docker `otel/weaver:v0.26.1` with `--v2`):

```bash
make check-events
```

### Step 3: Regenerate Go Code

Generate `pkg/event/generated_events.go`:

```bash
make gen-events
```
*(Alternatively, run `go generate ./pkg/event/...` from the root or `make generate` inside `pkg/event/`.)*

### Step 4: Add Unit Tests

Add tests in `pkg/event/events_test.go` covering:
1. **`zaptest/observer`**: Verify the emitted log entry level, message (`Body`), `event.name`, and structured fields.
2. **`otelzap` Bridge**: Verify end-to-end translation into an OpenTelemetry `log.Record` (checking `record.Body()`, `record.Severity()`, trace correlation, and attributes).

Run the tests:

```bash
make test-events
```

### Step 5: Run Precommit Checks

Verify license headers, linters, tests, and zero diff drift:

```bash
make precommit
```

---

## Modifying the Jinja2 Generator Templates

If you need to customize how Go code is generated:

1. **Template Configuration (`pkg/event/templates/go/weaver.yaml`)**:
   * Uses `filter: '{schema_url: .schema_url, attributes: .registry.attributes, events: .registry.events}'` to pass the resolved v2 registry and schema URL to `events.go.j2`.
   * `text_maps.go_types` maps Weaver types (`string`, `int`, `double`, `boolean`) to Go types.
   * `text_maps.zap_field_constructors` maps Weaver types to `zap` field constructors (`zap.String`, `zap.Int64`, etc.).
   * `text_maps.zap_levels` maps `annotations.severity` (`debug`, `info`, `warn`, `error`) to `zapcore.Level` constants.
2. **Jinja Template (`pkg/event/templates/go/events.go.j2`)**:
   * Accesses resolved attribute definitions via `ctx.attributes` (where each item has `.key`, `.type`, `.brief`).
   * Accesses resolved event definitions via `ctx.events` (where each item has `.name`, `.brief`, `.note`, `.annotations`, and `.attributes`).
   * Uses the `go_export_name` and `go_param_name` macros to ensure Go initialism conventions (e.g., `id` -> `ID` in exported names and trailing parameter names like `policyID` and `policySetRevisionID`).
   * After editing `events.go.j2`, always run `make gen-events && make precommit`.
