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
	"slices"
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

func logTransformIgnoreProcessor(statements ...string) map[string]any {
	return map[string]any{
		"error_mode": "ignore",
		"log_statements": []any{
			map[string]any{
				"context":    "log",
				"statements": statements,
			},
		},
	}
}

func logFilterProcessor(expressions ...string) map[string]any {
	if expressions == nil {
		expressions = []string{}
	}
	return map[string]any{
		"error_mode": "ignore",
		"logs": map[string]any{
			"log_record": expressions,
		},
	}
}

// disableOtlpRoundTripProcessor prevents telemetry.googleapis.com from populating
// the LogEntry.otlp field by setting the gcp.use_legacy_mapping resource attribute to true.
func disableOtlpRoundTripProcessor() map[string]any {
	return map[string]any{
		"attributes": []map[string]any{
			{
				"key":    "gcp.use_legacy_mapping",
				"value":  "true",
				"action": "insert",
			},
		},
	}
}

// preserveInstrumentationScopeProcessor preserves instrumentation scope name and version
// in log record attributes when sending logs to telemetry.googleapis.com via OTLP.
func preserveInstrumentationScopeProcessor() map[string]any {
	return logTransformIgnoreProcessor(
		`set(attributes["instrumentation_source"], instrumentation_scope.name) where instrumentation_scope.name != ""`,
		`set(attributes["instrumentation_version"], instrumentation_scope.version) where instrumentation_scope.version != ""`,
	)
}

// copyServiceResourceLabelsProcessor copies service.* resource attributes into log record attributes
// when sending logs to telemetry.googleapis.com via OTLP.
func copyServiceResourceLabelsProcessor() map[string]any {
	return logTransformIgnoreProcessor(
		`set(attributes["service.name"], resource.attributes["service.name"]) where resource.attributes["service.name"] != nil`,
		`set(attributes["service.namespace"], resource.attributes["service.namespace"]) where resource.attributes["service.namespace"] != nil`,
		`set(attributes["service.instance.id"], resource.attributes["service.instance.id"]) where resource.attributes["service.instance.id"] != nil`,
	)
}

// setLogNameProcessor sets default gcp.log_name and compute.googleapis.com/resource_name
// attributes on log records when not already populated.
func setLogNameProcessor(logName, hostname string) map[string]any {
	return logTransformIgnoreProcessor(
		fmt.Sprintf(`set(attributes["compute.googleapis.com/resource_name"], %q) where attributes["compute.googleapis.com/resource_name"] == nil`, hostname),
		fmt.Sprintf(`set(attributes["gcp.log_name"], %q) where attributes["gcp.log_name"] == nil`, logName),
	)
}

func syslogTransformProcessor() map[string]any {
	return logTransformIgnoreProcessor(
		`set(cache["__body_string"], body) where IsString(body)`,
		`set(cache["__body_map"], body) where IsMap(body)`,
		`set(body, {})`,
		`merge_maps(body, cache["__body_map"], "upsert") where (cache != nil and cache["__body_map"] != nil)`,
		`set(body["message"], cache["__body_string"]) where (cache != nil and cache["__body_string"] != nil)`,
		`set(attributes, {})`,
		`delete_key(cache, "__body_map") where (cache != nil and cache["__body_map"] != nil)`,
		`delete_key(cache, "__body_string") where (cache != nil and cache["__body_string"] != nil)`,
	)
}

func fluentForwardTransformProcessor() map[string]any {
	return logTransformIgnoreProcessor(
		`set(cache["body_string"], body) where IsString(body)`,
		`set(body, {})`,
		`set(body["message"], cache["body_string"]) where (cache != nil and cache["body_string"] != nil)`,
		`merge_maps(body, attributes, "upsert") where attributes != nil`,
		`set(attributes, {})`,
	)
}

func fluentForwardSetLogNameProcessor() map[string]any {
	return logTransformIgnoreProcessor(
		`set(attributes["gcp.log_name"], Concat([attributes["gcp.log_name"], body["fluent.tag"]], ".")) where (attributes["gcp.log_name"] != nil and body["fluent.tag"] != nil)`,
		`delete_key(body, "fluent.tag") where body["fluent.tag"] != nil`,
	)
}

func journaldTransformProcessor() map[string]any {
	return logTransformIgnoreProcessor(
		`set(severity_text, "EMERGENCY") where body["PRIORITY"] == "0"`,
		`set(severity_text, "ALERT") where body["PRIORITY"] == "1"`,
		`set(severity_text, "CRITICAL") where body["PRIORITY"] == "2"`,
		`set(severity_text, "ERROR") where body["PRIORITY"] == "3"`,
		`set(severity_text, "WARNING") where body["PRIORITY"] == "4"`,
		`set(severity_text, "NOTICE") where body["PRIORITY"] == "5"`,
		`set(severity_text, "INFO") where body["PRIORITY"] == "6"`,
		`set(severity_text, "DEBUG") where body["PRIORITY"] == "7"`,
		`set(severity_number, 0) where IsMatch(body["PRIORITY"], "^[0-7]$")`,
		`set(attributes["gcp.source_location"]["file"], body["CODE_FILE"]) where body["CODE_FILE"] != nil`,
		`set(attributes["gcp.source_location"]["func"], body["CODE_FUNC"]) where body["CODE_FUNC"] != nil`,
		`set(attributes["gcp.source_location"]["line"], body["CODE_LINE"]) where body["CODE_LINE"] != nil`,
		`set(attributes["gcp.source_location"]["line"], Int(body["CODE_LINE"])) where body["CODE_LINE"] != nil and Int(body["CODE_LINE"]) != nil`,
	)
}

func windowsEventLogParseXMLProcessor(deleteOriginalField bool) map[string]any {
	logRecordOriginal := ottlLValue{"attributes", "log.record.original"}
	bodyParsedXML := ottlLValue{"body", "parsed_xml"}
	statements := newOTTLStatements(
		bodyParsedXML.SetIf(ottlParseSimplifiedXML(logRecordOriginal), logRecordOriginal.IsPresent()),
	)
	if deleteOriginalField {
		statements = statements.Append(logRecordOriginal.Delete())
	}
	return logTransformIgnoreProcessor(statements...)
}

func formatWindowsEventSystemTime(v ottlLValue) ottlStatements {
	return v.Set(ottlConcat([]ottlValue{
		ottlFormatTime(ottlToTime(v, "%Y-%m-%dT%T.%s%z"), "%Y-%m-%d %T.%s"),
		ottlStringLiteral("+0000"),
	}, " "))
}

func convertWindowsEventDataToStringInserts(v ottlLValue) ottlStatements {
	eventData := append(slices.Clone(v), "data")
	eventBinary := append(slices.Clone(v), "binary")
	cacheEventData := ottlLValue{"cache", "__event_data"}
	return newOTTLStatements(
		cacheEventData.SetIf(ottlToValues(eventData), eventData.IsPresent()),
		cacheEventData.AppendValuesIf(eventBinary, ottlAnd(cacheEventData.IsPresent(), eventBinary.IsPresent())),
		v.SetIf(cacheEventData, cacheEventData.IsPresent()),
		v.SetIf(ottlParseJSON(ottlStringLiteral("[]")), ottlNot(v.IsPresent())),
	)
}

func mustModifyFieldsTransformProcessor(fields map[string]*ModifyField) map[string]any {
	stmts, err := (LoggingProcessor{
		Type:      "modify_fields",
		EmptyBody: true,
		Fields:    fields,
	}).modifyFieldsStatements()
	if err != nil {
		panic(err)
	}
	return logTransformIgnoreProcessor(stmts...)
}

func windowsEventLogV1TransformProcessor() map[string]any {
	var empty string
	return mustModifyFieldsTransformProcessor(map[string]*ModifyField{
		"jsonPayload.Channel":      {CopyFrom: "jsonPayload.channel"},
		"jsonPayload.ComputerName": {CopyFrom: "jsonPayload.computer"},
		"jsonPayload.Data": {
			CopyFrom:     "jsonPayload.event_data.binary",
			DefaultValue: &empty,
			CustomConvertFunc: func(v ottlLValue) ottlStatements {
				return v.Set(ottlConvertCase(v, "lower"))
			},
		},
		"jsonPayload.EventCategory": {CopyFrom: "jsonPayload.parsed_xml.Event.System.Task", Type: "integer"},
		"jsonPayload.EventID":       {CopyFrom: "jsonPayload.event_id.id"},
		"jsonPayload.EventType": {
			CopyFrom: "jsonPayload.level",
			CustomConvertFunc: func(v ottlLValue) ottlStatements {
				keywords := ottlLValue{"cache", "body", "keywords"}
				return newOTTLStatements(
					v.SetIf(ottlStringLiteral("SuccessAudit"), ottlContainsValue(keywords, "Audit Success")),
					v.SetIf(ottlStringLiteral("FailureAudit"), ottlContainsValue(keywords, "Audit Failure")),
				)
			},
		},
		"jsonPayload.Message":      {CopyFrom: "jsonPayload.parsed_xml.Event.RenderingInfo.Message"},
		"jsonPayload.Qualifiers":   {CopyFrom: "jsonPayload.event_id.qualifiers"},
		"jsonPayload.RecordNumber": {CopyFrom: "jsonPayload.record_id"},
		"jsonPayload.Sid": {
			CopyFrom:     "jsonPayload.security.user_id",
			DefaultValue: &empty,
		},
		"jsonPayload.SourceName": {
			CopyFrom: "jsonPayload.provider.name",
			CustomConvertFunc: func(v ottlLValue) ottlStatements {
				// Prefer jsonPayload.provider.event_source if present and non-empty.
				eventSource := ottlLValue{"cache", "body", "provider", "event_source"}
				return v.SetIf(
					eventSource,
					ottlAnd(
						eventSource.IsPresent(),
						ottlNot(ottlEquals(eventSource, ottlStringLiteral(""))),
					),
				)
			},
		},
		"jsonPayload.StringInserts": {
			CopyFrom: "jsonPayload.event_data.data",
			CustomConvertFunc: func(v ottlLValue) ottlStatements {
				return newOTTLStatements(
					v.SetIf(ottlToValues(v), v.IsPresent()),
					v.SetIf(ottlParseJSON(ottlStringLiteral("[]")), ottlNot(v.IsPresent())),
				)
			},
		},
		"jsonPayload.TimeGenerated": {
			CopyFrom:          "jsonPayload.system_time",
			CustomConvertFunc: formatWindowsEventSystemTime,
		},
		"jsonPayload.TimeWritten": {
			CopyFrom:          "jsonPayload.system_time",
			CustomConvertFunc: formatWindowsEventSystemTime,
		},
	})
}

func windowsEventLogV2TransformProcessor() map[string]any {
	var empty string
	zero := "0"
	return mustModifyFieldsTransformProcessor(map[string]*ModifyField{
		"jsonPayload.Channel":       {CopyFrom: "jsonPayload.channel", DefaultValue: &empty},
		"jsonPayload.Computer":      {CopyFrom: "jsonPayload.computer", DefaultValue: &empty},
		"jsonPayload.EventID":       {CopyFrom: "jsonPayload.event_id.id", Type: "integer", DefaultValue: &zero},
		"jsonPayload.EventRecordID": {CopyFrom: "jsonPayload.record_id", Type: "integer", DefaultValue: &zero},
		"jsonPayload.Keywords":      {CopyFrom: "jsonPayload.parsed_xml.Event.System.Keywords"},
		"jsonPayload.Level":         {CopyFrom: "jsonPayload.parsed_xml.Event.System.Level", Type: "integer", DefaultValue: &zero},
		"jsonPayload.Message":       {CopyFrom: "jsonPayload.parsed_xml.Event.RenderingInfo.Message", DefaultValue: &empty},
		"jsonPayload.Opcode":        {CopyFrom: "jsonPayload.parsed_xml.Event.System.Opcode", Type: "integer", DefaultValue: &zero},
		"jsonPayload.ProcessID":     {CopyFrom: "jsonPayload.execution.process_id", Type: "integer", DefaultValue: &zero},
		"jsonPayload.ProviderGuid":  {CopyFrom: "jsonPayload.provider.guid", DefaultValue: &empty},
		"jsonPayload.ProviderName":  {CopyFrom: "jsonPayload.provider.name", DefaultValue: &empty},
		"jsonPayload.Qualifiers":    {CopyFrom: "jsonPayload.event_id.qualifiers", Type: "integer", DefaultValue: &zero},
		"jsonPayload.StringInserts": {
			CopyFrom:          "jsonPayload.event_data",
			CustomConvertFunc: convertWindowsEventDataToStringInserts,
		},
		"jsonPayload.Task":     {CopyFrom: "jsonPayload.parsed_xml.Event.System.Task", Type: "integer", DefaultValue: &zero},
		"jsonPayload.ThreadId": {CopyFrom: "jsonPayload.execution.thread_id", Type: "integer", DefaultValue: &zero},
		"jsonPayload.TimeCreated": {
			CopyFrom:          "jsonPayload.system_time",
			CustomConvertFunc: formatWindowsEventSystemTime,
		},
		"jsonPayload.UserId": {
			CopyFrom:     "jsonPayload.security.user_id",
			DefaultValue: &empty,
		},
		"jsonPayload.ActivityID":        {CopyFrom: "jsonPayload.correlation.activity_id", DefaultValue: &empty},
		"jsonPayload.RelatedActivityID": {CopyFrom: "jsonPayload.correlation.related_activity_id", DefaultValue: &empty},
		"jsonPayload.Version":           {CopyFrom: "jsonPayload.version", Type: "integer", DefaultValue: &zero},
	})
}

func windowsEventLogRawXMLTransformProcessor() map[string]any {
	var empty string
	return mustModifyFieldsTransformProcessor(map[string]*ModifyField{
		"jsonPayload.Message": {CopyFrom: "jsonPayload.parsed_xml.Event.RenderingInfo.Message", DefaultValue: &empty},
		"jsonPayload.raw_xml": {MoveFrom: `labels."log.record.original"`},
		"jsonPayload.StringInserts": {
			CopyFrom:          "jsonPayload.event_data",
			CustomConvertFunc: convertWindowsEventDataToStringInserts,
		},
	})
}
