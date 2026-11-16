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

func buildFuzzLogs(body, attrKey, attrVal string, attrFloat float64) plog.Logs {
	ld := plog.NewLogs()

	// Resource 1: Fuzzed dynamic record + nested Map/Slice body record
	rl1 := ld.ResourceLogs().AppendEmpty()
	rl1.Resource().Attributes().PutStr("env", "prod")
	rl1.Resource().Attributes().PutStr(attrKey, attrVal)

	sl1 := rl1.ScopeLogs().AppendEmpty()
	sl1.Scope().SetName("fuzz.scope")

	lr1 := sl1.LogRecords().AppendEmpty()
	lr1.Body().SetStr(body)
	lr1.Attributes().PutStr(attrKey, attrVal)
	lr1.Attributes().PutStr("legacy_env", "staging")
	lr1.Attributes().PutDouble("float_val", attrFloat)

	lr2 := sl1.LogRecords().AppendEmpty()
	nestedBody := lr2.Body().SetEmptyMap()
	nestedBody.PutEmptyMap("event").PutStr("user", attrVal)
	nestedSlice := nestedBody.PutEmptySlice("tags")
	nestedSlice.AppendEmpty().SetStr(body)
	nestedSlice.AppendEmpty().SetEmptyMap().PutDouble("score", attrFloat)

	// Resource 2: Weird / edge-case values (NaN, +-Inf, nil/empty, empty maps/slices)
	rl2 := ld.ResourceLogs().AppendEmpty()
	sl2 := rl2.ScopeLogs().AppendEmpty()
	edgeLR := sl2.LogRecords().AppendEmpty()
	edgeAttrs := edgeLR.Attributes()
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
	rm2 := md.ResourceMetrics().AppendEmpty()
	rm2.Resource().Attributes().PutStr(attrKey, attrVal)
	sm2 := rm2.ScopeMetrics().AppendEmpty()

	// Untyped empty metric
	emptyMetric := sm2.Metrics().AppendEmpty()
	emptyMetric.SetName("untyped.metric")

	// Multi-datapoint Gauge with fuzzed and weird values (NaN, +-Inf, empty)
	fuzzGauge := sm2.Metrics().AppendEmpty()
	fuzzGauge.SetName(metricName)
	dps := fuzzGauge.SetEmptyGauge().DataPoints()

	dp1 := dps.AppendEmpty()
	dp1.SetDoubleValue(dpFloat)
	dp1.Attributes().PutStr("env", "staging")
	dp1.Attributes().PutStr(attrKey, attrVal)

	dp2 := dps.AppendEmpty()
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
	rs1 := td.ResourceSpans().AppendEmpty()
	rs1.Resource().Attributes().PutStr(attrKey, attrVal)
	ss1 := rs1.ScopeSpans().AppendEmpty()
	span1 := ss1.Spans().AppendEmpty()
	span1.SetName(spanName)
	span1.SetTraceID(pcommon.TraceID([16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}))
	span1.Attributes().PutStr(attrKey, attrVal)
	span1.Attributes().PutDouble("float_val", attrFloat)

	// Resource 2: Weird / edge-case span (NaN, +-Inf, nested map/slice, empty TraceID)
	rs2 := td.ResourceSpans().AppendEmpty()
	ss2 := rs2.ScopeSpans().AppendEmpty()
	edgeSpan := ss2.Spans().AppendEmpty()
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

		// Invariant 1: Dropped records never return
		if ld.LogRecordCount() > initialCount {
			t.Fatalf("LogRecordCount increased from %d to %d", initialCount, ld.LogRecordCount())
		}
		// Invariant 2: No empty ResourceLogs or ScopeLogs left behind
		for i := 0; i < ld.ResourceLogs().Len(); i++ {
			rl := ld.ResourceLogs().At(i)
			if rl.ScopeLogs().Len() == 0 {
				t.Fatalf("empty ResourceLogs at index %d", i)
			}
			for j := 0; j < rl.ScopeLogs().Len(); j++ {
				if rl.ScopeLogs().At(j).LogRecords().Len() == 0 {
					t.Fatalf("empty ScopeLogs at index [%d][%d]", i, j)
				}
			}
		}
	})
}

// FuzzTransformMetrics verifies that evaluating 1 to N MetricFilterPolicies
// through Evaluator.TransformMetrics never panics and preserves container invariants.
func FuzzTransformMetrics(f *testing.F) {
	seedCases := []struct {
		name                                   string
		setIdx                                 uint8
		matchMetric, metricName, attrKey, attrVal string
		keepInst, keepDp                       bool
		dpFloat                                float64
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

		// Invariant 1: Dropped metrics/datapoints never return
		if md.MetricCount() > initialMetrics || md.DataPointCount() > initialDataPoints {
			t.Fatalf("metric/datapoint count increased: metrics %d->%d, dps %d->%d",
				initialMetrics, md.MetricCount(), initialDataPoints, md.DataPointCount())
		}
		// Invariant 2: No empty ResourceMetrics or ScopeMetrics left behind
		for i := 0; i < md.ResourceMetrics().Len(); i++ {
			rm := md.ResourceMetrics().At(i)
			if rm.ScopeMetrics().Len() == 0 {
				t.Fatalf("empty ResourceMetrics at index %d", i)
			}
			for j := 0; j < rm.ScopeMetrics().Len(); j++ {
				if rm.ScopeMetrics().At(j).Metrics().Len() == 0 {
					t.Fatalf("empty ScopeMetrics at index [%d][%d]", i, j)
				}
			}
		}
	})
}

// FuzzTransformTraces verifies that evaluating 1 to N trace filter and sampling
// policies through Evaluator.TransformTraces never panics and preserves container invariants.
func FuzzTransformTraces(f *testing.F) {
	seedCases := []struct {
		name                                     string
		setIdx                                   uint8
		matchSpan, spanName, attrKey, attrVal    string
		samplingPct, attrFloat                   float64
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

		// Invariant 1: Dropped spans never return
		if td.SpanCount() > initialSpans {
			t.Fatalf("SpanCount increased from %d to %d", initialSpans, td.SpanCount())
		}
		// Invariant 2: No empty ResourceSpans or ScopeSpans left behind
		for i := 0; i < td.ResourceSpans().Len(); i++ {
			rs := td.ResourceSpans().At(i)
			if rs.ScopeSpans().Len() == 0 {
				t.Fatalf("empty ResourceSpans at index %d", i)
			}
			for j := 0; j < rs.ScopeSpans().Len(); j++ {
				if rs.ScopeSpans().At(j).Spans().Len() == 0 {
					t.Fatalf("empty ScopeSpans at index [%d][%d]", i, j)
				}
			}
		}
	})
}
