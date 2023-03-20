# Build and Test

All commands documented here will reference the `make` targets. If you don't have `make` installed, you can run simplified versions of the `make` commands that don't include things like embedding Build Info.

All builds require Go version 1.18 or greater.

### Base Collector

To build the base collector with no optional features:
```
make build
```
There is also a target provided with a default collector name that includes the platform and architecture:
```
make build_full_name
```
Or you can customize build output by providing the `GO_BUILD_OUT` environment variable to the `build` target:
```
GO_BUILD_OUT=./dist/collector make build
```
To run base collector tests:
```
make test
```

### GPU Support

Additional requirements:
* CGO support (having C build tools in your path should do it)

To build the collector with GPU receiver support added, you can use the build tag `gpu`:
```
GO_BUILD_TAGS=gpu make build
```
Building with GPU support on platforms other than `linux` or without CGO enabled will fail.

To run tests with GPU support:
```
GO_BUILD_TAGS=gpu make test
```

### JMX Receiver Support

Additional requirements:
* A valid JMX Jar and its sha256 hash

To build the collector with JMX receiver support, you can provide the environment variable JMX_JAR_SHA:
```
JMX_HASH=<sha256 of JMX jar> make build
```
Testing with a JMX Jar SHA currently does not affect tests.