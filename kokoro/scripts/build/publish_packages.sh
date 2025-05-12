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

# This script uploads the package files to a GCS bucket so that tests can read
# the packages from there. This is simpler than using Artifact Registry because
# there is no need to take extra steps to forward credentials through
# apt/yum (and zypper is even worse).

set -eux
set -o pipefail

BUCKET="gs://${_GOOGLE_OTEL_STAGING_BUCKET}/google-otel-packages/${KOKORO_BUILD_ID}"
BUCKET_WITH_SLASH="${BUCKET}/"

gcloud storage cp "${KOKORO_GFILE_DIR}"/dist/* "${BUCKET_WITH_SLASH}"

echo "_BUILD_ARTIFACTS_PACKAGE_GCS=${BUCKET}" > "${KOKORO_ARTIFACTS_DIR}/__output_parameters__"
