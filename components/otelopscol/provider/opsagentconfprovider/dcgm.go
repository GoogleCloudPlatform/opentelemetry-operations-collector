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

package opsagentconfprovider

import "fmt"

const defaultDCGMEndpoint = "localhost:5555"

var disabledDCGMV1Metrics = []string{
	"gpu.dcgm.utilization",
	"gpu.dcgm.codec.encoder.utilization",
	"gpu.dcgm.codec.decoder.utilization",
	"gpu.dcgm.memory.bytes_used",
	"gpu.dcgm.energy_consumption",
	"gpu.dcgm.temperature",
	"gpu.dcgm.clock.frequency",
	"gpu.dcgm.clock.throttle_duration.time",
	"gpu.dcgm.ecc_errors",
	"gpu.dcgm.xid_errors",
}

var enabledDCGMV1Metrics = []string{
	"gpu.dcgm.sm.utilization",
	"gpu.dcgm.sm.occupancy",
	"gpu.dcgm.pipe.utilization",
	"gpu.dcgm.memory.bandwidth_utilization",
	"gpu.dcgm.pcie.io",
	"gpu.dcgm.nvlink.io",
}

func (r MetricsReceiver) dcgmReceiver(escapedID string) (string, map[string]any) {
	endpoint := r.Endpoint
	if endpoint == "" {
		endpoint = defaultDCGMEndpoint
	}

	if r.ReceiverVersion == "2" {
		return fmt.Sprintf("dcgm/%s", escapedID), map[string]any{
			"collection_interval": r.collectionInterval(),
			"endpoint":            endpoint,
		}
	}

	metricsConfig := make(map[string]any, len(disabledDCGMV1Metrics)+len(enabledDCGMV1Metrics))
	for _, m := range disabledDCGMV1Metrics {
		metricsConfig[m] = map[string]any{"enabled": false}
	}
	for _, m := range enabledDCGMV1Metrics {
		metricsConfig[m] = map[string]any{"enabled": true}
	}

	return fmt.Sprintf("dcgm/%s", escapedID), map[string]any{
		"collection_interval": r.collectionInterval(),
		"endpoint":            endpoint,
		"metrics":             metricsConfig,
	}
}

func (r MetricsReceiver) dcgmProcessors(escapedID string) []namedProcessor {
	if r.ReceiverVersion == "2" {
		return []namedProcessor{
			{
				id: fmt.Sprintf("metricstransform/%s_0", escapedID),
				config: metricsTransformProcessor(
					updateMetric("gpu.dcgm.pipe.utilization", renameLabel("gpu.pipe", "pipe")),
					updateMetric("gpu.dcgm.memory.bytes_used", renameLabel("gpu.memory.state", "state")),
					updateMetric("gpu.dcgm.nvlink.io", renameLabel("network.io.direction", "direction")),
					updateMetric("gpu.dcgm.pcie.io", renameLabel("network.io.direction", "direction")),
					updateMetric("gpu.dcgm.clock.throttle_duration.time", renameLabel("gpu.clock.violation", "violation")),
					updateMetric("gpu.dcgm.ecc_errors", renameLabel("gpu.error.type", "error_type")),
					updateMetric("gpu.dcgm.xid_errors", renameLabel("gpu.error.xid", "xid")),
				),
			},
			{
				id: fmt.Sprintf("metricstransform/%s_1", escapedID),
				config: metricsTransformProcessor(
					addMetricPrefix("workload.googleapis.com"),
				),
			},
			dcgmFlattenResourceProcessor(escapedID, 2, "2.0"),
		}
	}

	return []namedProcessor{
		{
			id: fmt.Sprintf("metricstransform/%s_0", escapedID),
			config: metricsTransformProcessor(
				renameMetric(
					"gpu.dcgm.memory.bandwidth_utilization",
					"dcgm.gpu.profiling.dram_utilization",
				),
				renameMetric(
					"gpu.dcgm.nvlink.io",
					"dcgm.gpu.profiling.nvlink_traffic_rate",
					renameLabel("network.io.direction", "direction"),
					renameLabelValues("direction", networkDirectionMap),
				),
				renameMetric(
					"gpu.dcgm.pcie.io",
					"dcgm.gpu.profiling.pcie_traffic_rate",
					renameLabel("network.io.direction", "direction"),
					renameLabelValues("direction", networkDirectionMap),
				),
				renameMetric(
					"gpu.dcgm.pipe.utilization",
					"dcgm.gpu.profiling.pipe_utilization",
					renameLabel("gpu.pipe", "pipe"),
				),
				renameMetric(
					"gpu.dcgm.sm.occupancy",
					"dcgm.gpu.profiling.sm_occupancy",
				),
				renameMetric(
					"gpu.dcgm.sm.utilization",
					"dcgm.gpu.profiling.sm_utilization",
				),
			),
		},
		{
			id: fmt.Sprintf("cumulativetodelta/%s_1", escapedID),
			config: cumulativeToDeltaProcessor(
				"dcgm.gpu.profiling.nvlink_traffic_rate",
				"dcgm.gpu.profiling.pcie_traffic_rate",
			),
		},
		{
			id: fmt.Sprintf("deltatorate/%s_2", escapedID),
			config: deltaToRateProcessor(
				"dcgm.gpu.profiling.nvlink_traffic_rate",
				"dcgm.gpu.profiling.pcie_traffic_rate",
			),
		},
		{
			id: fmt.Sprintf("metricstransform/%s_3", escapedID),
			config: metricsTransformProcessor(
				updateMetric("dcgm.gpu.profiling.nvlink_traffic_rate", toggleScalarDataType()),
				updateMetric("dcgm.gpu.profiling.pcie_traffic_rate", toggleScalarDataType()),
			),
		},
		{
			id: fmt.Sprintf("metricstransform/%s_4", escapedID),
			config: metricsTransformProcessor(
				addMetricPrefix("workload.googleapis.com"),
			),
		},
		dcgmFlattenResourceProcessor(escapedID, 5, "1.0"),
	}
}

func dcgmFlattenResourceProcessor(escapedID string, idx int, version string) namedProcessor {
	return namedProcessor{
		id: fmt.Sprintf("transform/%s_%d", escapedID, idx),
		config: flattenResourceAndSetScopeProcessor(
			"agent.googleapis.com/dcgm",
			version,
			`set(attributes["model"], resource.attributes["gpu.model"])`,
			`set(attributes["gpu_number"], resource.attributes["gpu.number"])`,
			`set(attributes["uuid"], resource.attributes["gpu.uuid"])`,
		),
	}
}
