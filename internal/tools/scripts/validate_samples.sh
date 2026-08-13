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

# Ensure otelcol-google binary exists and is up to date
(cd google-built-opentelemetry-collector && make build)

# Provide default environment variables required by sample configuration files during validation
export service_name="${service_name:-test_service}"
export consumer_project_id="${consumer_project_id:-test_project}"
export service_config_id="${service_config_id:-test_config}"
export otel_healthcheck_scope="${otel_healthcheck_scope:-test_scope}"
export otel_healthcheck_name="${otel_healthcheck_name:-test_name}"
export otel_healthcheck_port="${otel_healthcheck_port:-8080}"
export consumer_project_number="${consumer_project_number:-123456}"
export instance_id="${instance_id:-test_instance}"
export instance_uuid="${instance_uuid:-test_uuid}"
export location="${location:-us-central1}"

exit_code=0

for dir in $(find samples -type d); do
    configs=""
    for f in "$dir"/*.yaml "$dir"/*.yml; do
        if [ -f "$f" ]; then
            configs="$configs --config=$f"
        fi
    done
    if [ -n "$configs" ]; then
        echo "Validating sample directory: $dir"
        if ! ./google-built-opentelemetry-collector/otelcol-google validate $configs; then
            echo "Validation failed for $dir"
            exit_code=1
        fi
    fi
done

exit $exit_code
