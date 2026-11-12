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

# Root pre-start script that rejects non-DNS TCP traffic from otelcol-google to
# 169.254.169.254. Port 53 stays open so GCE's internal resolver can still
# resolve *.googleapis.com, and root stays unrestricted so google-guest-agent
# keeps SSH access working.

set -eu
export PATH="$PATH:/usr/sbin:/sbin"

METADATA_IP="169.254.169.254"
COLLECTOR_USER="otelcol-google"

if command -v iptables >/dev/null 2>&1; then
  iptables -C OUTPUT -p tcp -d "$METADATA_IP" ! --dport 53 -m owner --uid-owner "$COLLECTOR_USER" -j REJECT --reject-with tcp-reset 2>/dev/null ||
    iptables -I OUTPUT -p tcp -d "$METADATA_IP" ! --dport 53 -m owner --uid-owner "$COLLECTOR_USER" -j REJECT --reject-with tcp-reset
elif command -v nft >/dev/null 2>&1; then
  nft list table inet tango_offgcp >/dev/null 2>&1 && nft delete table inet tango_offgcp || true
  nft add table inet tango_offgcp
  nft 'add chain inet tango_offgcp output { type filter hook output priority 0 ; policy accept ; }'
  nft "add rule inet tango_offgcp output ip daddr $METADATA_IP tcp dport != 53 meta skuid \"$COLLECTOR_USER\" reject with tcp reset"
else
  echo "neither iptables nor nft is available" >&2
  exit 1
fi
