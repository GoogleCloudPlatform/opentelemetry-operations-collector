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

func (r MetricsReceiver) iisReceiver(escapedID string) (string, map[string]any) {
	if r.ReceiverVersion == "2" {
		return fmt.Sprintf("iis/%s", escapedID), map[string]any{
			"collection_interval": r.collectionInterval(),
		}
	}

	return fmt.Sprintf("windowsperfcounters/%s", escapedID), map[string]any{
		"collection_interval": r.collectionInterval(),
		"perfcounters": []map[string]any{
			{
				"object":    "Web Service",
				"instances": []string{"_Total"},
				"counters": []map[string]string{
					{"name": "Current Connections"},
					{"name": "Total Bytes Received"},
					{"name": "Total Bytes Sent"},
					{"name": "Total Connection Attempts (all instances)"},
					{"name": "Total Delete Requests"},
					{"name": "Total Get Requests"},
					{"name": "Total Head Requests"},
					{"name": "Total Options Requests"},
					{"name": "Total Post Requests"},
					{"name": "Total Put Requests"},
					{"name": "Total Trace Requests"},
				},
			},
		},
	}
}

func (r MetricsReceiver) iisProcessors(escapedID string) []namedProcessor {
	if r.ReceiverVersion == "2" {
		return []namedProcessor{
			{
				id: fmt.Sprintf("transform/%s_0", escapedID),
				config: flattenResourceAndSetScopeProcessor(
					"agent.googleapis.com/iis",
					"2.0",
					`set(attributes["site"], resource.attributes["iis.site"])`,
					`set(attributes["app_pool"], resource.attributes["iis.application_pool"])`,
				),
			},
			{
				id:     fmt.Sprintf("transform/%s_1", escapedID),
				config: removeServiceAttributesProcessor(),
			},
			{
				id: fmt.Sprintf("transform/%s_2", escapedID),
				config: metricTransformProcessor(
					"datapoint",
					`keep_keys(resource.attributes, [])`,
				),
			},
			{
				id:     fmt.Sprintf("groupbyattrs/%s_3", escapedID),
				config: map[string]any{},
			},
			{
				id: fmt.Sprintf("metricstransform/%s_4", escapedID),
				config: metricsTransformProcessor(
					regexpUpdateMetric("^iis", aggregateLabels("sum", "direction", "request")),
					addMetricPrefix("workload.googleapis.com"),
				),
			},
			{
				id:     fmt.Sprintf("metric_start_time/%s_5", escapedID),
				config: metricStartTimeProcessor(),
			},
		}
	}

	return []namedProcessor{
		{
			id: fmt.Sprintf("metricstransform/%s_0", escapedID),
			config: metricsTransformProcessor(
				renameMetric(
					`\Web Service(_Total)\Current Connections`,
					"iis/current_connections",
				),
				combineMetrics(
					`^\\Web Service\(_Total\)\\Total Bytes (?P<direction>.*)$`,
					"iis/network/transferred_bytes_count",
					toggleScalarDataType(),
				),
				renameMetric(
					`\Web Service(_Total)\Total Connection Attempts (all instances)`,
					"iis/new_connection_count",
					toggleScalarDataType(),
				),
				combineMetrics(
					`^\\Web Service\(_Total\)\\Total (?P<http_method>.*) Requests$`,
					"iis/request_count",
					toggleScalarDataType(),
				),
				addMetricPrefix("agent.googleapis.com"),
			),
		},
		{
			id: fmt.Sprintf("transform/%s_1", escapedID),
			config: metricTransformProcessor(
				"metric",
				`convert_gauge_to_sum("cumulative", true) where name == "agent.googleapis.com/iis/network/transferred_bytes_count"`,
				`convert_gauge_to_sum("cumulative", true) where name == "agent.googleapis.com/iis/new_connection_count"`,
				`convert_gauge_to_sum("cumulative", true) where name == "agent.googleapis.com/iis/request_count"`,
			),
		},
		{
			id:     fmt.Sprintf("metric_start_time/%s_2", escapedID),
			config: metricStartTimeProcessor(),
		},
		{
			id:     fmt.Sprintf("transform/%s_3", escapedID),
			config: setInstrumentationScopeProcessor("agent.googleapis.com/iis", "1.0"),
		},
		{
			id:     fmt.Sprintf("transform/%s_4", escapedID),
			config: removeInstrumentationScopeProcessor(),
		},
		{
			id:     fmt.Sprintf("transform/%s_5", escapedID),
			config: removeServiceAttributesProcessor(),
		},
	}
}
