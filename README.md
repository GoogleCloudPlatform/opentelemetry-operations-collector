# OpenTelemetry Operations Collector

This repository contains tooling and OpenTelemetry Collector distributions used for Google-specific purposes.

* `distrogen`: a tool for generating OpenTelemetry Collector distributions built by OCB
* Two OpenTelemetry Collector distributions generated by `distrogen`
  * [`google-built-opentelemetry-collector`](#google-built-opentelemetry-collector): The foundation for the Google-Built OpenTelemetry Collector
  * [`otelopscol`](#otelopscol): The OpenTelemetry Collector backing the [Ops Agent](https://cloud.google.com/stackdriver/docs/solutions/agents/ops-agent)
* Numerous [custom OpenTelemetry Collector components](https://opentelemetry.io/docs/collector/building/) that are used in `google-built-opentelemetry-collector` or `otelopscol`

## Google-Built OpenTelemetry Collector

<<<<<<< HEAD
The Google-Built OpenTelemetry Collector is an open-source, production-ready build of the upstream OpenTelemetry Collector that is built with upstream OpenTelemetry components. The `google-built-opentelemetry-collector` folder is generated by `distrogen`, and the specification is at `specs/google-built-opentelemetry-collector.yaml`.
=======
The Google-Built OpenTelemetry Collector is an open-source, production-ready build of the upstream OpenTelemetry Collector that is built entirely with upstream OpenTelemetry components. The `google-built-opentelemetry-collector` folder is generated by `distrogen`, and the specification is at `specs/google-built-opentelemetry-collector.yaml`.
>>>>>>> 98f6d25b (Repo cleanup)

## otelopscol

NOTE: This product is currently only supported by Google as a component of the Ops Agent. This collector is usable on its own, however that method of usage is unsupported. If you have issues with this collector while using it through the Ops Agent, please go through Ops Agent support channels.

otelopscol is the OpenTelemetry Collector backing the [Ops Agent](https://cloud.google.com/stackdriver/docs/solutions/agents/ops-agent). It is tooled specifically for exporting the Ops Agent to send telemetry data to Google Cloud. It includes some custom receivers and processors built specifically to support Ops Agent features.