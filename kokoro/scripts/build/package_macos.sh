#!/usr/bin/env bash
# Copyright 2026 Google LLC
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

# Copy over all binaries and packages built/signed from previous stages.
mv "${KOKORO_GFILE_DIR}"/dist .

# Copy the macOS arm64 binary, package it as a dmg.
VERSION=$(grep '^version:' spec.yaml | awk '{print $2}')
mkdir -p /tmp/dmg_root
cp dist/otelcol-google_"${VERSION}"_darwin_arm64 /tmp/dmg_root/otelcol-google
hdiutil create -fs HFS+ -srcfolder /tmp/dmg_root -volname "otelcol-google-${VERSION}" dist/otelcol-google_"${VERSION}"_darwin_arm64.dmg

# Put the output folder directly in KOKORO_ARTIFACTS_DIR instead of being deeply
# nested within it.
mv dist "${KOKORO_ARTIFACTS_DIR}"/dist
