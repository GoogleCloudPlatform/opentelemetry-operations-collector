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
set -u
set -x
set -o pipefail

# cd to google-built-opentelemetry-collector.
cd "$(readlink -f "$(dirname "$0")")"
cd ../../../

# Avoids "fatal: detected dubious ownership in repository" errors on Kokoro containers.
git config --global --add safe.directory "$(pwd)"

function set_image_specs() {
  # if ARCH is not set, return an error
  if [[ -z "${ARCH:-}" ]]; then
    echo "ARCH is required." 1>&2
    return 1
  fi

  # Extracts all representative and exhaustive image_specs matching $ARCH from distros.yaml.
  IMAGE_SPECS="$(python3 -c "import yaml
all_distros = []
targets=yaml.safe_load(open('distros.yaml'))['targets']
for target in targets:
  if target == 'windows':
    continue
  test_distros = targets[target]['architectures']['${ARCH}']['test_distros']
  all_distros += test_distros['representative']
  if 'exhaustive' in test_distros:
    all_distros += test_distros['exhaustive']
print(','.join(all_distros))")"
  export IMAGE_SPECS
}

# Note: if we ever need to change regions, we will need to set up a new
# Cloud Router and Cloud NAT gateway for that region. This is because
# we use --no-address on Kokoro, because of b/169084857.
# The new Cloud NAT gateway must have "Minimum ports per VM instance"
# set to 512 as per this article:
# https://cloud.google.com/knowledge/kb/sles-unable-to-fetch-updates-when-behind-cloud-nat-000004450
#
# T2A machines are only available on us-central1-{a,b,f}. Hardcoding these zones
# will make it easier to support aarch64 in the future.
ZONES=us-central1-a,us-central1-b,us-central1-f
export ZONES

set_image_specs

LOGS_DIR="${KOKORO_ARTIFACTS_DIR}/logs"
mkdir -p "${LOGS_DIR}"

cd "integration_test/${TEST_SUITE_NAME}"

OTELCOL_CONFIGS_DIR="../otelcol_configs"
export OTELCOL_CONFIGS_DIR

# Boost the max number of open files from 1024 to 1 million.
ulimit -n 1000000

# Set up some command line flags for "gotestsum".
gotestsum_args=(
  --packages=./...
  --format=standard-verbose
  --junitfile="${LOGS_DIR}/sponge_log.xml"
)
if [[ -n "${GOTESTSUM_RERUN_FAILS:-}" ]]; then
  gotestsum_args+=( "--rerun-fails=${GOTESTSUM_RERUN_FAILS}" )
fi

# Set up some command line flags for "go test".
go_test_args=(
  -test.parallel=1000
  -timeout=3h
  -tags=integration_test
)
if [[ -n "${TEST_SELECTOR:-}" ]]; then
  go_test_args+=( "-test.run=${TEST_SELECTOR}" )
fi

TEST_UNDECLARED_OUTPUTS_DIR="${LOGS_DIR}" \
  gotestsum "${gotestsum_args[@]}" \
  -- "${go_test_args[@]}"
