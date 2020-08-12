# OpenTelemetry Operations Collector

This repository is focused on building and packaging the OpenTelemetry Collector for use with Google Cloud Monitoring.

## Collector for Linux

To generate a tarball archive that includes the OpenTelemetry binary and a configuration file compatible with Google Cloud Monitoring:
1. Run `make build-tarball`
2. The tarball file will be generated in the `dist` folder

## Collector for Windows

To generate an MSI that will install OpenTelemetry as a Windows service using a configuration file compatible with Google Cloud Monitoring:
1. Run `.build\msi\make.ps1 Install-Tools` to install the open source [WIX Toolset](https://wixtoolset.org)
2. Run `.build\msi\make.ps1 New-MSI`
3. The MSI file will be generated in the `dist` folder

Alternatively, you can generate a [googet](https://github.com/google/googet) package by running `make build-googet`. This is the packaging method used to install the Collector on GCE VMs.

## Running build commands in Docker

You can also run the build commands inside docker:
1. Run `make docker-build-image` to build the docker image. This will generate an image called `otelopscol-build`
2. Run `make TARGET=build-<package> docker-run`
3. The specified package will be generated in the `dist` folder
