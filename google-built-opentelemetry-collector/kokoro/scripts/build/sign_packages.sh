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

set -eux
set -o pipefail

PKG_DIR="${KOKORO_GFILE_DIR}"/dist

# Temporary, for debugging.
function print_layout() {
  echo "${KOKORO_GFILE_DIR}"
  ls "${KOKORO_GFILE_DIR}" || true
  ls "${KOKORO_GFILE_DIR}"/dist || true
  ls "${KOKORO_ARTIFACTS_DIR}" || true
}
print_layout

chmod 777 "${PKG_DIR}"
chmod 666 "${PKG_DIR}"/*

/escalated_sign/escalated_sign.py --tool=linux_gpg_sign \
  --job-dir=/escalated_sign_jobs -- \
  --loglevel=debug "${PKG_DIR}"/*.deb

/escalated_sign/escalated_sign.py --tool=linux_gpg_sign \
  --job-dir=/escalated_sign_jobs -- \
  --loglevel=debug "${PKG_DIR}"/*.rpm
