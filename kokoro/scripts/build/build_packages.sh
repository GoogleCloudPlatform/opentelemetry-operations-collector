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

# Temporary, for debugging.
function print_layout() {
  echo "${KOKORO_ARTIFACTS_DIR}"
  ls "${KOKORO_ARTIFACTS_DIR}" || true
  pwd
  ls .
}
print_layout

cd "${KOKORO_ARTIFACTS_DIR}"/git/otelcol-google/google-built-opentelemetry-collector

function cheat() {
  mkdir -p dist

  gsutil cp -r gs://cloud-built-otel-collector-file-transfers/martijnvs-temp-fast-iterations/297140bc-63c9-4b51-aac5-b64d3310d31a/deb/git/otelcol-google/google-built-opentelemetry-collector/dist/* dist
  gsutil cp -r gs://cloud-built-otel-collector-file-transfers/martijnvs-temp-fast-iterations/297140bc-63c9-4b51-aac5-b64d3310d31a/rpm/git/otelcol-google/google-built-opentelemetry-collector/dist/* dist
}

function build() {
  unset GOROOT

  # TODO: remove this
  echo "_VERSION: ${_VERSION}"

  # Avoids "fatal: detected dubious ownership in repository" errors.
  #git config --global --add safe.directory "${KOKORO_ARTIFACTS_DIR}/git/otelcol-google"

  make goreleaser-release
}

cheat
# build

./dist/otelcol-google-linux_linux_amd64_v1 version || echo 'version 1 failed'
./dist/otelcol-google-linux_linux_amd64_v1 -version || echo 'version 2 failed'
./dist/otelcol-google-linux_linux_amd64_v1 --version || echo 'version 3 failed'

ls dist || true  # Temporary, for debugging.

mv dist "${KOKORO_ARTIFACTS_DIR}"/dist

