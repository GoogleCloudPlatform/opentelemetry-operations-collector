This folder contains OpenTelemetry configuration for metrics defined by UCP.
Those metrics belong to "saasmanagement.googleapis.com/Instance" monitored
resource, and should only be sent to producer projects.

We prefix all components with `ucp_internal_`, so that our configs do not clash
with service producers' configs.
