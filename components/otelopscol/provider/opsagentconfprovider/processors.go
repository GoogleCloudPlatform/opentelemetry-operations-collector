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
	"path"
	"sort"
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

func removeInstrumentationScopeProcessor() map[string]any {
	return metricTransformProcessor(
		"scope",
		`set(name, "")`,
		`set(version, "")`,
	)
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
	return map[string]any{
		"metrics": map[string]any{
			"include": map[string]any{
				"match_type":   "strict",
				"metric_names": metricNames,
			},
		},
	}
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
	return map[string]any{
		"include":    "^(.*)$",
		"match_type": "regexp",
		"action":     "update",
		"new_name":   path.Join(prefix, "${1}"),
	}
}

func toggleScalarDataType() map[string]any {
	return map[string]any{
		"action": "toggle_scalar_data_type",
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
