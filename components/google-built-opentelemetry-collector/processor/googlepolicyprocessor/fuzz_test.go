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
	"bytes"
	"fmt"
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

const policyChunkSep = "\n---\n"

// encodeSeedPolicies serializes a variable-length slice of 1..N policy protos
// into a single byte slice so seed cases can pass any number of policies.
func encodeSeedPolicies(f *testing.F, msgs []proto.Message) []byte {
	f.Helper()
	var chunks [][]byte
	for _, m := range msgs {
		var kind byte
		switch m.(type) {
		case *policyv1alpha1.LogFilterPolicy, *policyv1alpha1.MetricFilterPolicy, *policyv1alpha1.TraceFilterPolicy:
			kind = 0
		default:
			kind = 1
		}
		b, err := proto.Marshal(m)
		if err != nil {
			f.Fatalf("proto.Marshal seed failed: %v", err)
		}
		chunks = append(chunks, append([]byte{kind}, b...))
	}
	return bytes.Join(chunks, []byte(policyChunkSep))
}

// decodeFuzzPolicies splits fuzz bytes into 1..N policy chunks and unmarshals
// each chunk into a policy proto with a unique ID.
func decodeFuzzPolicies(data []byte, newPolicy func(kind byte, id string) proto.Message) []proto.Message {
	var protos []proto.Message
	for i, chunk := range bytes.Split(data, []byte(policyChunkSep)) {
		if len(chunk) == 0 {
			continue
		}
		id := fmt.Sprintf("fuzz-policy-%02d", i)
		pb := newPolicy(chunk[0], id)
		if proto.Unmarshal(chunk[1:], pb) == nil {
			// Ensure every policy in the 1..N list has a non-empty, unique ID.
			pb = newPolicyWithUniqueID(pb, id)
			protos = append(protos, pb)
		}
	}
	return protos
}

func newPolicyWithUniqueID(pb proto.Message, fallbackID string) proto.Message {
	switch p := pb.(type) {
	case *policyv1alpha1.LogFilterPolicy:
		if p.Id == "" {
			p.Id = fallbackID
		} else {
			p.Id = p.Id + "-" + fallbackID
		}
	case *policyv1alpha1.LogTransformPolicy:
		if p.Id == "" {
			p.Id = fallbackID
		} else {
			p.Id = p.Id + "-" + fallbackID
		}
	case *policyv1alpha1.MetricFilterPolicy:
		if p.Id == "" {
			p.Id = fallbackID
		} else {
			p.Id = p.Id + "-" + fallbackID
		}
	case *policyv1alpha1.TraceFilterPolicy:
		if p.Id == "" {
			p.Id = fallbackID
		} else {
			p.Id = p.Id + "-" + fallbackID
		}
	case *policyv1alpha1.TraceSamplingPolicy:
		if p.Id == "" {
			p.Id = fallbackID
		} else {
			p.Id = p.Id + "-" + fallbackID
		}
	}
	return pb
}

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
	dropBodyPolicy := newDropLogBodyProto("01-drop-body", "redacted")
	keepBodyPolicy := newDropLogBodyProto("02-keep-body", "keep-me")
	keepBodyPolicy.Action = policyv1alpha1.Action_ACTION_KEEP.Enum()

	setBodyRedactedPolicy := &policyv1alpha1.LogTransformPolicy{
		Id: "01-set-body",
		Additions: []*policyv1alpha1.LogAddition{
			{
				Target: &policyv1alpha1.LogTargetSelector{
					Target: &policyv1alpha1.LogTargetSelector_RecordField{
						RecordField: policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_BODY,
					},
				},
				Source: &policyv1alpha1.LogAddition_Value{
					Value: &policyv1alpha1.Value{Kind: &policyv1alpha1.Value_StringValue{StringValue: "redacted"}},
				},
				UpsertEnabled: proto.Bool(true),
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
				UpsertEnabled: proto.Bool(true),
			},
		},
	}

	seedCases := []struct {
		name                   string
		policies               []proto.Message
		body, attrKey, attrVal string
		attrFloat              float64
	}{
		{
			name:     "1-policy-single-filter",
			policies: []proto.Message{dropBodyPolicy},
			body:     "redacted", attrKey: "env", attrVal: "prod", attrFloat: 1.5,
		},
		{
			name:     "2-policies-keep-overrides-drop",
			policies: []proto.Message{dropBodyPolicy, keepBodyPolicy},
			body:     "keep-me", attrKey: "env", attrVal: "prod", attrFloat: 2.0,
		},
		{
			name:     "2-policies-filter-and-transform-pass2",
			policies: []proto.Message{dropBodyPolicy, setBodyRedactedPolicy},
			body:     "hello", attrKey: "k", attrVal: "v", attrFloat: 0.0,
		},
		{
			name:     "2-policies-multi-transform-stage-order",
			policies: []proto.Message{setBodyRedactedPolicy, renameEnvPolicy},
			body:     "hello", attrKey: "env", attrVal: "staging", attrFloat: -1.0,
		},
		{
			name:     "4-policies-all-combined-weird-values",
			policies: []proto.Message{dropBodyPolicy, keepBodyPolicy, setBodyRedactedPolicy, renameEnvPolicy},
			body:     "", attrKey: "", attrVal: "", attrFloat: math.NaN(),
		},
	}
	for _, tc := range seedCases {
		f.Add(encodeSeedPolicies(f, tc.policies), tc.body, tc.attrKey, tc.attrVal, tc.attrFloat)
	}

	f.Fuzz(func(t *testing.T, policiesData []byte, body, attrKey, attrVal string, attrFloat float64) {
		protos := decodeFuzzPolicies(policiesData, func(kind byte, _ string) proto.Message {
			if kind%2 == 0 {
				return &policyv1alpha1.LogFilterPolicy{}
			}
			return &policyv1alpha1.LogTransformPolicy{}
		})
		if len(protos) == 0 {
			return
		}

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
	dropInstPolicy := newDropMetricNameProto("01-drop-inst", "gauge.metric")
	keepInstPolicy := newDropMetricNameProto("02-keep-inst", "gauge.metric")
	keepInstPolicy.Action = policyv1alpha1.Action_ACTION_KEEP.Enum()

	dropDpPolicy := newDropDatapointAttrProto("03-drop-dp", "env", "staging")
	keepDpPolicy := newDropDatapointAttrProto("04-keep-dp", "env", "prod")
	keepDpPolicy.Action = policyv1alpha1.Action_ACTION_KEEP.Enum()

	seedCases := []struct {
		name                         string
		policies                     []proto.Message
		metricName, attrKey, attrVal string
		dpFloat                      float64
	}{
		{
			name:       "1-policy-single-instrument-drop",
			policies:   []proto.Message{dropInstPolicy},
			metricName: "gauge.metric", attrKey: "env", attrVal: "staging", dpFloat: 1.0,
		},
		{
			name:       "2-policies-instrument-keep-overrides-drop",
			policies:   []proto.Message{dropInstPolicy, keepInstPolicy},
			metricName: "gauge.metric", attrKey: "env", attrVal: "prod", dpFloat: 2.0,
		},
		{
			name:       "3-policies-instrument-and-datapoint-mix",
			policies:   []proto.Message{dropInstPolicy, dropDpPolicy, keepDpPolicy},
			metricName: "fuzz.metric", attrKey: "env", attrVal: "staging", dpFloat: 3.0,
		},
		{
			name:       "4-policies-all-combined-weird-values",
			policies:   []proto.Message{dropInstPolicy, keepInstPolicy, dropDpPolicy, keepDpPolicy},
			metricName: "", attrKey: "", attrVal: "", dpFloat: math.NaN(),
		},
	}
	for _, tc := range seedCases {
		f.Add(encodeSeedPolicies(f, tc.policies), tc.metricName, tc.attrKey, tc.attrVal, tc.dpFloat)
	}

	f.Fuzz(func(t *testing.T, policiesData []byte, metricName, attrKey, attrVal string, dpFloat float64) {
		protos := decodeFuzzPolicies(policiesData, func(_ byte, _ string) proto.Message {
			return &policyv1alpha1.MetricFilterPolicy{}
		})
		if len(protos) == 0 {
			return
		}

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
	dropSpan1 := newDropSpanNameProto("01-drop-span", "drop-span")
	dropSpan2 := newDropSpanNameProto("02-drop-health", "healthcheck")
	sample50 := &policyv1alpha1.TraceSamplingPolicy{
		Id:              "03-sample-50",
		SamplingPercent: 50.0,
	}
	sample10 := &policyv1alpha1.TraceSamplingPolicy{
		Id:              "04-sample-10",
		SamplingPercent: 10.0,
	}

	seedCases := []struct {
		name                       string
		policies                   []proto.Message
		spanName, attrKey, attrVal string
		attrFloat                  float64
	}{
		{
			name:     "1-policy-single-trace-filter",
			policies: []proto.Message{dropSpan1},
			spanName: "drop-span", attrKey: "env", attrVal: "prod", attrFloat: 1.0,
		},
		{
			name:     "2-policies-filter-and-sampling-combined",
			policies: []proto.Message{dropSpan1, sample50},
			spanName: "keep-span", attrKey: "env", attrVal: "prod", attrFloat: 2.0,
		},
		{
			name:     "3-policies-multi-filter-and-sampling",
			policies: []proto.Message{dropSpan1, dropSpan2, sample10},
			spanName: "keep-span", attrKey: "k", attrVal: "v", attrFloat: 3.0,
		},
		{
			name:     "4-policies-all-combined-weird-values",
			policies: []proto.Message{dropSpan1, dropSpan2, sample50, sample10},
			spanName: "", attrKey: "", attrVal: "", attrFloat: math.NaN(),
		},
	}
	for _, tc := range seedCases {
		f.Add(encodeSeedPolicies(f, tc.policies), tc.spanName, tc.attrKey, tc.attrVal, tc.attrFloat)
	}

	f.Fuzz(func(t *testing.T, policiesData []byte, spanName, attrKey, attrVal string, attrFloat float64) {
		protos := decodeFuzzPolicies(policiesData, func(kind byte, _ string) proto.Message {
			if kind%2 == 0 {
				return &policyv1alpha1.TraceFilterPolicy{}
			}
			return &policyv1alpha1.TraceSamplingPolicy{}
		})
		if len(protos) == 0 {
			return
		}

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
