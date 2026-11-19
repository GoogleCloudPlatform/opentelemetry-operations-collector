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

set -euo pipefail

FUZZTIME="${FUZZTIME:-5s}"

# Fast pre-check strictly within the current Go module (pruning nested go.mod subtrees)
# so modules with no Fuzz targets exit immediately without downloading dependencies.
has_fuzz=0
while IFS= read -r file; do
  if grep -q '^func Fuzz' "${file}"; then
    has_fuzz=1
    break
  fi
done < <(find . \( -name '.git' -o -name '.tools' \) -prune -o \( ! -path . -type d -exec test -f '{}/go.mod' \; -prune \) -o \( -name '*_test.go' -type f -print \))

if [[ "${has_fuzz}" -eq 0 ]]; then
  exit 0
fi

pkgs="$(
  go list -f '{{if or .TestGoFiles .XTestGoFiles}}{{.ImportPath}} {{.Dir}}{{end}}' ./... |
    while read -r pkg dir; do
      if [[ -n "${pkg}" ]] && grep -q '^func Fuzz' "${dir}"/*_test.go 2>/dev/null; then
        echo "${pkg}"
      fi
    done
)"

if [[ -z "${pkgs}" ]]; then
  exit 0
fi

for pkg in ${pkgs}; do
  list_out="$(go test -list='^Fuzz' "${pkg}")"
  targets="$(printf '%s\n' "${list_out}" | grep '^Fuzz' || true)"
  for target in ${targets}; do
    echo "=== Fuzzing ${pkg}: ${target} (fuzztime=${FUZZTIME}) ==="
    go test -run='^$' -fuzz="^${target}$" -fuzztime="${FUZZTIME}" "${pkg}"
  done
done
