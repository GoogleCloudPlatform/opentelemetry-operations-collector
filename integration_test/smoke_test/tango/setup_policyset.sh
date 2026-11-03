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
WIF_POOL_ID="${WIF_POOL_ID:-gboc-x509-pool}"
WIF_PROVIDER_ID="${WIF_PROVIDER_ID:-gboc-x509-prov}"
WIF_WORKLOAD_CN="${WIF_WORKLOAD_CN:-gboc-smoke-workload}"
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
    telemetry.googleapis.com \
    --project="${WIF_PROJECT_ID}"

  mkdir -p "${WIF_OUT_DIR}"
  if [[ ! -f "${WIF_OUT_DIR}/ca.pem" ]] && gcloud iam workload-identity-pools providers describe "${WIF_PROVIDER_ID}" \
    --workload-identity-pool="${WIF_POOL_ID}" --project="${WIF_PROJECT_ID}" --location=global >/dev/null 2>&1; then
    # Generating a new CA here would replace the provider's trust store below
    # and invalidate every workload certificate issued by the existing CA.
    echo "ERROR: ${WIF_PROVIDER_ID} already exists but ${WIF_OUT_DIR}/ca.pem does not." >&2
    echo "Point WIF_OUT_DIR at the directory holding the existing CA (see README.md), or delete the provider first." >&2
    exit 1
  fi
  if [[ ! -f "${WIF_OUT_DIR}/ca.pem" ]]; then
    echo "==> Generating X.509 CA and workload leaf certificate in ${WIF_OUT_DIR}"
    openssl req -x509 -newkey rsa:2048 -nodes \
      -keyout "${WIF_OUT_DIR}/ca.key" \
      -out "${WIF_OUT_DIR}/ca.pem" \
      -days 3650 \
      -subj "/CN=gboc-tango-testing-root-ca/O=Google" \
      -addext "basicConstraints=critical,CA:TRUE" \
      -addext "keyUsage=critical,keyCertSign,cRLSign" \
      -addext "subjectKeyIdentifier=hash"

    openssl req -newkey rsa:2048 -nodes \
      -keyout "${WIF_OUT_DIR}/leaf.key" \
      -out "${WIF_OUT_DIR}/leaf.csr" \
      -subj "/CN=${WIF_WORKLOAD_CN}/O=Google"

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

  python3 - "${WIF_OUT_DIR}" "${WIF_PROJECT_NUMBER}" "${WIF_POOL_ID}" "${WIF_PROVIDER_ID}" <<'PY'
import json, os, sys
out_dir, proj_num, pool_id, prov_id = sys.argv[1:5]
with open(os.path.join(out_dir, "ca.pem")) as f:
    ca_pem = f.read()
with open(os.path.join(out_dir, "trust_store.json"), "w") as f:
    json.dump({"trustAnchors": [{"pemCertificate": ca_pem}]}, f, indent=2)
cert_cfg_path = os.path.join(out_dir, "cert_config.json")
with open(cert_cfg_path, "w") as f:
    json.dump({
        "cert_configs": {
            "workload": {
                "cert_path": os.path.join(out_dir, "leaf.pem"),
                "key_path": os.path.join(out_dir, "leaf.key"),
            }
        }
    }, f, indent=2)
with open(os.path.join(out_dir, "cred_config.json"), "w") as f:
    json.dump({
        "universe_domain": "googleapis.com",
        "type": "external_account",
        "audience": f"//iam.googleapis.com/projects/{proj_num}/locations/global/workloadIdentityPools/{pool_id}/providers/{prov_id}",
        "subject_token_type": "urn:ietf:params:oauth:token-type:mtls",
        "token_url": "https://sts.mtls.googleapis.com/v1/token",
        "credential_source": {
            "certificate": {
                "certificate_config_location": cert_cfg_path
            }
        }
    }, f, indent=2)
PY

  if ! gcloud iam workload-identity-pools describe "${WIF_POOL_ID}" --project="${WIF_PROJECT_ID}" --location=global >/dev/null 2>&1; then
    gcloud iam workload-identity-pools create "${WIF_POOL_ID}" \
      --project="${WIF_PROJECT_ID}" \
      --location=global \
      --display-name="GBOC X.509 Smoke Pool"
  fi

  if gcloud iam workload-identity-pools providers describe "${WIF_PROVIDER_ID}" \
    --workload-identity-pool="${WIF_POOL_ID}" \
    --project="${WIF_PROJECT_ID}" \
    --location=global >/dev/null 2>&1; then
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

  PRINCIPAL="principal://iam.googleapis.com/projects/${WIF_PROJECT_NUMBER}/locations/global/workloadIdentityPools/${WIF_POOL_ID}/subject/${WIF_WORKLOAD_CN}"
  for ROLE in roles/logging.logWriter roles/monitoring.metricWriter roles/trafficdirector.client roles/viewer; do
    gcloud projects add-iam-policy-binding "${WIF_PROJECT_ID}" \
      --member="${PRINCIPAL}" \
      --role="${ROLE}" >/dev/null
  done

  echo "==> X.509 WIF pool and credentials ready in ${WIF_OUT_DIR}/cred_config.json"
fi
