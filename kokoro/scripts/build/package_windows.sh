#!/usr/bin/env bash
# Copyright 2025 Google LLC
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -eux

cd "${KOKORO_ARTIFACTS_DIR}"/git/otelcol-google/google-built-opentelemetry-collector

# The image we're using at the moment has set GOROOT and that mucks everything
# up. Unset it and let's look for a cleaner image to use as a base.
unset GOROOT

mkdir dist
mv "${KOKORO_GFILE_DIR}"/dist/*.exe dist

make -f goopack.mk goo-package

# Put the output folder directly in KOKORO_ARTIFACTS_DIR instead of being deeply
# nested within it.
mv dist "${KOKORO_ARTIFACTS_DIR}"/dist
