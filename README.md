# OpenTelemetry Operations Collector

This repository is focused on building and packaging the OpenTelemetry Collector for use with Google Cloud Monitoring.


## Collector for Linux

To generate a tarball file that packages the executable OpenTelemetry binary with the configuration file compatible with Google Cloud Monitoring.
1. Run `make build-tarball`
    - To also include JVM, Apache, MySQL and StasD supports, run `make build-tarball-exporters` instead
2. The tarball file will be generated in the `dist` folder

## Running build commands in Docker

Alternatively, you can run the build commands inside docker:
1. Run `make docker-build-image` to build the docker image. This will generate an image called `otelopscol-build`
2. Run `make TARGET=build-<package> docker-run`
3. The specified package will be generated in the `dist` folder
