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

# This script uploads the package files to two places:
# 1. A GCS bucket so that tests can read the packages from there.
#    This is simpler than using Artifact Registry.
# 2. Artifact Registry, so that we can use a Louhi Promotion (not yet
#    implemented by Louhi) to publish the artifacts in a MOSS-friendly way.

set -eux
set -o pipefail

BUCKET="gs://${_GOOGLE_OTEL_STAGING_BUCKET}/google-otel-packages/${KOKORO_BUILD_ID}"
BUCKET_WITH_SLASH="${BUCKET}/"

gcloud storage cp "${KOKORO_GFILE_DIR}"/dist/* "${BUCKET_WITH_SLASH}"

LOCATION=us
DESCRIPTION="Staging repository for GBOC Linux Packages"
# "ephemeral=true" will cause the following flow to clean up the repo after a month:
# https://louhi.dev/6025093129699328/flow-configuration/07c78361-6487-4f09-8708-7ac478e8daaa
LABELS="ephemeral=true,kokoro_build_id=${KOKORO_BUILD_ID}"

for PACKAGE in "${KOKORO_GFILE_DIR}"/dist/*.deb; do
  gcloud artifacts apt upload "${_APT_STAGING_REPO}" \
    --project="${_STAGING_ARTIFACTS_PROJECT_ID}" \
    --location="${LOCATION}" \
    --source="${PACKAGE}"
done

for PACKAGE in "${KOKORO_GFILE_DIR}"/dist/*.rpm; do
  gcloud artifacts yum upload "${_YUM_STAGING_REPO}" \
    --project="${_STAGING_ARTIFACTS_PROJECT_ID}" \
    --location="${LOCATION}" \
    --source="${PACKAGE}"
done

echo "_BUILD_ARTIFACTS_PACKAGE_GCS=${BUCKET}" > "${KOKORO_ARTIFACTS_DIR}/__output_parameters__"
