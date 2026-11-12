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
# Telemetry Management and Telemetry Director APIs, creates (or updates) the
# Linux and Windows smoke test fleets and PolicySets, and rolls the latest
# PolicySet revisions out to their respective fleets.
#
# When WIF_PROJECT_ID is also passed (e.g. gboc-tango-testing, a project outside
# the google.com organization), the script also sets up the X.509 Workload
# Identity Pool (gboc-x509-pool), X.509 provider (gboc-x509-prov), certificates,
# credential configuration files, and IAM bindings for off-GCP smoke testing.
#
# Not run by CI. Requires a Google corp workstation (for the Tenant Manager
# CLI) and permission to manage services in the project.
#
# Usage: ./setup_policyset.sh PROJECT_ID [LOCATION] [WIF_PROJECT_ID] [WIF_OUT_DIR]

set -euo pipefail

PROJECT_ID="${1:?usage: $0 PROJECT_ID [LOCATION] [WIF_PROJECT_ID] [WIF_OUT_DIR]}"
LOCATION="${2:-asia-east1}"
WIF_PROJECT_ID="${3:-${WIF_PROJECT_ID:-}}"
WIF_OUT_DIR="${4:-${WIF_OUT_DIR:-/tmp/gboc-tango-wif}}"
FLEET_ID="${FLEET_ID:-gboc-smoke-fleet}"
POLICY_SET_ID="${POLICY_SET_ID:-gboc-smoke-ps}"
WINDOWS_FLEET_ID="${WINDOWS_FLEET_ID:-gboc-smoke-fleet-windows}"
WINDOWS_POLICY_SET_ID="${WINDOWS_POLICY_SET_ID:-gboc-smoke-ps-windows}"
WIF_POOL_ID="${WIF_POOL_ID:-gboc-x509-pool}"
WIF_PROVIDER_ID="${WIF_PROVIDER_ID:-gboc-x509-prov}"
WIF_WORKLOAD_CN="${WIF_WORKLOAD_CN:-gboc-smoke-workload}"
WIF_CERT_SECRET="${WIF_CERT_SECRET:-gboc-x509-leaf-cert}"
WIF_KEY_SECRET="${WIF_KEY_SECRET:-gboc-x509-leaf-key}"
ROTATE_WIF_CERTS="${ROTATE_WIF_CERTS:-false}"
SERVICE="staging-telemetrymanagement.sandbox.googleapis.com"
TD_SERVICE="staging-telemetrydirector.sandbox.googleapis.com"
TM_BIN="${TM_BIN:-/google/data/ro/teams/tenantmanager/tools/tm}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

PROJECT_NUMBER="$(gcloud projects describe "${PROJECT_ID}" --format='value(projectNumber)')"
BASE="https://${SERVICE}/v1alpha/projects/${PROJECT_NUMBER}/locations/${LOCATION}"

echo "==> Opting ${PROJECT_ID} into ${SERVICE} and enabling ${TD_SERVICE}"
gcloud services enable daily-serviceconsumermanagement.sandbox.googleapis.com --project="${PROJECT_ID}"
"${TM_BIN}" policies sharing allow --user_project="${PROJECT_ID}" "${SERVICE}" "${PROJECT_ID}"
"${TM_BIN}" labels add --user_project="${PROJECT_ID}" "${SERVICE}" TRUSTED_TESTER_COLLECTORS "${PROJECT_ID}"
gcloud services enable "${SERVICE}" "${TD_SERVICE}" --project="${PROJECT_ID}"
gcloud beta services identity create --service="${SERVICE}" --project="${PROJECT_ID}" >/dev/null 2>&1 || true
gcloud projects add-iam-policy-binding "${PROJECT_ID}" \
  --member="serviceAccount:service-${PROJECT_NUMBER}@gcp-sa-staging-telmgmt.iam.gserviceaccount.com" \
  --role="roles/telemetrymanagement.serviceAgent" \
  --condition=None --quiet >/dev/null

COMPUTE_SA="serviceAccount:${PROJECT_NUMBER}-compute@developer.gserviceaccount.com"
for ROLE in roles/logging.logWriter roles/monitoring.metricWriter roles/telemetry.writer roles/trafficdirector.client roles/viewer; do
  gcloud projects add-iam-policy-binding "${PROJECT_ID}" \
    --member="${COMPUTE_SA}" \
    --role="${ROLE}" \
    --condition=None --quiet >/dev/null
done

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

api_status() {
  local method="$1" url="$2"
  curl -sS -o /dev/null -w "%{http_code}" -X "${method}" \
    -H "Authorization: Bearer $(gcloud auth print-access-token)" \
    -H "X-Goog-User-Project: ${PROJECT_ID}" \
    -H "X-Goog-Visibilities: TRUSTED_TESTER_COLLECTORS" \
    "${url}"
}

upsert_fleet_and_policyset() {
  local fleet_id="$1" ps_id="$2" ps_file="$3" desc="$4" out_var="$5"

  echo "==> Ensuring fleet ${fleet_id}"
  if [[ "$(api_status GET "${BASE}/fleets/${fleet_id}")" == "404" ]]; then
    api POST "${BASE}/fleets?fleetId=${fleet_id}" \
      -d "{\"description\":\"${desc}\"}" >/dev/null
  else
    api PATCH "${BASE}/fleets/${fleet_id}?updateMask=description" \
      -d "{\"description\":\"${desc}\"}" >/dev/null
  fi

  echo "==> Creating or updating PolicySet ${ps_id} from $(basename "${ps_file}")"
  local status ps=""
  status="$(api_status GET "${BASE}/policySets/${ps_id}")"
  if [[ "${status}" == "404" ]]; then
    ps="$(api POST "${BASE}/policySets?policySetId=${ps_id}" -d @"${ps_file}")"
  else
    if ! ps="$(api PATCH "${BASE}/policySets/${ps_id}?updateMask=description,receiverPolicies,componentPolicies,universalPolicies" -d @"${ps_file}" 2>/dev/null)"; then
      echo "    PATCH on ${ps_id} failed (legacy revision incompatible); recreating ${ps_id}"
      api DELETE "${BASE}/policySets/${ps_id}?force=true" >/dev/null 2>&1 || api DELETE "${BASE}/policySets/${ps_id}" >/dev/null
      ps="$(api POST "${BASE}/policySets?policySetId=${ps_id}" -d @"${ps_file}")"
    fi
  fi

  local rev rollout_id
  rev="$(python3 -c 'import json,sys; print(json.load(sys.stdin)["policySetRevision"])' <<<"${ps}")"
  echo "    revision: ${rev}"

  rollout_id="r-$(date +%s)"
  echo "==> Rolling out ${rev} to ${fleet_id} (${rollout_id})"
  api POST "${BASE}/fleets/${fleet_id}/rollouts?rolloutId=${rollout_id}" \
    -d "{\"policySetRevision\":\"${rev}\"}" >/dev/null
  api GET "${BASE}/fleets/${fleet_id}/rollouts/${rollout_id}"
  echo
  printf -v "${out_var}" '%s' "${rev##*/}"
}

LINUX_REV=""
WINDOWS_REV=""
upsert_fleet_and_policyset "${FLEET_ID}" "${POLICY_SET_ID}" "${SCRIPT_DIR}/policyset.json" "Tango-GBOC Linux smoke test fleet" LINUX_REV
upsert_fleet_and_policyset "${WINDOWS_FLEET_ID}" "${WINDOWS_POLICY_SET_ID}" "${SCRIPT_DIR}/policyset_windows.json" "Tango-GBOC Windows smoke test fleet" WINDOWS_REV

echo "Set TANGO_EXPECTED_REVISION=${LINUX_REV} TANGO_EXPECTED_REVISION_WINDOWS=${WINDOWS_REV} when running the test (see README.md) and update the revisions in README.md."

if [[ -n "${WIF_PROJECT_ID}" ]]; then
  echo
  echo "==> Setting up off-GCP X.509 WIF pool in ${WIF_PROJECT_ID}"
  WIF_PROJECT_NUMBER="$(gcloud projects describe "${WIF_PROJECT_ID}" --format='value(projectNumber)')"

  gcloud services enable \
    iam.googleapis.com \
    sts.googleapis.com \
    iamcredentials.googleapis.com \
    cloudresourcemanager.googleapis.com \
    compute.googleapis.com \
    logging.googleapis.com \
    monitoring.googleapis.com \
    secretmanager.googleapis.com \
    telemetry.googleapis.com \
    --project="${WIF_PROJECT_ID}"

  if ! gcloud iam workload-identity-pools describe "${WIF_POOL_ID}" --project="${WIF_PROJECT_ID}" --location=global >/dev/null 2>&1; then
    gcloud iam workload-identity-pools create "${WIF_POOL_ID}" \
      --project="${WIF_PROJECT_ID}" \
      --location=global \
      --display-name="GBOC X.509 Smoke Pool"
  fi

  PROVIDER_EXISTS=false
  if gcloud iam workload-identity-pools providers describe "${WIF_PROVIDER_ID}" \
    --workload-identity-pool="${WIF_POOL_ID}" --project="${WIF_PROJECT_ID}" --location=global >/dev/null 2>&1; then
    PROVIDER_EXISTS=true
  fi
  SECRETS_EXIST=false
  if gcloud secrets describe "${WIF_CERT_SECRET}" --project="${WIF_PROJECT_ID}" >/dev/null 2>&1 && \
     gcloud secrets describe "${WIF_KEY_SECRET}" --project="${WIF_PROJECT_ID}" >/dev/null 2>&1; then
    SECRETS_EXIST=true
  fi

  if [[ "${PROVIDER_EXISTS}" == "true" && "${SECRETS_EXIST}" == "true" && "${ROTATE_WIF_CERTS}" != "true" ]]; then
    echo "==> Reusing existing X.509 provider ${WIF_PROVIDER_ID} and Secret Manager secrets (${WIF_CERT_SECRET}, ${WIF_KEY_SECRET})"
    echo "    (Set ROTATE_WIF_CERTS=true to generate a new CA, update the provider trust store, and rotate the secrets.)"
  else
    if [[ "${PROVIDER_EXISTS}" == "true" && "${ROTATE_WIF_CERTS}" != "true" ]] && \
       [[ ! -f "${WIF_OUT_DIR}/ca.pem" || ! -f "${WIF_OUT_DIR}/leaf.pem" || ! -f "${WIF_OUT_DIR}/leaf.key" ]]; then
      echo "ERROR: ${WIF_PROVIDER_ID} already exists but Secret Manager secrets or ${WIF_OUT_DIR}/{ca.pem,leaf.pem,leaf.key} are missing." >&2
      echo "Point WIF_OUT_DIR at the existing CA and leaf cert/key, or set ROTATE_WIF_CERTS=true to replace the provider trust store." >&2
      exit 1
    fi
    mkdir -p "${WIF_OUT_DIR}"
    chmod 0700 "${WIF_OUT_DIR}"
    if [[ "${ROTATE_WIF_CERTS}" == "true" || ! -f "${WIF_OUT_DIR}/ca.pem" || ! -f "${WIF_OUT_DIR}/leaf.pem" || ! -f "${WIF_OUT_DIR}/leaf.key" ]]; then
      echo "==> Generating X.509 CA and workload leaf certificate in ${WIF_OUT_DIR}"
      openssl req -x509 -newkey rsa:2048 -nodes \
        -keyout "${WIF_OUT_DIR}/ca.key" \
        -out "${WIF_OUT_DIR}/ca.pem" \
        -days 3650 \
        -subj "/CN=gboc-tango-x509-ca" \
        -addext "basicConstraints=critical,CA:TRUE" \
        -addext "keyUsage=critical,keyCertSign,cRLSign" \
        -addext "subjectKeyIdentifier=hash"

      openssl req -newkey rsa:2048 -nodes \
        -keyout "${WIF_OUT_DIR}/leaf.key" \
        -out "${WIF_OUT_DIR}/leaf.csr" \
        -subj "/CN=${WIF_WORKLOAD_CN}"

      cat > "${WIF_OUT_DIR}/leaf_ext.cnf" <<EOF
basicConstraints=critical,CA:FALSE
keyUsage=critical,digitalSignature,keyEncipherment
extendedKeyUsage=clientAuth
subjectKeyIdentifier=hash
authorityKeyIdentifier=keyid,issuer
EOF

      openssl x509 -req \
        -in "${WIF_OUT_DIR}/leaf.csr" \
        -CA "${WIF_OUT_DIR}/ca.pem" \
        -CAkey "${WIF_OUT_DIR}/ca.key" \
        -CAcreateserial \
        -out "${WIF_OUT_DIR}/leaf.pem" \
        -days 3650 \
        -extfile "${WIF_OUT_DIR}/leaf_ext.cnf"
    fi

    python3 - "${WIF_OUT_DIR}" <<'PY'
import json, os, sys
out_dir = sys.argv[1]
with open(os.path.join(out_dir, "ca.pem"), encoding="utf-8") as f:
    ca_pem = f.read().strip()
with open(os.path.join(out_dir, "trust_store.json"), "w", encoding="utf-8") as f:
    json.dump({"trustStore": {"trustAnchors": [{"pemCertificate": ca_pem}]}}, f, indent=2)
PY

    if [[ "${PROVIDER_EXISTS}" == "true" ]]; then
      gcloud iam workload-identity-pools providers update-x509 "${WIF_PROVIDER_ID}" \
        --workload-identity-pool="${WIF_POOL_ID}" \
        --project="${WIF_PROJECT_ID}" \
        --location=global \
        --trust-store-config-path="${WIF_OUT_DIR}/trust_store.json" \
        --attribute-mapping="google.subject=assertion.subject.dn.cn"
    else
      gcloud iam workload-identity-pools providers create-x509 "${WIF_PROVIDER_ID}" \
        --workload-identity-pool="${WIF_POOL_ID}" \
        --project="${WIF_PROJECT_ID}" \
        --location=global \
        --trust-store-config-path="${WIF_OUT_DIR}/trust_store.json" \
        --attribute-mapping="google.subject=assertion.subject.dn.cn"
    fi

    for pair in "${WIF_CERT_SECRET}:leaf.pem" "${WIF_KEY_SECRET}:leaf.key"; do
      secret="${pair%%:*}"
      file="${WIF_OUT_DIR}/${pair#*:}"
      if ! gcloud secrets describe "${secret}" --project="${WIF_PROJECT_ID}" >/dev/null 2>&1; then
        gcloud secrets create "${secret}" --project="${WIF_PROJECT_ID}" --replication-policy=automatic
      fi
      gcloud secrets versions add "${secret}" --project="${WIF_PROJECT_ID}" --data-file="${file}"
    done
    echo "==> Stored workload certificate and key in Secret Manager (${WIF_CERT_SECRET}, ${WIF_KEY_SECRET})"
  fi

  PRINCIPAL="principal://iam.googleapis.com/projects/${WIF_PROJECT_NUMBER}/locations/global/workloadIdentityPools/${WIF_POOL_ID}/subject/${WIF_WORKLOAD_CN}"
  for ROLE in roles/logging.logWriter roles/monitoring.metricWriter roles/telemetry.writer roles/trafficdirector.client roles/viewer; do
    gcloud projects add-iam-policy-binding "${WIF_PROJECT_ID}" \
      --member="${PRINCIPAL}" \
      --role="${ROLE}" \
      --condition=None --quiet >/dev/null
  done

  echo "==> X.509 WIF pool, provider, and Secret Manager credentials ready in ${WIF_PROJECT_ID}"
fi
