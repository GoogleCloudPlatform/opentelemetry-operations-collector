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

IMAGE_DIGEST=$(gcloud artifacts docker images describe \
us-docker.pkg.dev/cloud-ops-agents-artifacts/google-cloud-opentelemetry-collector-standard/otelcol-google:latest \
--format="value(image_summary.digest)")

VSA_VERSION_AND_FILENAME=$(gcloud artifacts attachments list \
--repository=google-cloud-opentelemetry-collector-standard \
--location=us \
--project=cloud-ops-agents-artifacts \
--target=projects/cloud-ops-agents-artifacts/locations/us/repositories/google-cloud-opentelemetry-collector-standard/packages/otelcol-google/versions/$IMAGE_DIGEST \
--filter="type:application/vnd.in-toto.verification_summary" \
--format="value[separator=';'](ociVersionName,files[0])" | head -n 1)

IFS=";"
read -r VSA_VERSION VSA_FILENAME <<< "$VSA_VERSION_AND_FILENAME"
unset IFS

VSA_FILENAME="${VSA_FILENAME#*:}"

gcloud artifacts attachments download \
--oci-version-name=$VSA_VERSION \
--destination=.

mv "$VSA_FILENAME" "vsa-${IMAGE_DIGEST}.intoto.jsonl"