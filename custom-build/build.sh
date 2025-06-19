#!/bin/bash
docker build --build-arg CUSTOM_COMPONENTS=${_LOUHI_CUSTOM_COMPONENTS} --output=type=oci,dest=$KOKORO_ARTIFACTS_DIR/container.tar --file git/otelcol-google/custom-build/Dockerfile .
