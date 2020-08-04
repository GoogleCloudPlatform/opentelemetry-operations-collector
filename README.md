# OpenTelemetry Operations Collector

This repository is focused on building and packaging the OpenTelemetry Collector for use with Google Cloud Monitoring.

## Prerequisite

[docker](https://docs.docker.com/engine/install/) is installed

## Building the Docker Image

Run `make docker-build-image`

## Collector for Linux

To generate a tarball file that packages the executable OpenTelemetry binary with the configuration file compatible with Google Cloud Monitoring.
1. Run `make build-tarball`
2. The tarball file will be generated in the `dist` folder

Alternatively, you can  run the make commands inside docker.
1. Make sure docker image has already been built: run `docker images` to check if `otelopscol-build` image exists
2. If image doesn't exist, run `make docker-build-image` to build an image from the dockerfile
2. Run `make TARGET=build-tarball docker-run`
3. The tarball file will be generated in the `dist` folder
