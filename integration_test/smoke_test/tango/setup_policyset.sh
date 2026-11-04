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

# One-time setup for TestTangoSmoke: opts the project into the staging
# Telemetry Management API, creates (or updates) the smoke test fleet and
# PolicySet, and rolls the latest PolicySet revision out to the fleet.
#
# Not run by CI. Requires a Google corp workstation (for the Tenant Manager
# CLI) and permission to manage services in the project.
#
# Usage: ./setup_policyset.sh PROJECT_ID [LOCATION]

set -euo pipefail

PROJECT_ID="${1:?usage: $0 PROJECT_ID [LOCATION]}"
LOCATION="${2:-asia-east1}"
FLEET_ID="${FLEET_ID:-gboc-smoke-fleet}"
POLICY_SET_ID="${POLICY_SET_ID:-gboc-smoke-ps}"
SERVICE="staging-telemetrymanagement.sandbox.googleapis.com"
TM_BIN="${TM_BIN:-/google/data/ro/teams/tenantmanager/tools/tm}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

PROJECT_NUMBER="$(gcloud projects describe "${PROJECT_ID}" --format='value(projectNumber)')"
BASE="https://${SERVICE}/v1alpha/projects/${PROJECT_NUMBER}/locations/${LOCATION}"

echo "==> Opting ${PROJECT_ID} into ${SERVICE}"
gcloud services enable daily-serviceconsumermanagement.sandbox.googleapis.com --project="${PROJECT_ID}"
"${TM_BIN}" policies sharing allow --user_project="${PROJECT_ID}" "${SERVICE}" "${PROJECT_ID}"
"${TM_BIN}" labels add --user_project="${PROJECT_ID}" "${SERVICE}" TRUSTED_TESTER_COLLECTORS "${PROJECT_ID}"
gcloud services enable "${SERVICE}" --project="${PROJECT_ID}"

api() {
  local method="$1" url="$2"
  shift 2
  curl -sS --fail-with-body -X "${method}" \
    -H "Authorization: Bearer $(gcloud auth print-access-token)" \
    -H "X-Goog-User-Project: ${PROJECT_ID}" \
    -H "X-Goog-Visibilities: TRUSTED_TESTER_COLLECTORS" \
    -H "Content-Type: application/json" \
    "$@" "${url}"
}

echo "==> Ensuring fleet ${FLEET_ID}"
if ! api GET "${BASE}/fleets/${FLEET_ID}" >/dev/null 2>&1; then
  api POST "${BASE}/fleets?fleetId=${FLEET_ID}" \
    -d '{"description":"Tango-GBOC smoke test fleet"}'
fi

echo "==> Creating or updating PolicySet ${POLICY_SET_ID}"
if api GET "${BASE}/policySets/${POLICY_SET_ID}" >/dev/null 2>&1; then
  PS="$(api PATCH "${BASE}/policySets/${POLICY_SET_ID}?updateMask=description,receiverPolicies,componentPolicies,universalPolicies" \
    -d @"${SCRIPT_DIR}/policyset.json")"
else
  PS="$(api POST "${BASE}/policySets?policySetId=${POLICY_SET_ID}" \
    -d @"${SCRIPT_DIR}/policyset.json")"
fi
REVISION="$(python3 -c 'import json,sys; print(json.load(sys.stdin)["policySetRevision"])' <<<"${PS}")"
echo "    revision: ${REVISION}"

ROLLOUT_ID="r-$(date +%s)"
echo "==> Rolling out ${REVISION} to ${FLEET_ID} (${ROLLOUT_ID})"
api POST "${BASE}/fleets/${FLEET_ID}/rollouts?rolloutId=${ROLLOUT_ID}" \
  -d "{\"policySetRevision\":\"${REVISION}\"}" >/dev/null
api GET "${BASE}/fleets/${FLEET_ID}/rollouts/${ROLLOUT_ID}"

echo
echo "Set TANGO_EXPECTED_REVISION=${REVISION##*/} when running the test (see README.md) and update the revision in README.md."
