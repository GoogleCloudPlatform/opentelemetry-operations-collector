// Copyright 2026 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package googlepolicy

import (
	"fmt"
	"strconv"
	"time"
)

// ResourceDetectionProcessorConfig returns the resourcedetection processor
// configuration shared by the canonical workload pipelines and the collector's
// self-observability pipelines.
//
// Detectors run with override: false so attributes already present on incoming
// telemetry (or set in service::telemetry::resource on collector self-telemetry,
// such as service.instance.id and gcp.fleet_id) are preserved, and detected
// attributes merge in detector list order (first non-empty value wins):
//   - "env" lets operators supply or override resource attributes (such as a
//     custom host.id or Kubernetes attributes) via OTEL_RESOURCE_ATTRIBUTES.
//   - "gcp" resolves Google Cloud platform metadata (such as Compute Engine
//     instance ID and zone/region) when running on Google Cloud.
//   - "system" (with host.id enabled) falls back to the OS machine ID
//     (/etc/machine-id on Linux, MachineGuid on Windows) along with host.name
//     and os.type off Google Cloud, ensuring telemetry.googleapis.com has a
//     unique host.id per node for generic_node.node_id and
//     prometheus_target.instance.
func ResourceDetectionProcessorConfig() map[string]any {
	return map[string]any{
		"detectors": []string{"env", "gcp", "system"},
		"override":  false,
		"timeout":   10 * time.Second,
		"system": map[string]any{
			"resource_attributes": map[string]any{
				"host.id": map[string]any{
					"enabled": true,
				},
			},
		},
	}
}

// CloudRegionFallbackStatement returns an OTTL resource-context statement that
// sets cloud.region to region only when the resource carries no location at
// all (no cloud.region, cloud.availability_zone or location attribute).
//
// It is meant to run in a transform processor after resource detection, so a
// region or zone detected for the host (e.g. by the gcp detector on Compute
// Engine) is never replaced, while hosts with nothing detected (off Google
// Cloud) still get the location that telemetry.googleapis.com requires.
// It returns "" if region is empty.
func CloudRegionFallbackStatement(region string) string {
	if region == "" {
		return ""
	}
	return fmt.Sprintf(
		`set(attributes["cloud.region"], %s) where attributes["cloud.region"] == nil and attributes["cloud.availability_zone"] == nil and attributes["location"] == nil`,
		strconv.Quote(region),
	)
}
