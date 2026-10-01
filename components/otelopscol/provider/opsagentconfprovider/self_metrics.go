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

import (
	"fmt"
	"slices"
)

const (
	defaultMetricsPort         = 20201
	defaultMetricsVersionLabel = "google-cloud-ops-agent-metrics/latest"
	agentDomainPrefix          = "agent.googleapis.com"
	rpcDurationMetric          = "rpc.client.call.duration"
	rpcDurationCountMetric     = "rpc.client.call.duration_count"
	metricsExportRPCMethod     = "opentelemetry.proto.collector.metrics.v1.MetricsService/Export"
	logsExportRPCMethod        = "opentelemetry.proto.collector.logs.v1.LogsService/Export"
)

// grpcToHTTPStatus maps canonical gRPC status codes to HTTP status codes
// following https://github.com/googleapis/googleapis/blob/master/google/rpc/code.proto.
var grpcToHTTPStatus = map[string]string{
	"OK":                  "200",
	"INVALID_ARGUMENT":    "400",
	"FAILED_PRECONDITION": "400",
	"OUT_OF_RANGE":        "400",
	"UNAUTHENTICATED":     "401",
	"PERMISSION_DENIED":   "403",
	"NOT_FOUND":           "404",
	"ALREADY_EXISTS":      "409",
	"ABORTED":             "409",
	"RESOURCE_EXHAUSTED":  "429",
	"CANCELLED":           "499",
	"UNKNOWN":             "500",
	"INTERNAL":            "500",
	"DATA_LOSS":           "500",
	"UNIMPLEMENTED":       "501",
	"UNAVAILABLE":         "503",
	"DEADLINE_EXCEEDED":   "504",
}

// otelErrorTypeToStatus maps OpenTelemetry exporter error.type and rpc.response.status_code
// label values to canonical gRPC status codes.
var otelErrorTypeToStatus = map[string]string{
	"OK":                 "OK",
	"Canceled":           "CANCELLED",
	"Cancelled":          "CANCELLED",
	"canceled":           "CANCELLED",
	"cancelled":          "CANCELLED",
	"Unknown":            "UNKNOWN",
	"Shutdown":           "UNKNOWN",
	"shutdown":           "UNKNOWN",
	"InvalidArgument":    "INVALID_ARGUMENT",
	"DeadlineExceeded":   "DEADLINE_EXCEEDED",
	"Deadline_Exceeded":  "DEADLINE_EXCEEDED",
	"deadline_exceeded":  "DEADLINE_EXCEEDED",
	"NotFound":           "NOT_FOUND",
	"AlreadyExists":      "ALREADY_EXISTS",
	"PermissionDenied":   "PERMISSION_DENIED",
	"ResourceExhausted":  "RESOURCE_EXHAUSTED",
	"FailedPrecondition": "FAILED_PRECONDITION",
	"Aborted":            "ABORTED",
	"OutOfRange":         "OUT_OF_RANGE",
	"Unimplemented":      "UNIMPLEMENTED",
	"Internal":           "INTERNAL",
	"Unavailable":        "UNAVAILABLE",
	"DataLoss":           "DATA_LOSS",
	"Unauthenticated":    "UNAUTHENTICATED",
}

func (b *collectorConfig) addSelfMetrics(port int, versionLabel string, exporterProcessors []string) {
	b.receivers["prometheus/agent_prometheus"] = agentPrometheusReceiver(port)

	promProcIDs := registerProcessors(b.processors, agentPrometheusProcessors())
	otelProcIDs := registerProcessors(b.processors, otelSelfMetricsProcessors(versionLabel))
	loggingProcIDs := registerProcessors(b.processors, loggingSelfMetricsProcessors())

	b.pipelines["metrics/otel"] = map[string]any{
		"receivers":  []string{"prometheus/agent_prometheus"},
		"processors": slices.Concat(promProcIDs, otelProcIDs, exporterProcessors),
		"exporters":  []string{"otlp_grpc/otlp_metrics"},
	}
	b.pipelines["metrics/loggingmetrics"] = map[string]any{
		"receivers":  []string{"prometheus/agent_prometheus"},
		"processors": slices.Concat(promProcIDs, loggingProcIDs, exporterProcessors),
		"exporters":  []string{"otlp_grpc/otlp_metrics"},
	}
}

func agentPrometheusReceiver(port int) map[string]any {
	return map[string]any{
		"config": map[string]any{
			"scrape_configs": []map[string]any{
				{
					"job_name":        "otel-collector",
					"scrape_interval": "1m",
					"static_configs": []map[string]any{
						{
							"targets": []string{fmt.Sprintf("0.0.0.0:%d", port)},
						},
					},
				},
			},
		},
	}
}

func telemetryConfig(port int) map[string]any {
	return map[string]any{
		"metrics": map[string]any{
			"level": "detailed",
			"readers": []map[string]any{
				{
					"pull": map[string]any{
						"exporter": map[string]any{
							"prometheus": map[string]any{
								"host":                "0.0.0.0",
								"port":                port,
								"without_scope_info":  true,
								"without_units":       true,
								"without_type_suffix": true,
							},
						},
					},
				},
			},
		},
	}
}

func extractRPCCountProcessor() map[string]any {
	return metricTransformIgnoreProcessor(
		"metric",
		fmt.Sprintf(`extract_count_metric(true) where name == %q`, rpcDurationMetric),
	)
}

func filterRPCMethodProcessor(rpcMethod string) map[string]any {
	return metricsDatapointFilterProcessor(
		fmt.Sprintf(`metric.name == %q and (not IsMatch(datapoint.attributes["rpc.method"], %q))`, rpcDurationCountMetric, rpcMethod),
	)
}

func agentPrometheusProcessors() []namedProcessor {
	return []namedProcessor{
		{
			id: "transform/agent_prometheus_0",
			config: metricTransformProcessor(
				"resource",
				`delete_key(attributes, "service.name")`,
				`delete_key(attributes, "service.version")`,
				`delete_key(attributes, "service.instance.id")`,
				`delete_key(attributes, "server.port")`,
				`delete_key(attributes, "url.scheme")`,
			),
		},
		{
			id:     "transform/agent_prometheus_1",
			config: removeInstrumentationScopeProcessor(),
		},
		{
			id:     "transform/agent_prometheus_2",
			config: removeServiceAttributesProcessor(),
		},
	}
}

func otelSelfMetricsProcessors(versionLabel string) []namedProcessor {
	return []namedProcessor{
		{
			id:     "transform/otel_0",
			config: extractRPCCountProcessor(),
		},
		{
			id:     "filter/otel_1",
			config: filterRPCMethodProcessor(metricsExportRPCMethod),
		},
		{
			id: "filter/otel_2",
			config: metricsIncludeFilterProcessor(
				"otelcol_process_uptime",
				"otelcol_process_memory_rss",
				"otelcol_exporter_sent_metric_points",
				"otelcol_exporter_send_failed_metric_points",
				rpcDurationCountMetric,
			),
		},
		{
			id: "metricstransform/otel_3",
			config: metricsTransformProcessor(
				renameMetric("otelcol_process_uptime", "agent/uptime",
					toggleScalarDataType(),
					addLabel("version", versionLabel),
					aggregateLabels("sum", "version"),
				),
				renameMetric("otelcol_process_memory_rss", "agent/memory_usage",
					aggregateLabels("sum"),
				),
				renameMetric(rpcDurationCountMetric, "agent/api_request_count",
					renameLabelValues("rpc.response.status_code", otelErrorTypeToStatus),
					renameLabel("rpc.response.status_code", "state"),
					aggregateLabels("sum", "state"),
				),
				updateMetric("otelcol_exporter_sent_metric_points",
					toggleScalarDataType(),
					addLabel("status", "OK"),
					aggregateLabels("sum", "status"),
				),
				updateMetric("otelcol_exporter_send_failed_metric_points",
					toggleScalarDataType(),
					renameLabel("error.type", "status"),
					renameLabelValues("status", otelErrorTypeToStatus),
					aggregateLabels("sum", "status"),
				),
				combineMetrics("otelcol_exporter_sent_metric_points|otelcol_exporter_send_failed_metric_points", "agent/monitoring/point_count",
					aggregateLabels("sum", "status"),
				),
				addMetricPrefix(agentDomainPrefix),
			),
		},
	}
}

func loggingSelfMetricsProcessors() []namedProcessor {
	return []namedProcessor{
		{
			id:     "transform/loggingmetrics_0",
			config: extractRPCCountProcessor(),
		},
		{
			id:     "filter/loggingmetrics_1",
			config: filterRPCMethodProcessor(logsExportRPCMethod),
		},
		{
			id: "filter/loggingmetrics_2",
			config: metricsIncludeFilterProcessor(
				"otelcol_exporter_sent_log_records",
				"otelcol_exporter_send_failed_log_records",
				rpcDurationCountMetric,
			),
		},
		{
			id: "metricstransform/loggingmetrics_3",
			config: metricsTransformProcessor(
				duplicateMetric("otelcol_exporter_send_failed_log_records", "agent/log_entry_retry_count",
					toggleScalarDataType(),
					addLabel("response_code", "400"),
					aggregateLabels("sum", "response_code"),
				),
				renameMetric(rpcDurationCountMetric, "agent/request_count",
					renameLabelValues("rpc.response.status_code", otelErrorTypeToStatus),
					renameLabel("rpc.response.status_code", "response_code"),
					renameLabelValues("response_code", grpcToHTTPStatus),
					aggregateLabels("sum", "response_code"),
				),
				renameMetric("otelcol_exporter_sent_log_records", "agent/log_entry_count",
					toggleScalarDataType(),
					addLabel("response_code", "200"),
					aggregateLabels("sum", "response_code"),
				),
				renameMetric("otelcol_exporter_send_failed_log_records", "agent/log_entry_count",
					toggleScalarDataType(),
					addLabel("response_code", "400"),
					aggregateLabels("sum", "response_code"),
				),
				combineMetrics(`^agent/log_entry_count$`, "agent/log_entry_count",
					aggregateLabels("sum", "response_code"),
				),
			),
		},
		{
			id: "transform/loggingmetrics_4",
			config: metricTransformProcessor(
				"metric",
				`set(unit, "1")`,
			),
		},
		{
			id:     "interval/loggingmetrics_5",
			config: intervalProcessor("1m"),
		},
		{
			id: "metricstransform/loggingmetrics_6",
			config: metricsTransformProcessor(
				addMetricPrefix(agentDomainPrefix),
			),
		},
	}
}
