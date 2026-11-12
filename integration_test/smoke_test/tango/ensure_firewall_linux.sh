#!/bin/sh
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

# Ensures either iptables or nft is installed on a Linux VM before detaching its
# GCE service account in off-GCP mode.

set -eu
export PATH="$PATH:/usr/sbin:/sbin"

if ! command -v iptables >/dev/null 2>&1 && ! command -v nft >/dev/null 2>&1; then
  if command -v apt-get >/dev/null 2>&1; then
    apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y nftables
  elif command -v dnf >/dev/null 2>&1; then
    dnf install -y nftables
  elif command -v yum >/dev/null 2>&1; then
    yum install -y nftables
  elif command -v zypper >/dev/null 2>&1; then
    zypper --non-interactive install nftables
  fi
fi
