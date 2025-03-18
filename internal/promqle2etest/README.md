# PromQL e2e tests

This directory contains (manual for now) test suite that allows testing various Prometheus
compliance elements around different Prometheus metric cases going through OpenTelemetry
to Prometheus and GCM.

## Usage

For now those tests are skipped by default (we can make them run on every CI or 
nightly). To run those you need:

1. Install Go and Docker if not on your machine.
2. Obtain GCM secret with the permissions to read and write to GCM for the test project of your choice. Put the JSON body into a `GCM_SECRET` envvar. 
3.

