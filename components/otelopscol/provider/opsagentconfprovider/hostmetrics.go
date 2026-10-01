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

const defaultHostmetricsCollectionInterval = "60s"

var networkDirectionMap = map[string]string{
	"receive":  "rx",
	"transmit": "tx",
}

func hostmetricsReceiver(collectionInterval string, isWindows bool) map[string]any {
	if collectionInterval == "" {
		collectionInterval = defaultHostmetricsCollectionInterval
	}

	processScraper := map[string]any{
		"mute_process_name_error": true,
		"mute_process_exe_error":  true,
		"mute_process_all_errors": true,
	}
	if isWindows {
		processScraper["metrics"] = map[string]any{
			"process.handles": map[string]any{
				"enabled": true,
			},
		}
	}

	return map[string]any{
		"collection_interval": collectionInterval,
		"scrapers": map[string]any{
			"cpu": map[string]any{
				"metrics": map[string]any{
					"system.cpu.time": map[string]any{
						"attributes": []string{"cpu", "state"},
					},
					"system.cpu.logical.count": map[string]any{
						"enabled": false,
					},
				},
			},
			"disk":       map[string]any{},
			"filesystem": map[string]any{},
			"load":       map[string]any{},
			"memory":     map[string]any{},
			"network":    map[string]any{},
			"paging":     map[string]any{},
			"process":    processScraper,
			"processes":  map[string]any{},
		},
	}
}

func hostmetricsProcessors(receiverID string, isWindows bool) []namedProcessor {
	return []namedProcessor{
		{
			id: fmt.Sprintf("agentmetrics/%s_0", receiverID),
			config: map[string]any{
				"blank_label_metrics": []string{"system.cpu.utilization"},
			},
		},
		{
			id: fmt.Sprintf("filter/%s_1", receiverID),
			config: metricsExcludeFilterProcessor(
				"system.network.dropped",
				"system.filesystem.inodes.usage",
				"system.paging.faults",
				"system.disk.operation_time",
			),
		},
		{
			id: fmt.Sprintf("metricstransform/%s_2", receiverID),
			config: metricsTransformProcessor(
				hostmetricsTransforms(isWindows)...,
			),
		},
		{
			id:     fmt.Sprintf("transform/%s_3", receiverID),
			config: removeInstrumentationScopeProcessor(),
		},
		{
			id:     fmt.Sprintf("transform/%s_4", receiverID),
			config: removeServiceAttributesProcessor(),
		},
	}
}

func hostmetricsTransforms(isWindows bool) []map[string]any {
	transforms := []map[string]any{
		renameMetric("system.cpu.time", "cpu/usage_time",
			toggleScalarDataType(),
			renameLabel("cpu", "cpu_number"),
			renameLabel("state", "cpu_state"),
		),
		renameMetric("system.cpu.utilization", "cpu/utilization",
			aggregateLabels("mean", "state", "blank"),
			renameLabel("blank", "cpu_number"),
			renameLabel("state", "cpu_state"),
		),
		renameMetric("system.cpu.load_average.1m", "cpu/load_1m"),
		renameMetric("system.cpu.load_average.5m", "cpu/load_5m"),
		renameMetric("system.cpu.load_average.15m", "cpu/load_15m"),
		renameMetric("system.disk.read_io", "disk/read_bytes_count"),
		renameMetric("system.disk.write_io", "disk/write_bytes_count"),
		renameMetric("system.disk.operations", "disk/operation_count"),
		renameMetric("system.disk.io_time", "disk/io_time",
			scaleValue(1000),
			toggleScalarDataType(),
		),
		renameMetric("system.disk.weighted_io_time", "disk/weighted_io_time",
			scaleValue(1000),
			toggleScalarDataType(),
		),
		renameMetric("system.disk.average_operation_time", "disk/operation_time",
			scaleValue(1000),
			toggleScalarDataType(),
		),
		renameMetric("system.disk.pending_operations", "disk/pending_operations",
			toggleScalarDataType(),
		),
		renameMetric("system.disk.merged", "disk/merged_operations"),
		renameMetric("system.filesystem.usage", "disk/bytes_used",
			toggleScalarDataType(),
			aggregateLabels("max", "device", "state"),
		),
		renameMetric("system.filesystem.utilization", "disk/percent_used",
			aggregateLabels("max", "device", "state"),
		),
		renameMetric("system.memory.usage", "memory/bytes_used",
			toggleScalarDataType(),
			aggregateLabelValues("sum", "state", "slab", "slab_reclaimable", "slab_unreclaimable"),
		),
		renameMetric("system.memory.utilization", "memory/percent_used",
			aggregateLabelValues("sum", "state", "slab", "slab_reclaimable", "slab_unreclaimable"),
		),
		renameMetric("system.network.io", "interface/traffic",
			renameLabel("interface", "device"),
			renameLabelValues("direction", networkDirectionMap),
		),
		renameMetric("system.network.errors", "interface/errors",
			renameLabel("interface", "device"),
			renameLabelValues("direction", networkDirectionMap),
		),
		renameMetric("system.network.packets", "interface/packets",
			renameLabel("interface", "device"),
			renameLabelValues("direction", networkDirectionMap),
		),
		renameMetric("system.network.connections", "network/tcp_connections",
			toggleScalarDataType(),
			deleteLabelValue("protocol", "udp"),
			renameLabel("state", "tcp_state"),
			aggregateLabels("sum", "tcp_state"),
			addLabel("port", "all"),
		),
		renameMetric("system.processes.created", "processes/fork_count"),
		renameMetric("system.processes.count", "processes/count_by_state",
			toggleScalarDataType(),
			renameLabel("status", "state"),
		),
		renameMetric("system.paging.usage", "swap/bytes_used",
			toggleScalarDataType(),
		),
		renameMetric("system.paging.utilization", "swap/percent_used"),
		duplicateMetric("swap/percent_used", "pagefile/percent_used",
			aggregateLabels("sum", "state"),
		),
		renameMetric("system.paging.operations", "swap/io",
			aggregateLabels("sum", "direction"),
			renameLabelValues("direction", map[string]string{
				"page_in":  "in",
				"page_out": "out",
			}),
		),
		renameMetric("process.cpu.time", "processes/cpu_time",
			scaleValue(1000000),
			toggleScalarDataType(),
			addLabel("process", "all"),
			deleteLabelValue("state", "wait"),
			renameLabel("state", "user_or_syst"),
			renameLabelValues("user_or_syst", map[string]string{
				"system": "syst",
			}),
		),
		renameMetric("process.disk.read_io", "processes/disk/read_bytes_count",
			addLabel("process", "all"),
		),
		renameMetric("process.disk.write_io", "processes/disk/write_bytes_count",
			addLabel("process", "all"),
		),
		renameMetric("process.memory.usage", "processes/rss_usage",
			toggleScalarDataType(),
			addLabel("process", "all"),
		),
		renameMetric("process.memory.virtual", "processes/vm_usage",
			toggleScalarDataType(),
			addLabel("process", "all"),
		),
	}

	if isWindows {
		transforms = append(transforms,
			renameMetric("process.handles", "processes/windows/handles",
				addLabel("process", "all"),
			),
		)
	}

	transforms = append(transforms, addMetricPrefix(agentDomainPrefix))
	return transforms
}
