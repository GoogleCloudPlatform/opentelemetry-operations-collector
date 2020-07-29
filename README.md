# OpenTelemetry Operations Collector

This repository is focused on building and packaging the OpenTelemetry Collector with the necessary tools in different OSs to be compatible with Google Cloud Monitoring.

## Prerequisite

[docker](https://docs.docker.com/engine/install/) is installed

## Building the Docker Image

Run `make docker-build-image`

## Collector for Linux

To generate a tarball file that packages the executable OpenTelemetry binary with the configuration file compatible with Google Cloud Monitoring.
1. Make sure docker image has already been built: `docker images`
2. Run `make TARGET=build-tarball docker-run`
3. The tarball file will be generated in the `dist` folder
