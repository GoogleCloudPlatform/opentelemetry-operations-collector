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

# Downloads the upstream OBI (opentelemetry-ebpf-instrumentation) release
# source archive (obi-vX.Y.Z-source-generated.tar.gz), which contains
# pre-generated bpf2go source and object files not tracked in git, verifies its
# SHA256 checksum against SHA256SUMS, and extracts it to .tools/obi-src.
#
# Mirrors open-telemetry/opentelemetry-collector-releases/scripts/prepare-obi.sh
# until upstream OCB custom pre-build plugins land.

set -euo pipefail

REPO_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )/../../.." &> /dev/null && pwd )"
MANIFEST="${1:-${REPO_DIR}/google-built-opentelemetry-collector/manifest.yaml}"
OBI_DIR="${REPO_DIR}/.tools/obi-src"

if [[ ! -f "${MANIFEST}" ]]; then
  echo "ERROR: manifest not found at ${MANIFEST}" >&2
  exit 1
fi

OBI_VERSION="${OBI_VERSION:-$(
  awk '/- gomod: go\.opentelemetry\.io\/obi\/collector / {print $NF; exit}' "${MANIFEST}"
)}"

if [[ -z "${OBI_VERSION}" ]]; then
  exit 0
fi

TARBALL="obi-${OBI_VERSION}-source-generated.tar.gz"
BASE_URL="https://github.com/open-telemetry/opentelemetry-ebpf-instrumentation/releases/download/${OBI_VERSION}"
TARBALL_CACHE="${REPO_DIR}/.tools/${TARBALL}"
OBI_STAMP="${OBI_DIR}/.obi-${OBI_VERSION}"

if [[ -f "${OBI_STAMP}" ]]; then
  echo "OBI ${OBI_VERSION} already prepared at ${OBI_DIR}"
  exit 0
fi

echo "Fetching OBI ${OBI_VERSION} source-generated tarball..."
mkdir -p "${REPO_DIR}/.tools"
if [[ ! -f "${TARBALL_CACHE}" ]]; then
  curl --fail --show-error --location --retry 3 --retry-delay 1 \
    --output "${TARBALL_CACHE}" "${BASE_URL}/${TARBALL}"
fi

echo "Verifying OBI ${OBI_VERSION} tarball checksum..."
if ! expected_checksum="$(
  curl --fail --show-error --location --silent --retry 3 --retry-delay 1 \
    "${BASE_URL}/SHA256SUMS" \
    | awk -v filename="${TARBALL}" '
        {
          checksum_filename = $2
          sub(/^\*/, "", checksum_filename)
          if (checksum_filename == filename) {
            print $1
            found = 1
            exit
          }
        }
        END { if (!found) exit 1 }
      '
)"; then
  rm -f "${TARBALL_CACHE}"
  echo "ERROR: ${TARBALL} not found in SHA256SUMS." >&2
  exit 1
fi

if [[ "$(uname -s)" == "Linux" ]]; then
  actual_checksum="$(sha256sum "${TARBALL_CACHE}" | awk '{print $1}')"
else
  actual_checksum="$(shasum -a 256 "${TARBALL_CACHE}" | awk '{print $1}')"
fi

if [[ "${actual_checksum}" != "${expected_checksum}" ]]; then
  rm -f "${TARBALL_CACHE}"
  echo "ERROR: checksum verification failed for ${TARBALL}." >&2
  exit 1
fi
echo "SHA256 verified: ${TARBALL}"

rm -rf "${OBI_DIR}"
mkdir -p "${OBI_DIR}"
tar xzf "${TARBALL_CACHE}" --strip-components=1 -C "${OBI_DIR}"
touch "${OBI_STAMP}"
echo "OBI ${OBI_VERSION} source prepared at ${OBI_DIR}"
