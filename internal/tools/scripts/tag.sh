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

set -e

function tag_repo() {
    git tag -a ${1} -m "Update to OpenTelemetry Collector version ${1}"
    printf "Created git tag ${1}. Would you like to push? (y/n) "
    read yn
    if [ "$yn" != "${yn#[Yy]}" ]; then
        git push origin ${1}
    else
        git tag -d ${1}
        echo "Removed tag ${1}"
    fi
}

GBOC_TAG=$1
SERVICE_CONTROL_TAG="components/google-built-opentelemetry-collector/exporter/googleservicecontrolexporter/${GBOC_TAG}"

tag_repo "$GBOC_TAG"
tag_repo "$SERVICE_CONTROL_TAG"