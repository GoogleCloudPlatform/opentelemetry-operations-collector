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

// SelfResourceDetectionProcessorConfig returns the resourcedetection processor
// configuration for the collector's self-observability pipelines.
//
// Self-telemetry always originates from the collector process itself, so all
// three detectors ([env, gcp, system]) run with override: false (preserving
// attributes set in service::telemetry::resource, such as service.instance.id
// and gcp.fleet_id):
//   - "env" reads standard OpenTelemetry identity and platform attributes from
//     OTEL_RESOURCE_ATTRIBUTES (for example Kubernetes Downward API pod/node
//     attributes or a custom host.id), while ignoring arbitrary custom or
//     workload-routing attributes that should not apply to collector
//     self-telemetry.
//   - "gcp" resolves Google Cloud platform metadata when running on Google Cloud.
//   - "system" (with host.id enabled and os.type disabled) falls back to the OS
//     machine ID and host.name off Google Cloud without adding extra attributes
//     on Compute Engine.
func SelfResourceDetectionProcessorConfig() map[string]any {
	return map[string]any{
		"detectors": []string{"env", "gcp", "system"},
		"override":  false,
		"timeout":   10 * time.Second,
		"env": map[string]any{
			"attributes": map[string]any{
				"included": []string{
					"cloud.*",
					"faas.*",
					"host.*",
					"k8s.*",
					"location",
					"os.*",
					"service.*",
				},
			},
		},
		"system": map[string]any{
			"resource_attributes": map[string]any{
				"host.id": map[string]any{
					"enabled": true,
				},
				"os.type": map[string]any{
					"enabled": false,
				},
			},
		},
	}
}

// WorkloadResourceDetectionProcessorConfig returns the resourcedetection
// processor configuration for the canonical workload pipelines.
//
// Detectors ([env, gcp, system]) run with override: false so attributes already
// present on incoming telemetry win:
//   - "env" lets operators supply host/node/cloud or routing attributes via
//     OTEL_RESOURCE_ATTRIBUTES while excluding collector process and pod/workload
//     identity attributes (service.*, k8s.pod.*, k8s.container.*, k8s.namespace.*,
//     and Kubernetes controller names) that commonly describe the collector pod
//     itself in Kubernetes deployments and would otherwise leak onto workload
//     telemetry.
//   - "gcp" resolves Google Cloud platform metadata (such as Compute Engine or
//     GKE instance ID and zone/region) when running on Google Cloud.
//   - "system" (with host.id enabled and os.type disabled) falls back to the OS
//     machine ID (/etc/machine-id on Linux, MachineGuid on Windows) and
//     host.name off Google Cloud, ensuring telemetry.googleapis.com receives a
//     unique per-host host.id for generic_node.node_id and
//     prometheus_target.instance without adding extra attributes on Compute
//     Engine.
//
// Note on gateway / forwarding deployments: the canonical workload pipelines
// assume a host/node-agent topology. Detected gcp/system attributes and
// non-excluded host/node-level OTEL_RESOURCE_ATTRIBUTES (such as host.*,
// k8s.node.name, and k8s.cluster.name) apply to all workload telemetry lacking
// those keys, including forwarded OTLP. In gateway setups, edge collectors
// should populate their own host.id and host.name before forwarding, and
// host/node-level OTEL_RESOURCE_ATTRIBUTES should not be set on gateways.
func WorkloadResourceDetectionProcessorConfig() map[string]any {
	return map[string]any{
		"detectors": []string{"env", "gcp", "system"},
		"override":  false,
		"timeout":   10 * time.Second,
		"env": map[string]any{
			"attributes": map[string]any{
				"excluded": []string{
					"service.*",
					"k8s.pod.*",
					"k8s.container.*",
					"k8s.namespace.*",
					"k8s.daemonset.*",
					"k8s.deployment.*",
					"k8s.replicaset.*",
					"k8s.statefulset.*",
					"k8s.job.*",
					"k8s.cronjob.*",
				},
			},
		},
		"system": map[string]any{
			"resource_attributes": map[string]any{
				"host.id": map[string]any{
					"enabled": true,
				},
				"os.type": map[string]any{
					"enabled": false,
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
