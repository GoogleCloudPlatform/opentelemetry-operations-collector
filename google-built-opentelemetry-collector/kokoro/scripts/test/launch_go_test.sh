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

IMAGE=us-east4-docker.pkg.dev/stackdriver-test-143416/build-tools/test-env:latest

ENV_FILE"=$(mktemp env-file-XXXXXXX.txt)"
env > "${ENV_FILE}"

docker run \
  --env-file="${ENV_FILE}" \
  -v "${KOKORO_ARTIFACTS_DIR}/:/artifacts/" \
  "${IMAGE}" \
  /bin/bash /artifacts/otelcol-google/kokoro/scripts/test/go_test.sh
