# Nvidia NVML Receiver

This receiver uses Nvidia's NVML [Go API](https://github.com/NVIDIA/go-nvml) to collect Nvidia GPU metrics.

## `gpu` Build Tag

When the `gpu` build tag is set, this receiver will be built with full functionality enabled. This requires `CGO` support in your build environment.

When the `gpu` build tag is not set, no functionality is built and the receiver factory will return an error when attempting to construct.
