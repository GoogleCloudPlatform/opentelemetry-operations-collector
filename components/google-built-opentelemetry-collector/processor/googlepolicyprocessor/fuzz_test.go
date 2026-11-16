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

package googlepolicyprocessor

import (
	"math"
	"testing"

	policyv1alpha1 "github.com/GoogleCloudPlatform/opentelemetry-operations-collector/gen/go/policy/v1alpha1"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"google.golang.org/protobuf/proto"
)

type indexedSlice[Element any] interface {
	Len() int
	At(int) Element
}

type sizedSlice interface {
	Len() int
}

// assertCountNeverIncreasesInvariant verifies that transformation never increases item counts.
func assertCountNeverIncreasesInvariant(t *testing.T, itemKind string, before, after int) {
	t.Helper()
	if after > before {
		t.Fatalf("invariant violated: %s count increased from %d to %d", itemKind, before, after)
	}
}

// assertNoEmptyContainersInvariant verifies that no empty Resource or Scope containers remain.
func assertNoEmptyContainersInvariant[
	Resource any,
	ResourceSlice indexedSlice[Resource],
	Scope any,
	ScopeSlice indexedSlice[Scope],
	ItemSlice sizedSlice,
](
	t *testing.T,
	resources ResourceSlice,
	scopesOf func(Resource) ScopeSlice,
	itemsOf func(Scope) ItemSlice,
) {
	t.Helper()
	for i := 0; i < resources.Len(); i++ {
		scopes := scopesOf(resources.At(i))
		if scopes.Len() == 0 {
			t.Fatalf("invariant violated: empty Resource container at index %d", i)
		}
		for j := 0; j < scopes.Len(); j++ {
			if itemsOf(scopes.At(j)).Len() == 0 {
				t.Fatalf("invariant violated: empty Scope container at index [%d][%d]", i, j)
			}
		}
	}
}

func buildFuzzLogs(body, attrKey, attrVal string, attrFloat float64) plog.Logs {
	ld := plog.NewLogs()

	// Resource 1: Fuzzed dynamic record + nested Map/Slice body record
	resourceLogs1 := ld.ResourceLogs().AppendEmpty()
	resourceLogs1.Resource().Attributes().PutStr("env", "prod")
	resourceLogs1.Resource().Attributes().PutStr(attrKey, attrVal)

	scopeLogs1 := resourceLogs1.ScopeLogs().AppendEmpty()
	scopeLogs1.Scope().SetName("fuzz.scope")

	logRecord1 := scopeLogs1.LogRecords().AppendEmpty()
	logRecord1.Body().SetStr(body)
	logRecord1.Attributes().PutStr(attrKey, attrVal)
	logRecord1.Attributes().PutStr("legacy_env", "staging")
	logRecord1.Attributes().PutDouble("float_val", attrFloat)

	logRecord2 := scopeLogs1.LogRecords().AppendEmpty()
	nestedBody := logRecord2.Body().SetEmptyMap()
	nestedBody.PutEmptyMap("event").PutStr("user", attrVal)
	nestedSlice := nestedBody.PutEmptySlice("tags")
	nestedSlice.AppendEmpty().SetStr(body)
	nestedSlice.AppendEmpty().SetEmptyMap().PutDouble("score", attrFloat)

	// Resource 2: Weird / edge-case values (NaN, +-Inf, nil/empty, empty maps/slices)
	resourceLogs2 := ld.ResourceLogs().AppendEmpty()
	scopeLogs2 := resourceLogs2.ScopeLogs().AppendEmpty()
	edgeLogRecord := scopeLogs2.LogRecords().AppendEmpty()
	edgeAttrs := edgeLogRecord.Attributes()
	edgeAttrs.PutDouble("nan_val", math.NaN())
	edgeAttrs.PutDouble("pos_inf", math.Inf(1))
	edgeAttrs.PutDouble("neg_inf", math.Inf(-1))
	edgeAttrs.PutEmpty("null_val")
	edgeAttrs.PutStr("", "")
	edgeAttrs.PutEmptyMap("empty_map")
	edgeAttrs.PutEmptySlice("empty_slice")

	return ld
}

func buildFuzzMetrics(metricName, attrKey, attrVal string, dpFloat float64) pmetric.Metrics {
	// Resource 1: All 5 standard metric types (Gauge, Sum, Histogram, ExponentialHistogram, Summary)
	md := newAllMetricTypes()

	// Resource 2: Fuzzed metric + weird/edge-case values and multiple datapoints
	resourceMetrics2 := md.ResourceMetrics().AppendEmpty()
	resourceMetrics2.Resource().Attributes().PutStr(attrKey, attrVal)
	scopeMetrics2 := resourceMetrics2.ScopeMetrics().AppendEmpty()

	// Untyped empty metric
	emptyMetric := scopeMetrics2.Metrics().AppendEmpty()
	emptyMetric.SetName("untyped.metric")

	// Multi-datapoint Gauge with fuzzed and weird values (NaN, +-Inf, empty)
	fuzzGauge := scopeMetrics2.Metrics().AppendEmpty()
	fuzzGauge.SetName(metricName)
	dataPoints := fuzzGauge.SetEmptyGauge().DataPoints()

	dp1 := dataPoints.AppendEmpty()
	dp1.SetDoubleValue(dpFloat)
	dp1.Attributes().PutStr("env", "staging")
	dp1.Attributes().PutStr(attrKey, attrVal)

	dp2 := dataPoints.AppendEmpty()
	dp2.SetDoubleValue(math.NaN())
	dp2.Attributes().PutStr("env", "prod")
	dp2.Attributes().PutDouble("pos_inf", math.Inf(1))
	dp2.Attributes().PutDouble("neg_inf", math.Inf(-1))
	dp2.Attributes().PutEmpty("null_val")
	dp2.Attributes().PutEmptyMap("empty_map")
	dp2.Attributes().PutEmptySlice("empty_slice")

	return md
}

func buildFuzzTraces(spanName, attrKey, attrVal string, attrFloat float64) ptrace.Traces {
	td := ptrace.NewTraces()

	// Resource 1: Fuzzed dynamic span
	resourceSpans1 := td.ResourceSpans().AppendEmpty()
	resourceSpans1.Resource().Attributes().PutStr(attrKey, attrVal)
	scopeSpans1 := resourceSpans1.ScopeSpans().AppendEmpty()
	span1 := scopeSpans1.Spans().AppendEmpty()
	span1.SetName(spanName)
	span1.SetTraceID(pcommon.TraceID([16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}))
	span1.Attributes().PutStr(attrKey, attrVal)
	span1.Attributes().PutDouble("float_val", attrFloat)

	// Resource 2: Weird / edge-case span (NaN, +-Inf, nested map/slice, empty TraceID)
	resourceSpans2 := td.ResourceSpans().AppendEmpty()
	scopeSpans2 := resourceSpans2.ScopeSpans().AppendEmpty()
	edgeSpan := scopeSpans2.Spans().AppendEmpty()
	edgeSpan.SetName("healthcheck")
	edgeAttrs := edgeSpan.Attributes()
	edgeAttrs.PutDouble("nan_val", math.NaN())
	edgeAttrs.PutDouble("pos_inf", math.Inf(1))
	edgeAttrs.PutDouble("neg_inf", math.Inf(-1))
	edgeAttrs.PutEmpty("null_val")
	edgeAttrs.PutEmptyMap("nested_map").PutStr(attrKey, attrVal)
	edgeAttrs.PutEmptySlice("nested_slice").AppendEmpty().SetStr(spanName)

	return td
}

// FuzzTransformLogs verifies that evaluating 1 to N log filter and transform
// policies through Evaluator.TransformLogs never panics and preserves container invariants.
func FuzzTransformLogs(f *testing.F) {
	seedCases := []struct {
		name                                     string
		setIdx                                   uint8
		matchVal, addVal, body, attrKey, attrVal string
		keep, upsert                             bool
		attrFloat                                float64
	}{
		{"1-policy-single-filter", 0, "redacted", "redacted", "redacted", "env", "prod", false, true, 1.5},
		{"2-policies-keep-overrides-drop", 1, "keep-me", "redacted", "keep-me", "env", "prod", true, true, 2.0},
		{"2-policies-filter-and-transform-pass2", 2, "redacted", "redacted", "hello", "k", "v", false, true, 0.0},
		{"2-policies-multi-transform-stage-order", 3, "staging", "prod", "hello", "env", "staging", false, true, -1.0},
		{"4-policies-all-combined-weird-values", 4, "", "", "", "", "", true, false, math.NaN()},
	}
	for _, tc := range seedCases {
		f.Add(tc.setIdx, tc.matchVal, tc.addVal, tc.body, tc.attrKey, tc.attrVal, tc.keep, tc.upsert, tc.attrFloat)
	}

	f.Fuzz(func(t *testing.T, setIdx uint8, matchVal, addVal, body, attrKey, attrVal string, keep, upsert bool, attrFloat float64) {
		dropBodyPolicy := newDropLogBodyProto("01-drop-body", matchVal)

		secondFilterAction := policyv1alpha1.Action_ACTION_DROP
		if keep {
			secondFilterAction = policyv1alpha1.Action_ACTION_KEEP
		}
		keepOrDropPolicy := newDropLogBodyProto("02-keep-or-drop-body", matchVal)
		keepOrDropPolicy.Action = secondFilterAction.Enum()

		setBodyPolicy := &policyv1alpha1.LogTransformPolicy{
			Id: "01-set-body",
			Additions: []*policyv1alpha1.LogAddition{
				{
					Target: &policyv1alpha1.LogTargetSelector{
						Target: &policyv1alpha1.LogTargetSelector_RecordField{
							RecordField: policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_BODY,
						},
					},
					Source: &policyv1alpha1.LogAddition_Value{
						Value: &policyv1alpha1.Value{Kind: &policyv1alpha1.Value_StringValue{StringValue: addVal}},
					},
					UpsertEnabled: proto.Bool(upsert),
				},
			},
		}
		renameEnvPolicy := &policyv1alpha1.LogTransformPolicy{
			Id: "02-rename-env",
			Renamings: []*policyv1alpha1.LogRenaming{
				{
					Source: &policyv1alpha1.LogAttributeSelector{
						Target: &policyv1alpha1.LogAttributeSelector_LogAttributePath{
							LogAttributePath: &policyv1alpha1.AttributePath{Segments: []string{"legacy_env"}},
						},
					},
					Target: &policyv1alpha1.LogAttributeSelector{
						Target: &policyv1alpha1.LogAttributeSelector_LogAttributePath{
							LogAttributePath: &policyv1alpha1.AttributePath{Segments: []string{"env"}},
						},
					},
					UpsertEnabled: proto.Bool(upsert),
				},
			},
		}

		// 1..N policy combinations (new combinations of any length can be appended here).
		policySets := [][]proto.Message{
			{dropBodyPolicy},
			{dropBodyPolicy, keepOrDropPolicy},
			{dropBodyPolicy, setBodyPolicy},
			{setBodyPolicy, renameEnvPolicy},
			{dropBodyPolicy, keepOrDropPolicy, setBodyPolicy, renameEnvPolicy},
		}
		protos := policySets[int(setIdx)%len(policySets)]

		ps, _ := googlepolicy.MakePolicySetFromProtos("fuzz-set", "fuzz-rev", protos)
		policies := ps.TransformationPolicies()
		if len(policies) == 0 {
			return
		}

		eval, err := NewEvaluator(policies)
		if err != nil || eval == nil {
			return
		}

		ld := buildFuzzLogs(body, attrKey, attrVal, attrFloat)
		initialCount := ld.LogRecordCount()

		eval.TransformLogs(ld)

		assertCountNeverIncreasesInvariant(t, "log record", initialCount, ld.LogRecordCount())
		assertNoEmptyContainersInvariant(t, ld.ResourceLogs(), plog.ResourceLogs.ScopeLogs, plog.ScopeLogs.LogRecords)
	})
}

// FuzzTransformMetrics verifies that evaluating 1 to N MetricFilterPolicies
// through Evaluator.TransformMetrics never panics and preserves container invariants.
func FuzzTransformMetrics(f *testing.F) {
	seedCases := []struct {
		name                                      string
		setIdx                                    uint8
		matchMetric, metricName, attrKey, attrVal string
		keepInst, keepDp                          bool
		dpFloat                                   float64
	}{
		{"1-policy-single-instrument-drop", 0, "gauge.metric", "gauge.metric", "env", "staging", false, false, 1.0},
		{"2-policies-instrument-keep-overrides-drop", 1, "gauge.metric", "gauge.metric", "env", "prod", true, false, 2.0},
		{"3-policies-instrument-and-datapoint-mix", 2, "gauge.metric", "fuzz.metric", "env", "staging", false, true, 3.0},
		{"4-policies-all-combined-weird-values", 3, "", "", "", "", true, true, math.NaN()},
	}
	for _, tc := range seedCases {
		f.Add(tc.setIdx, tc.matchMetric, tc.metricName, tc.attrKey, tc.attrVal, tc.keepInst, tc.keepDp, tc.dpFloat)
	}

	f.Fuzz(func(t *testing.T, setIdx uint8, matchMetric, metricName, attrKey, attrVal string, keepInst, keepDp bool, dpFloat float64) {
		dropInstPolicy := newDropMetricNameProto("01-drop-inst", matchMetric)

		keepInstPolicy := newDropMetricNameProto("02-keep-inst", matchMetric)
		if keepInst {
			keepInstPolicy.Action = policyv1alpha1.Action_ACTION_KEEP.Enum()
		}

		dropDpPolicy := newDropDatapointAttrProto("03-drop-dp", "env", "staging")
		keepDpPolicy := newDropDatapointAttrProto("04-keep-dp", attrKey, attrVal)
		if keepDp {
			keepDpPolicy.Action = policyv1alpha1.Action_ACTION_KEEP.Enum()
		}

		// 1..N policy combinations (new combinations of any length can be appended here).
		policySets := [][]proto.Message{
			{dropInstPolicy},
			{dropInstPolicy, keepInstPolicy},
			{dropInstPolicy, dropDpPolicy, keepDpPolicy},
			{dropInstPolicy, keepInstPolicy, dropDpPolicy, keepDpPolicy},
		}
		protos := policySets[int(setIdx)%len(policySets)]

		ps, _ := googlepolicy.MakePolicySetFromProtos("fuzz-set", "fuzz-rev", protos)
		policies := ps.TransformationPolicies()
		if len(policies) == 0 {
			return
		}

		eval, err := NewEvaluator(policies)
		if err != nil || eval == nil {
			return
		}

		md := buildFuzzMetrics(metricName, attrKey, attrVal, dpFloat)
		initialMetrics := md.MetricCount()
		initialDataPoints := md.DataPointCount()

		eval.TransformMetrics(md)

		assertCountNeverIncreasesInvariant(t, "metric", initialMetrics, md.MetricCount())
		assertCountNeverIncreasesInvariant(t, "datapoint", initialDataPoints, md.DataPointCount())
		assertNoEmptyContainersInvariant(t, md.ResourceMetrics(), pmetric.ResourceMetrics.ScopeMetrics, pmetric.ScopeMetrics.Metrics)
	})
}

// FuzzTransformTraces verifies that evaluating 1 to N trace filter and sampling
// policies through Evaluator.TransformTraces never panics and preserves container invariants.
func FuzzTransformTraces(f *testing.F) {
	seedCases := []struct {
		name                                  string
		setIdx                                uint8
		matchSpan, spanName, attrKey, attrVal string
		samplingPct, attrFloat                float64
	}{
		{"1-policy-single-trace-filter", 0, "drop-span", "drop-span", "env", "prod", 50.0, 1.0},
		{"2-policies-filter-and-sampling-combined", 1, "drop-span", "keep-span", "env", "prod", 50.0, 2.0},
		{"3-policies-multi-filter-and-sampling", 2, "drop-span", "keep-span", "k", "v", 10.0, 3.0},
		{"4-policies-all-combined-weird-values", 3, "", "", "", "", 100.0, math.NaN()},
	}
	for _, tc := range seedCases {
		f.Add(tc.setIdx, tc.matchSpan, tc.spanName, tc.attrKey, tc.attrVal, tc.samplingPct, tc.attrFloat)
	}

	f.Fuzz(func(t *testing.T, setIdx uint8, matchSpan, spanName, attrKey, attrVal string, samplingPct, attrFloat float64) {
		dropSpan1 := newDropSpanNameProto("01-drop-span", matchSpan)
		dropSpan2 := newDropSpanNameProto("02-drop-health", "healthcheck")
		sample1 := &policyv1alpha1.TraceSamplingPolicy{
			Id:              "03-sample-custom",
			SamplingPercent: samplingPct,
		}
		sample2 := &policyv1alpha1.TraceSamplingPolicy{
			Id:              "04-sample-50",
			SamplingPercent: 50.0,
		}

		// 1..N policy combinations (new combinations of any length can be appended here).
		policySets := [][]proto.Message{
			{dropSpan1},
			{dropSpan1, sample1},
			{dropSpan1, dropSpan2, sample1},
			{dropSpan1, dropSpan2, sample1, sample2},
		}
		protos := policySets[int(setIdx)%len(policySets)]

		ps, _ := googlepolicy.MakePolicySetFromProtos("fuzz-set", "fuzz-rev", protos)
		policies := ps.TransformationPolicies()
		if len(policies) == 0 {
			return
		}

		eval, err := NewEvaluator(policies)
		if err != nil || eval == nil {
			return
		}

		td := buildFuzzTraces(spanName, attrKey, attrVal, attrFloat)
		initialSpans := td.SpanCount()

		eval.TransformTraces(td)

		assertCountNeverIncreasesInvariant(t, "span", initialSpans, td.SpanCount())
		assertNoEmptyContainersInvariant(t, td.ResourceSpans(), ptrace.ResourceSpans.ScopeSpans, ptrace.ScopeSpans.Spans)
	})
}
