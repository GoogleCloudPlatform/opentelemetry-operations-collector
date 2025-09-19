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

GBOC_TAG=$1

set -e

git tag -a ${GBOC_TAG} -m "Update to OpenTelemetry Collector version ${GBOC_TAG}"
printf "Created git tag ${GBOC_TAG}. Would you like to push? (y/n) "
read yn
if [ "$yn" != "${yn#[Yy]}" ]; then
    git push origin ${GBOC_TAG}
else
    git tag -d ${GBOC_TAG}
    echo "Removed tag ${GBOC_TAG}"
fi
