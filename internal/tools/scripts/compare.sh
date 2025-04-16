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

make compare-google-built-otel
GOOGLE_OTEL_RESULT=$?
make compare-otelopscol
OTELOPSCOL_RESULT=$?

# If either result is 2, which is what `make` exits with when
# either check exits with code 1, exit this script with code 1.
if [[ $GOOGLE_OTEL_RESULT -eq 2 || $OTELOPSCOL_RESULT -eq 2 ]]; then
    exit 1
fi
