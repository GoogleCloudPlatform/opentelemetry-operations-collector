// Copyright 2021 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package confgenerator

import (
	"context"

	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/internal/confgenerator/otel"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/internal/platform"
)

type MetricsReceiverIis struct {
	ConfigComponent `yaml:",inline"`

	MetricsReceiverShared `yaml:",inline"`

	VersionedReceivers `yaml:",inline"`
}

func (r MetricsReceiverIis) Type() string {
	return "iis"
}

func (r MetricsReceiverIis) Pipelines(ctx context.Context) ([]otel.ReceiverPipeline, error) {
	if r.IsV2() {
		return []otel.ReceiverPipeline{otel.ReceiverPipeline{
			Receiver: otel.Component{
				Type: "iis",
				Config: map[string]interface{}{
					"collection_interval": r.CollectionIntervalString(),
				},
			},
			Processors: map[string][]otel.Component{"metrics": {
				otel.TransformationMetrics(
					otel.FlattenResourceAttribute("iis.site", "site"),
					otel.FlattenResourceAttribute("iis.application_pool", "app_pool"),
					otel.SetScopeName("agent.googleapis.com/"+r.Type()),
					otel.SetScopeVersion("2.0"),
				),
				otel.MetricsRemoveServiceAttributes(),
				// Drop all resource keys; Must be done in a separate transform,
				// otherwise the above flatten resource attribute queries will only
				// work for the first datapoint
				otel.TransformationMetrics(
					otel.RetainResource(),
				),
				otel.CondenseResourceMetrics(),
				otel.MetricsTransform(
					otel.UpdateMetricRegexp("^iis",
						otel.AggregateLabels(
							"sum",
							"direction",
							"request",
						),
					),
					otel.AddPrefix("workload.googleapis.com"),
				),
				otel.MetricStartTime(),
			}},
		}}, nil
	}

	// Return version 1 if version is anything other than 2
	return []otel.ReceiverPipeline{otel.ReceiverPipeline{
		Receiver: otel.Component{
			Type: "windowsperfcounters",
			Config: map[string]interface{}{
				"collection_interval": r.CollectionIntervalString(),
				"perfcounters": []map[string]interface{}{
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
			},
		},
		Processors: map[string][]otel.Component{"metrics": append([]otel.Component{
			otel.MetricsTransform(
				otel.RenameMetric(
					`\Web Service(_Total)\Current Connections`,
					"iis/current_connections",
				),
				// $ needs to be escaped because reasons.
				// https://github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/processor/metricstransformprocessor#rename-multiple-metrics-using-substitution
				otel.CombineMetrics(
					`^\\Web Service\(_Total\)\\Total Bytes (?P<direction>.*)$$`,
					"iis/network/transferred_bytes_count",
					// change data type from double -> int64
					otel.ToggleScalarDataType,
				),
				otel.RenameMetric(
					`\Web Service(_Total)\Total Connection Attempts (all instances)`,
					"iis/new_connection_count",
					// change data type from double -> int64
					otel.ToggleScalarDataType,
				),
				otel.CombineMetrics(
					`^\\Web Service\(_Total\)\\Total (?P<http_method>.*) Requests$$`,
					"iis/request_count",
					// change data type from double -> int64
					otel.ToggleScalarDataType,
				),
				otel.AddPrefix("agent.googleapis.com"),
			),
			otel.TransformationMetrics(
				otel.ConvertGaugeToSum("agent.googleapis.com/iis/network/transferred_bytes_count"),
				otel.ConvertGaugeToSum("agent.googleapis.com/iis/new_connection_count"),
				otel.ConvertGaugeToSum("agent.googleapis.com/iis/request_count"),
			),
			otel.MetricStartTime(),
			otel.TransformationMetrics(
				otel.SetScopeName("agent.googleapis.com/"+r.Type()),
				otel.SetScopeVersion("1.0"),
			),
		}, otel.ConvertGCMSystemExporterToOtlpExporter()...)},
	}}, nil
}

func init() {
	MetricsReceiverTypes.RegisterType(func() MetricsReceiver { return &MetricsReceiverIis{} }, platform.Windows)
}
