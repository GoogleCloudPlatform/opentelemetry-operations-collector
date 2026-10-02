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
	"path"
	"regexp"
	"sort"
	"strings"
)

type namedProcessor struct {
	id     string
	config map[string]any
}

func registerProcessors(dst map[string]any, list []namedProcessor) []string {
	ids := make([]string, 0, len(list))
	for _, p := range list {
		dst[p.id] = p.config
		ids = append(ids, p.id)
	}
	return ids
}

func metricTransformProcessor(context string, statements ...string) map[string]any {
	return map[string]any{
		"metric_statements": []any{
			map[string]any{
				"context":    context,
				"statements": statements,
			},
		},
	}
}

func metricTransformIgnoreProcessor(context string, statements ...string) map[string]any {
	cfg := metricTransformProcessor(context, statements...)
	cfg["error_mode"] = "ignore"
	return cfg
}

func setInstrumentationScopeProcessor(name, version string) map[string]any {
	return metricTransformProcessor(
		"scope",
		fmt.Sprintf(`set(name, %q)`, name),
		fmt.Sprintf(`set(version, %q)`, version),
	)
}

func flattenResourceAndSetScopeProcessor(scopeName, scopeVersion string, datapointStatements ...string) map[string]any {
	return map[string]any{
		"metric_statements": []any{
			map[string]any{
				"context":    "datapoint",
				"statements": datapointStatements,
			},
			map[string]any{
				"context": "scope",
				"statements": []string{
					fmt.Sprintf(`set(name, %q)`, scopeName),
					fmt.Sprintf(`set(version, %q)`, scopeVersion),
				},
			},
		},
	}
}

func removeInstrumentationScopeProcessor() map[string]any {
	return setInstrumentationScopeProcessor("", "")
}

func removeServiceAttributesProcessor() map[string]any {
	return map[string]any{
		"metric_statements": []any{
			map[string]any{
				"context":    "resource",
				"error_mode": "silent",
				"statements": []string{
					`delete_key(attributes, "service.name")`,
					`delete_key(attributes, "service.instance.id")`,
					`delete_key(attributes, "service.namespace")`,
					`delete_key(attributes, "service.version")`,
				},
			},
		},
	}
}

func metricsIncludeFilterProcessor(metricNames ...string) map[string]any {
	return metricsFilterProcessor("include", "strict", metricNames...)
}

func metricsExcludeFilterProcessor(metricNames ...string) map[string]any {
	return metricsFilterProcessor("exclude", "strict", metricNames...)
}

func metricsExcludeRegexpFilterProcessor(patterns ...string) map[string]any {
	regexps := make([]string, 0, len(patterns))
	for _, glob := range patterns {
		regexps = append(regexps, globToRegex(glob))
	}
	return metricsFilterProcessor("exclude", "regexp", regexps...)
}

func metricsFilterProcessor(polarity, matchType string, metricNames ...string) map[string]any {
	if metricNames == nil {
		metricNames = []string{}
	}
	return map[string]any{
		"metrics": map[string]any{
			polarity: map[string]any{
				"match_type":   matchType,
				"metric_names": metricNames,
			},
		},
	}
}

// globToRegex converts a metric glob pattern (using '*' wildcards) to an anchored regex pattern.
func globToRegex(glob string) string {
	parts := strings.Split(glob, "*")
	literals := make([]string, 0, len(parts))
	for _, p := range parts {
		literals = append(literals, regexp.QuoteMeta(p))
	}
	return fmt.Sprintf(`^%s$`, strings.Join(literals, `.*`))
}

func metricsDatapointFilterProcessor(expressions ...string) map[string]any {
	return map[string]any{
		"metrics": map[string]any{
			"datapoint": expressions,
		},
	}
}

func intervalProcessor(duration string) map[string]any {
	return map[string]any{
		"interval": duration,
	}
}

func cumulativeToDeltaProcessor(metrics ...string) map[string]any {
	return map[string]any{
		"include": map[string]any{
			"metrics":    metrics,
			"match_type": "strict",
		},
	}
}

func deltaToRateProcessor(metrics ...string) map[string]any {
	return map[string]any{
		"metrics": metrics,
	}
}

func gcpResourceDetectorProcessor(override bool) map[string]any {
	cfg := map[string]any{
		"detectors": []string{"gcp"},
	}
	if !override {
		cfg["override"] = false
	}
	return cfg
}

func metricStartTimeProcessor() map[string]any {
	return map[string]any{
		"strategy": "subtract_initial_point",
	}
}

func batchProcessor() map[string]any {
	return map[string]any{
		"send_batch_max_size": 200,
		"send_batch_size":     200,
		"timeout":             "200ms",
	}
}

func metricsTransformProcessor(transforms ...map[string]any) map[string]any {
	return map[string]any{
		"transforms": transforms,
	}
}

func renameMetric(oldName, newName string, operations ...map[string]any) map[string]any {
	out := map[string]any{
		"include":  oldName,
		"action":   "update",
		"new_name": newName,
	}
	if len(operations) > 0 {
		out["operations"] = operations
	}
	return out
}

func regexpRenameMetric(includeRegex, newName string) map[string]any {
	return map[string]any{
		"include":    includeRegex,
		"match_type": "regexp",
		"action":     "update",
		"new_name":   newName,
	}
}

func regexpUpdateMetric(includeRegex string, operations ...map[string]any) map[string]any {
	out := map[string]any{
		"include":    includeRegex,
		"match_type": "regexp",
		"action":     "update",
	}
	if len(operations) > 0 {
		out["operations"] = operations
	}
	return out
}

func updateMetric(metricName string, operations ...map[string]any) map[string]any {
	out := map[string]any{
		"include": metricName,
		"action":  "update",
	}
	if len(operations) > 0 {
		out["operations"] = operations
	}
	return out
}

func duplicateMetric(oldName, newName string, operations ...map[string]any) map[string]any {
	out := map[string]any{
		"include":  oldName,
		"action":   "insert",
		"new_name": newName,
	}
	if len(operations) > 0 {
		out["operations"] = operations
	}
	return out
}

func combineMetrics(includeRegex, newName string, operations ...map[string]any) map[string]any {
	out := map[string]any{
		"include":          includeRegex,
		"match_type":       "regexp",
		"action":           "combine",
		"new_name":         newName,
		"submatch_case":    "lower",
		"aggregation_type": "sum",
	}
	if len(operations) > 0 {
		out["operations"] = operations
	}
	return out
}

// addMetricPrefix adds a domain prefix to all metrics using regexp substitution.
func addMetricPrefix(prefix string) map[string]any {
	return regexpRenameMetric("^(.*)$", path.Join(prefix, "${1}"))
}

func toggleScalarDataType() map[string]any {
	return map[string]any{
		"action": "toggle_scalar_data_type",
	}
}

func scaleValue(factor float64) map[string]any {
	return map[string]any{
		"action":             "experimental_scale_value",
		"experimental_scale": factor,
	}
}

func addLabel(key, value string) map[string]any {
	return map[string]any{
		"action":    "add_label",
		"new_label": key,
		"new_value": value,
	}
}

func renameLabel(oldKey, newKey string) map[string]any {
	return map[string]any{
		"action":    "update_label",
		"label":     oldKey,
		"new_label": newKey,
	}
}

func deleteLabelValue(label, value string) map[string]any {
	return map[string]any{
		"action":      "delete_label_value",
		"label":       label,
		"label_value": value,
	}
}

func renameLabelValues(label string, transforms map[string]string) map[string]any {
	actions := make([]map[string]string, 0, len(transforms))
	for oldVal, newVal := range transforms {
		actions = append(actions, map[string]string{
			"value":     oldVal,
			"new_value": newVal,
		})
	}
	sort.Slice(actions, func(i, j int) bool {
		return actions[i]["value"] < actions[j]["value"]
	})
	return map[string]any{
		"action":        "update_label",
		"label":         label,
		"value_actions": actions,
	}
}

func aggregateLabels(aggregationType string, labels ...string) map[string]any {
	if labels == nil {
		labels = []string{}
	}
	return map[string]any{
		"action":           "aggregate_labels",
		"aggregation_type": aggregationType,
		"label_set":        labels,
	}
}

func aggregateLabelValues(aggregationType, label, newValue string, oldValues ...string) map[string]any {
	return map[string]any{
		"action":            "aggregate_label_values",
		"aggregation_type":  aggregationType,
		"label":             label,
		"new_value":         newValue,
		"aggregated_values": oldValues,
	}
}

// Keep these in sync:
// https://github.com/GoogleCloudPlatform/opentelemetry-operations-go/blob/main/exporter/collector/config.go#L158
var knownDomains = []string{"googleapis.com", "kubernetes.io", "istio.io", "knative.dev"}

func workloadMetricsProcessors(escapedID string) []namedProcessor {
	knownDomainsRegexEscaped := make([]string, 0, len(knownDomains))
	for _, d := range knownDomains {
		knownDomainsRegexEscaped = append(knownDomainsRegexEscaped, regexp.QuoteMeta(d))
	}
	return []namedProcessor{
		{
			id: fmt.Sprintf("metricstransform/%s_0", escapedID),
			config: metricsTransformProcessor(
				regexpRenameMetric(`^(.*)$`, `A${1}`),
				regexpRenameMetric(fmt.Sprintf(`^A((?:[a-z]+\.)*(?:%s)/.+)$`, strings.Join(knownDomainsRegexEscaped, "|")), `B${1}`),
				regexpRenameMetric(`^A(.*)$`, `Aworkload.googleapis.com/${1}`),
				regexpRenameMetric(`^[AB](.*)$`, `${1}`),
			),
		},
	}
}

func gmpMetricsProcessors(escapedID string) []namedProcessor {
	stmt := func(target, source string) string {
		return fmt.Sprintf(`set(%s, %s) where %s != nil and resource.attributes["cloud.platform"] == "gcp_compute_engine"`, target, source, source)
	}
	return []namedProcessor{
		{
			id:     fmt.Sprintf("resourcedetection/%s_0", escapedID),
			config: gcpResourceDetectorProcessor(false),
		},
		{
			id: fmt.Sprintf("transform/%s_1", escapedID),
			config: metricTransformIgnoreProcessor(
				"datapoint",
				stmt(`attributes["location"]`, `resource.attributes["cloud.availability_zone"]`),
				stmt(`attributes["namespace"]`, `Concat([resource.attributes["host.id"], resource.attributes["host.name"]], "/")`),
				stmt(`attributes["cluster"]`, `"__gce__"`),
				stmt(`attributes["instance_name"]`, `resource.attributes["host.name"]`),
				stmt(`attributes["machine_type"]`, `resource.attributes["host.type"]`),
			),
		},
		{
			id: fmt.Sprintf("groupbyattrs/%s_2", escapedID),
			config: map[string]any{
				"keys": []string{"namespace", "cluster", "location"},
			},
		},
		{
			id:     fmt.Sprintf("transform/%s_3", escapedID),
			config: metricUnknownCounterProcessor(),
		},
		{
			id: fmt.Sprintf("metricstransform/%s_4", escapedID),
			config: metricsTransformProcessor(
				addMetricPrefix("prometheus.googleapis.com"),
			),
		},
	}
}

func metricUnknownCounterProcessor() map[string]any {
	return metricTransformIgnoreProcessor(
		"metric",
		`copy_metric(Concat([metric.name, "unknowncounter"], ":")) where metric.metadata["prometheus.type"] == "unknown" and not HasSuffix(metric.name, ":unknowncounter")`,
		`convert_gauge_to_sum("cumulative", true) where HasSuffix(metric.name, ":unknowncounter")`,
		`set(metric.name, Substring(metric.name, 0, Len(metric.name)-Len(":unknowncounter"))) where HasSuffix(metric.name, ":unknowncounter")`,
	)
}
