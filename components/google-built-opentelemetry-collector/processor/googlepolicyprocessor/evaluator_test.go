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
	"testing"

	policyv1alpha1 "github.com/GoogleCloudPlatform/opentelemetry-operations-collector/gen/go/policy/v1alpha1"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"google.golang.org/protobuf/proto"
)

type testTransformationPolicy struct {
	name    string
	signals []googlepolicy.Signal
	pb      proto.Message
}

func (p *testTransformationPolicy) PolicyName() string { return p.name }
func (p *testTransformationPolicy) PolicyType() string { return "test_policy" }
func (p *testTransformationPolicy) PolicyClass() googlepolicy.PolicyClass {
	return googlepolicy.PolicyClassTransformation
}
func (p *testTransformationPolicy) Validate() error                      { return nil }
func (p *testTransformationPolicy) TargetSignals() []googlepolicy.Signal { return p.signals }
func (p *testTransformationPolicy) Proto() proto.Message                 { return p.pb }

func TestEvaluator_LogFiltering(t *testing.T) {
	// Policy 1: Drop logs where severity_text regex matches "^DEBUG"
	dropDebug := &testTransformationPolicy{
		name:    "drop-debug",
		signals: []googlepolicy.Signal{googlepolicy.SignalLogs},
		pb: &policyv1alpha1.LogFilterPolicy{
			Id:     "drop-debug",
			Action: policyv1alpha1.Action_ACTION_DROP.Enum(),
			Matches: []*policyv1alpha1.LogMatcher{
				{
					Target: &policyv1alpha1.LogFieldSelector{
						Target: &policyv1alpha1.LogFieldSelector_RecordField{
							RecordField: policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SEVERITY_TEXT,
						},
					},
					Predicate: &policyv1alpha1.LogMatcher_Regex{Regex: "^DEBUG"},
				},
			},
		},
	}

	// Policy 2: Keep logs (exemption) where resource attribute env == "production"
	keepProd := &testTransformationPolicy{
		name:    "keep-prod",
		signals: []googlepolicy.Signal{googlepolicy.SignalLogs},
		pb: &policyv1alpha1.LogFilterPolicy{
			Id:     "keep-prod",
			Action: policyv1alpha1.Action_ACTION_KEEP.Enum(),
			Matches: []*policyv1alpha1.LogMatcher{
				{
					Target: &policyv1alpha1.LogFieldSelector{
						Target: &policyv1alpha1.LogFieldSelector_ResourceAttribute{
							ResourceAttribute: &policyv1alpha1.AttributePath{Path: []string{"env"}},
						},
					},
					Predicate: &policyv1alpha1.LogMatcher_Equals{
						Equals: &policyv1alpha1.Value{
							Value: &policyv1alpha1.Value_StringValue{StringValue: "production"},
						},
					},
				},
			},
		},
	}

	ev, err := NewEvaluator([]googlepolicy.TransformationPolicy{dropDebug, keepProd})
	require.NoError(t, err)

	res := pcommon.NewResource()
	res.Attributes().PutStr("env", "staging")
	scope := pcommon.NewInstrumentationScope()
	scope.SetName("test.scope")

	// 1. INFO log in staging -> NOT dropped (no match, default allow)
	lr1 := plog.NewLogRecord()
	lr1.SetSeverityText("INFO")
	assert.False(t, ev.EvalLog(LogContext{Record: lr1, Resource: res, Scope: scope}))

	// 2. DEBUG log in staging -> DROPPED (matches dropDebug)
	lr2 := plog.NewLogRecord()
	lr2.SetSeverityText("DEBUG_VERBOSE")
	assert.True(t, ev.EvalLog(LogContext{Record: lr2, Resource: res, Scope: scope}))

	// 3. DEBUG log in production -> NOT dropped (keepProd overrides dropDebug)
	resProd := pcommon.NewResource()
	resProd.Attributes().PutStr("env", "production")
	assert.False(t, ev.EvalLog(LogContext{Record: lr2, Resource: resProd, Scope: scope}))

	// Test TransformLogs batch pruning
	ld := plog.NewLogs()
	rl := ld.ResourceLogs().AppendEmpty()
	rl.Resource().Attributes().PutStr("env", "staging")
	sl := rl.ScopeLogs().AppendEmpty()
	l1 := sl.LogRecords().AppendEmpty()
	l1.SetSeverityText("INFO")
	l2 := sl.LogRecords().AppendEmpty()
	l2.SetSeverityText("DEBUG_VERBOSE")

	ev.TransformLogs(ld)
	require.Equal(t, 1, ld.ResourceLogs().Len())
	require.Equal(t, 1, ld.ResourceLogs().At(0).ScopeLogs().Len())
	require.Equal(t, 1, ld.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().Len())
	assert.Equal(t, "INFO", ld.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0).SeverityText())
}

func TestEvaluator_LogNestedAttributeAndNegate(t *testing.T) {
	// Drop logs where http.request.method != "GET" (negated equals)
	dropNonGet := &testTransformationPolicy{
		name:    "drop-non-get",
		signals: []googlepolicy.Signal{googlepolicy.SignalLogs},
		pb: &policyv1alpha1.LogFilterPolicy{
			Id:     "drop-non-get",
			Action: policyv1alpha1.Action_ACTION_DROP.Enum(),
			Matches: []*policyv1alpha1.LogMatcher{
				{
					Target: &policyv1alpha1.LogFieldSelector{
						Target: &policyv1alpha1.LogFieldSelector_LogAttribute{
							LogAttribute: &policyv1alpha1.AttributePath{Path: []string{"http", "method"}},
						},
					},
					Predicate: &policyv1alpha1.LogMatcher_Equals{
						Equals: &policyv1alpha1.Value{
							Value: &policyv1alpha1.Value_StringValue{StringValue: "GET"},
						},
					},
					Negate: true,
				},
			},
		},
	}

	ev, err := NewEvaluator([]googlepolicy.TransformationPolicy{dropNonGet})
	require.NoError(t, err)

	res := pcommon.NewResource()
	scope := pcommon.NewInstrumentationScope()

	// 1. GET log -> NOT dropped (equals GET is true, negate is false -> does not match drop rule)
	lrGet := plog.NewLogRecord()
	httpMap := lrGet.Attributes().PutEmptyMap("http")
	httpMap.PutStr("method", "GET")
	assert.False(t, ev.EvalLog(LogContext{Record: lrGet, Resource: res, Scope: scope}))

	// 2. POST log -> DROPPED (equals GET is false, negate is true -> matches drop rule)
	lrPost := plog.NewLogRecord()
	httpMap2 := lrPost.Attributes().PutEmptyMap("http")
	httpMap2.PutStr("method", "POST")
	assert.True(t, ev.EvalLog(LogContext{Record: lrPost, Resource: res, Scope: scope}))
}

func TestEvaluator_MetricFiltering(t *testing.T) {
	// Drop metrics named "http.server.duration" with type "histogram"
	dropHist := &testTransformationPolicy{
		name:    "drop-duration-histogram",
		signals: []googlepolicy.Signal{googlepolicy.SignalMetrics},
		pb: &policyv1alpha1.MetricFilterPolicy{
			Id:     "drop-duration-histogram",
			Action: policyv1alpha1.Action_ACTION_DROP.Enum(),
			Matches: []*policyv1alpha1.MetricMatcher{
				{
					Target: &policyv1alpha1.MetricFieldSelector{
						Target: &policyv1alpha1.MetricFieldSelector_DescriptorField{
							DescriptorField: policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_NAME,
						},
					},
					Predicate: &policyv1alpha1.MetricMatcher_Equals{
						Equals: &policyv1alpha1.Value{
							Value: &policyv1alpha1.Value_StringValue{StringValue: "http.server.duration"},
						},
					},
				},
				{
					Target: &policyv1alpha1.MetricFieldSelector{
						Target: &policyv1alpha1.MetricFieldSelector_DescriptorField{
							DescriptorField: policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_TYPE,
						},
					},
					Predicate: &policyv1alpha1.MetricMatcher_Equals{
						Equals: &policyv1alpha1.Value{
							Value: &policyv1alpha1.Value_StringValue{StringValue: "HISTOGRAM"},
						},
					},
				},
			},
		},
	}

	ev, err := NewEvaluator([]googlepolicy.TransformationPolicy{dropHist})
	require.NoError(t, err)

	res := pcommon.NewResource()
	scope := pcommon.NewInstrumentationScope()
	dpAttrs := pcommon.NewMap()

	// Histogram metric matching name -> DROPPED
	mHist := pmetric.NewMetric()
	mHist.SetName("http.server.duration")
	mHist.SetEmptyHistogram()
	assert.True(t, ev.EvalMetric(MetricContext{
		Metric:                 mHist,
		DatapointAttributes:    dpAttrs,
		AggregationTemporality: pmetric.AggregationTemporalityCumulative,
		Resource:               res,
		Scope:                  scope,
	}))

	// Gauge metric with same name -> NOT dropped (different type)
	mGauge := pmetric.NewMetric()
	mGauge.SetName("http.server.duration")
	mGauge.SetEmptyGauge()
	assert.False(t, ev.EvalMetric(MetricContext{
		Metric:                 mGauge,
		DatapointAttributes:    dpAttrs,
		AggregationTemporality: pmetric.AggregationTemporalityUnspecified,
		Resource:               res,
		Scope:                  scope,
	}))

	// Other metric -> NOT dropped
	mOther := pmetric.NewMetric()
	mOther.SetName("system.cpu.load")
	mOther.SetEmptyGauge()
	assert.False(t, ev.EvalMetric(MetricContext{
		Metric:                 mOther,
		DatapointAttributes:    dpAttrs,
		AggregationTemporality: pmetric.AggregationTemporalityUnspecified,
		Resource:               res,
		Scope:                  scope,
	}))

	// Test TransformMetrics batch pruning
	md := pmetric.NewMetrics()
	rm := md.ResourceMetrics().AppendEmpty()
	sm := rm.ScopeMetrics().AppendEmpty()
	m1 := sm.Metrics().AppendEmpty()
	m1.SetName("http.server.duration")
	m1.SetEmptyHistogram().DataPoints().AppendEmpty()
	m2 := sm.Metrics().AppendEmpty()
	m2.SetName("system.cpu.load")
	m2.SetEmptyGauge().DataPoints().AppendEmpty()

	ev.TransformMetrics(md)
	require.Equal(t, 1, md.ResourceMetrics().Len())
	require.Equal(t, 1, md.ResourceMetrics().At(0).ScopeMetrics().Len())
	require.Equal(t, 1, md.ResourceMetrics().At(0).ScopeMetrics().At(0).Metrics().Len())
	assert.Equal(t, "system.cpu.load", md.ResourceMetrics().At(0).ScopeMetrics().At(0).Metrics().At(0).Name())
}

func TestEvaluator_TraceFiltering(t *testing.T) {
	// Drop internal spans with name starting with "healthcheck"
	dropHealthcheck := &testTransformationPolicy{
		name:    "drop-healthcheck",
		signals: []googlepolicy.Signal{googlepolicy.SignalTraces},
		pb: &policyv1alpha1.TraceFilterPolicy{
			Id:     "drop-healthcheck",
			Action: policyv1alpha1.Action_ACTION_DROP.Enum(),
			Matches: []*policyv1alpha1.TraceMatcher{
				{
					Target: &policyv1alpha1.TraceFieldSelector{
						Target: &policyv1alpha1.TraceFieldSelector_RecordField{
							RecordField: policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_NAME,
						},
					},
					Predicate: &policyv1alpha1.TraceMatcher_Regex{Regex: "^healthcheck"},
				},
				{
					Target: &policyv1alpha1.TraceFieldSelector{
						Target: &policyv1alpha1.TraceFieldSelector_RecordField{
							RecordField: policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_KIND,
						},
					},
					Predicate: &policyv1alpha1.TraceMatcher_Equals{
						Equals: &policyv1alpha1.Value{
							Value: &policyv1alpha1.Value_StringValue{StringValue: "INTERNAL"},
						},
					},
				},
			},
		},
	}

	ev, err := NewEvaluator([]googlepolicy.TransformationPolicy{dropHealthcheck})
	require.NoError(t, err)

	res := pcommon.NewResource()
	scope := pcommon.NewInstrumentationScope()

	// Internal healthcheck span -> DROPPED
	s1 := ptrace.NewSpan()
	s1.SetName("healthcheck.ping")
	s1.SetKind(ptrace.SpanKindInternal)
	assert.True(t, ev.EvalTrace(TraceContext{Span: s1, Resource: res, Scope: scope}))

	// Server healthcheck span -> NOT dropped (kind is SERVER, not INTERNAL)
	s2 := ptrace.NewSpan()
	s2.SetName("healthcheck.ping")
	s2.SetKind(ptrace.SpanKindServer)
	assert.False(t, ev.EvalTrace(TraceContext{Span: s2, Resource: res, Scope: scope}))

	// User request span -> NOT dropped
	s3 := ptrace.NewSpan()
	s3.SetName("get_user")
	s3.SetKind(ptrace.SpanKindInternal)
	assert.False(t, ev.EvalTrace(TraceContext{Span: s3, Resource: res, Scope: scope}))

	// Test TransformTraces batch pruning
	td := ptrace.NewTraces()
	rs := td.ResourceSpans().AppendEmpty()
	ss := rs.ScopeSpans().AppendEmpty()
	span1 := ss.Spans().AppendEmpty()
	span1.SetName("healthcheck.ping")
	span1.SetKind(ptrace.SpanKindInternal)
	span2 := ss.Spans().AppendEmpty()
	span2.SetName("get_user")
	span2.SetKind(ptrace.SpanKindInternal)

	ev.TransformTraces(td)
	require.Equal(t, 1, td.ResourceSpans().Len())
	require.Equal(t, 1, td.ResourceSpans().At(0).ScopeSpans().Len())
	require.Equal(t, 1, td.ResourceSpans().At(0).ScopeSpans().At(0).Spans().Len())
	assert.Equal(t, "get_user", td.ResourceSpans().At(0).ScopeSpans().At(0).Spans().At(0).Name())
}

// rawProtoLogPolicy tests NewEvaluator with a policy that does NOT implement ProtoPolicy
// (directly embeds *policyv1alpha1.LogFilterPolicy).
type rawProtoLogPolicy struct {
	*policyv1alpha1.LogFilterPolicy
}

func (p *rawProtoLogPolicy) PolicyName() string { return p.GetId() }
func (p *rawProtoLogPolicy) PolicyType() string { return "raw_log" }
func (p *rawProtoLogPolicy) PolicyClass() googlepolicy.PolicyClass {
	return googlepolicy.PolicyClassTransformation
}
func (p *rawProtoLogPolicy) Validate() error                      { return nil }
func (p *rawProtoLogPolicy) TargetSignals() []googlepolicy.Signal { return nil }

func TestEvaluator_PredicatesAndTypes(t *testing.T) {
	// Test compilePredicate errors and all predicate types
	_, err := compilePredicate(nil, false)
	assert.Error(t, err)

	_, err = compilePredicate(&policyv1alpha1.LogMatcher_Regex{Regex: "[invalid"}, false)
	assert.Error(t, err)
	_, err = compilePredicate(&policyv1alpha1.MetricMatcher_Regex{Regex: "[invalid"}, false)
	assert.Error(t, err)
	_, err = compilePredicate(&policyv1alpha1.TraceMatcher_Regex{Regex: "[invalid"}, false)
	assert.Error(t, err)

	// Exists predicate
	pExists, err := compilePredicate(&policyv1alpha1.LogMatcher_Exists{}, false)
	require.NoError(t, err)
	assert.True(t, pExists.evaluate("val", true))
	assert.False(t, pExists.evaluate("", false))

	// Regex on string, int64, int, bool
	pRegex, err := compilePredicate(&policyv1alpha1.MetricMatcher_Regex{Regex: "^(42|true|hello)$"}, false)
	require.NoError(t, err)
	assert.True(t, pRegex.evaluate("hello", true))
	assert.True(t, pRegex.evaluate(int64(42), true))
	assert.True(t, pRegex.evaluate(int(42), true))
	assert.True(t, pRegex.evaluate(true, true))
	assert.False(t, pRegex.evaluate(false, true))
	assert.False(t, pRegex.evaluate(99, true))

	// Regex on TraceMatcher
	pTraceRegex, err := compilePredicate(&policyv1alpha1.TraceMatcher_Regex{Regex: "^span"}, false)
	require.NoError(t, err)
	assert.True(t, pTraceRegex.evaluate("span-1", true))

	// Equals on all types (string, int, double, bool, bytes)
	pEqInt, _ := compilePredicate(&policyv1alpha1.LogMatcher_Equals{
		Equals: &policyv1alpha1.Value{Value: &policyv1alpha1.Value_IntValue{IntValue: 100}},
	}, false)
	assert.True(t, pEqInt.evaluate(int64(100), true))
	assert.True(t, pEqInt.evaluate(int(100), true))
	assert.True(t, pEqInt.evaluate(int32(100), true))
	assert.True(t, pEqInt.evaluate(uint64(100), true))
	assert.True(t, pEqInt.evaluate(uint32(100), true))
	assert.False(t, pEqInt.evaluate("not-an-int", true))

	pEqDouble, _ := compilePredicate(&policyv1alpha1.MetricMatcher_Equals{
		Equals: &policyv1alpha1.Value{Value: &policyv1alpha1.Value_DoubleValue{DoubleValue: 3.5}},
	}, false)
	assert.True(t, pEqDouble.evaluate(float64(3.5), true))
	assert.True(t, pEqDouble.evaluate(float32(3.5), true))
	assert.False(t, pEqDouble.evaluate("str", true))

	pEqBool, _ := compilePredicate(&policyv1alpha1.TraceMatcher_Equals{
		Equals: &policyv1alpha1.Value{Value: &policyv1alpha1.Value_BoolValue{BoolValue: true}},
	}, false)
	assert.True(t, pEqBool.evaluate(true, true))
	assert.False(t, pEqBool.evaluate(false, true))

	pEqBytes, _ := compilePredicate(&policyv1alpha1.LogMatcher_Equals{
		Equals: &policyv1alpha1.Value{Value: &policyv1alpha1.Value_BytesValue{BytesValue: []byte{1, 2, 3}}},
	}, false)
	assert.True(t, pEqBytes.evaluate([]byte{1, 2, 3}, true))
	assert.False(t, pEqBytes.evaluate([]byte{4, 5}, true))

	// Numeric comparisons: Gt, Gte, Lt, Lte across Log, Metric, Trace matchers
	pGtInt, _ := compilePredicate(&policyv1alpha1.LogMatcher_Gt{
		Gt: &policyv1alpha1.NumericValue{Value: &policyv1alpha1.NumericValue_IntValue{IntValue: 10}},
	}, false)
	assert.True(t, pGtInt.evaluate(int64(11), true))
	assert.False(t, pGtInt.evaluate(int64(10), true))

	pGtMetric, _ := compilePredicate(&policyv1alpha1.MetricMatcher_Gt{
		Gt: &policyv1alpha1.NumericValue{Value: &policyv1alpha1.NumericValue_DoubleValue{DoubleValue: 5.5}},
	}, false)
	assert.True(t, pGtMetric.evaluate(float64(6.0), true))

	pGtTrace, _ := compilePredicate(&policyv1alpha1.TraceMatcher_Gt{
		Gt: &policyv1alpha1.NumericValue{},
	}, false)
	assert.True(t, pGtTrace.evaluate(int(1), true))

	pGteLog, _ := compilePredicate(&policyv1alpha1.LogMatcher_Gte{
		Gte: &policyv1alpha1.NumericValue{Value: &policyv1alpha1.NumericValue_IntValue{IntValue: 10}},
	}, false)
	assert.True(t, pGteLog.evaluate(int32(10), true))

	pGteMetric, _ := compilePredicate(&policyv1alpha1.MetricMatcher_Gte{
		Gte: &policyv1alpha1.NumericValue{Value: &policyv1alpha1.NumericValue_IntValue{IntValue: 10}},
	}, false)
	assert.True(t, pGteMetric.evaluate(uint64(10), true))

	pGteTrace, _ := compilePredicate(&policyv1alpha1.TraceMatcher_Gte{
		Gte: &policyv1alpha1.NumericValue{Value: &policyv1alpha1.NumericValue_IntValue{IntValue: 10}},
	}, false)
	assert.True(t, pGteTrace.evaluate(uint32(10), true))

	pLtLog, _ := compilePredicate(&policyv1alpha1.LogMatcher_Lt{
		Lt: &policyv1alpha1.NumericValue{Value: &policyv1alpha1.NumericValue_DoubleValue{DoubleValue: 5.0}},
	}, false)
	assert.True(t, pLtLog.evaluate(float32(4.0), true))

	pLtMetric, _ := compilePredicate(&policyv1alpha1.MetricMatcher_Lt{
		Lt: &policyv1alpha1.NumericValue{Value: &policyv1alpha1.NumericValue_IntValue{IntValue: 5}},
	}, false)
	assert.True(t, pLtMetric.evaluate(int64(4), true))

	pLtTrace, _ := compilePredicate(&policyv1alpha1.TraceMatcher_Lt{
		Lt: &policyv1alpha1.NumericValue{Value: &policyv1alpha1.NumericValue_IntValue{IntValue: 5}},
	}, false)
	assert.True(t, pLtTrace.evaluate(int64(4), true))

	pLteLog, _ := compilePredicate(&policyv1alpha1.LogMatcher_Lte{
		Lte: &policyv1alpha1.NumericValue{Value: &policyv1alpha1.NumericValue_IntValue{IntValue: 5}},
	}, false)
	assert.True(t, pLteLog.evaluate(int64(5), true))

	pLteMetric, _ := compilePredicate(&policyv1alpha1.MetricMatcher_Lte{
		Lte: &policyv1alpha1.NumericValue{Value: &policyv1alpha1.NumericValue_IntValue{IntValue: 5}},
	}, false)
	assert.True(t, pLteMetric.evaluate(int64(5), true))

	pLteTrace, _ := compilePredicate(&policyv1alpha1.TraceMatcher_Lte{
		Lte: &policyv1alpha1.NumericValue{Value: &policyv1alpha1.NumericValue_IntValue{IntValue: 5}},
	}, false)
	assert.True(t, pLteTrace.evaluate(int64(5), true))

	// Contains on string, bytes, slice
	pContainsStr, _ := compilePredicate(&policyv1alpha1.LogMatcher_Contains{
		Contains: &policyv1alpha1.Value{Value: &policyv1alpha1.Value_StringValue{StringValue: "sub"}},
	}, false)
	assert.True(t, pContainsStr.evaluate("my_substring", true))
	assert.True(t, pContainsStr.evaluate([]any{"a", "sub", "b"}, true))
	assert.False(t, pContainsStr.evaluate([]any{"a", "b"}, true))
	assert.False(t, pContainsStr.evaluate(123, true))

	pContainsBytes, _ := compilePredicate(&policyv1alpha1.MetricMatcher_Contains{
		Contains: &policyv1alpha1.Value{Value: &policyv1alpha1.Value_BytesValue{BytesValue: []byte{2, 3}}},
	}, false)
	assert.True(t, pContainsBytes.evaluate([]byte{1, 2, 3, 4}, true))

	pContainsTrace, _ := compilePredicate(&policyv1alpha1.TraceMatcher_Contains{
		Contains: &policyv1alpha1.Value{Value: &policyv1alpha1.Value_IntValue{IntValue: 42}},
	}, false)
	assert.True(t, pContainsTrace.evaluate([]any{int64(10), int64(42)}, true))
}

func TestEvaluator_LookupPathAndValueTypes(t *testing.T) {
	m := pcommon.NewMap()
	m.PutStr("str", "val")
	m.PutInt("int", 123)
	m.PutDouble("double", 4.56)
	m.PutBool("bool", true)
	m.PutEmptyBytes("bytes").FromRaw([]byte{9, 8})
	subMap := m.PutEmptyMap("nested")
	subMap.PutStr("inner", "found")
	sl := m.PutEmptySlice("items")
	sl.AppendEmpty().SetStr("zero")
	sl.AppendEmpty().SetInt(10)
	mapInSlice := sl.AppendEmpty().SetEmptyMap()
	mapInSlice.PutStr("deep", "value")

	// Empty path
	val, ok := lookupPath(m, nil)
	assert.False(t, ok)
	assert.Nil(t, val)

	// Missing top-level key
	_, ok = lookupPath(m, []string{"missing"})
	assert.False(t, ok)

	// Scalar types
	v, ok := lookupPath(m, []string{"int"})
	assert.True(t, ok)
	assert.Equal(t, int64(123), v)

	v, ok = lookupPath(m, []string{"double"})
	assert.True(t, ok)
	assert.Equal(t, 4.56, v)

	v, ok = lookupPath(m, []string{"bool"})
	assert.True(t, ok)
	assert.Equal(t, true, v)

	v, ok = lookupPath(m, []string{"bytes"})
	assert.True(t, ok)
	assert.Equal(t, []byte{9, 8}, v)

	v, ok = lookupPath(m, []string{"items"})
	assert.True(t, ok)
	assert.IsType(t, []any{}, v)

	// Map fallback in pcommonValueToAny
	mapVal := pcommon.NewValueMap()
	mapVal.Map().PutStr("k", "v")
	assert.NotEmpty(t, pcommonValueToAny(mapVal))

	// Nested map lookup
	v, ok = lookupPath(m, []string{"nested", "inner"})
	assert.True(t, ok)
	assert.Equal(t, "found", v)

	// Missing nested map key
	_, ok = lookupPath(m, []string{"nested", "missing"})
	assert.False(t, ok)

	// Array/slice indexing (Review Issue #5)
	v, ok = lookupPath(m, []string{"items", "0"})
	assert.True(t, ok)
	assert.Equal(t, "zero", v)

	v, ok = lookupPath(m, []string{"items", "1"})
	assert.True(t, ok)
	assert.Equal(t, int64(10), v)

	// Nested map inside slice
	v, ok = lookupPath(m, []string{"items", "2", "deep"})
	assert.True(t, ok)
	assert.Equal(t, "value", v)

	// Out of bounds / negative / non-integer slice index
	_, ok = lookupPath(m, []string{"items", "99"})
	assert.False(t, ok)
	_, ok = lookupPath(m, []string{"items", "-1"})
	assert.False(t, ok)
	_, ok = lookupPath(m, []string{"items", "not_an_int"})
	assert.False(t, ok)

	// Attempt to traverse into a scalar
	_, ok = lookupPath(m, []string{"str", "deeper"})
	assert.False(t, ok)
}

func TestEvaluator_CompileErrorsAndValidation(t *testing.T) {
	// Nil policy in slice is ignored
	ev, err := NewEvaluator([]googlepolicy.TransformationPolicy{nil})
	require.NoError(t, err)
	assert.NotNil(t, ev)

	// Empty matchers validation (Review Issue #2)
	_, err = NewEvaluator([]googlepolicy.TransformationPolicy{
		&testTransformationPolicy{
			pb: &policyv1alpha1.LogFilterPolicy{
				Id:     "empty-log",
				Action: policyv1alpha1.Action_ACTION_DROP.Enum(),
			},
		},
	})
	assert.ErrorContains(t, err, "at least one matcher")

	_, err = NewEvaluator([]googlepolicy.TransformationPolicy{
		&testTransformationPolicy{
			pb: &policyv1alpha1.MetricFilterPolicy{
				Id:     "empty-metric",
				Action: policyv1alpha1.Action_ACTION_DROP.Enum(),
			},
		},
	})
	assert.ErrorContains(t, err, "at least one matcher")

	_, err = NewEvaluator([]googlepolicy.TransformationPolicy{
		&testTransformationPolicy{
			pb: &policyv1alpha1.TraceFilterPolicy{
				Id:     "empty-trace",
				Action: policyv1alpha1.Action_ACTION_DROP.Enum(),
			},
		},
	})
	assert.ErrorContains(t, err, "at least one matcher")

	// Unspecified action validation
	_, err = compileLogPolicy(&policyv1alpha1.LogFilterPolicy{
		Id:     "no-action",
		Action: policyv1alpha1.Action_ACTION_UNSPECIFIED.Enum(),
	})
	assert.ErrorContains(t, err, "action must be specified")

	_, err = compileMetricPolicy(&policyv1alpha1.MetricFilterPolicy{
		Id:     "no-action",
		Action: policyv1alpha1.Action_ACTION_UNSPECIFIED.Enum(),
	})
	assert.ErrorContains(t, err, "action must be specified")

	_, err = compileTracePolicy(&policyv1alpha1.TraceFilterPolicy{
		Id:     "no-action",
		Action: policyv1alpha1.Action_ACTION_UNSPECIFIED.Enum(),
	})
	assert.ErrorContains(t, err, "action must be specified")

	// Invalid predicate inside compile*Policy
	_, err = compileLogPolicy(&policyv1alpha1.LogFilterPolicy{
		Id:     "bad-regex",
		Action: policyv1alpha1.Action_ACTION_DROP.Enum(),
		Matches: []*policyv1alpha1.LogMatcher{
			{Predicate: &policyv1alpha1.LogMatcher_Regex{Regex: "[invalid"}},
		},
	})
	assert.Error(t, err)

	_, err = compileMetricPolicy(&policyv1alpha1.MetricFilterPolicy{
		Id:     "bad-regex",
		Action: policyv1alpha1.Action_ACTION_DROP.Enum(),
		Matches: []*policyv1alpha1.MetricMatcher{
			{Predicate: &policyv1alpha1.MetricMatcher_Regex{Regex: "[invalid"}},
		},
	})
	assert.Error(t, err)

	_, err = compileTracePolicy(&policyv1alpha1.TraceFilterPolicy{
		Id:     "bad-regex",
		Action: policyv1alpha1.Action_ACTION_DROP.Enum(),
		Matches: []*policyv1alpha1.TraceMatcher{
			{Predicate: &policyv1alpha1.TraceMatcher_Regex{Regex: "[invalid"}},
		},
	})
	assert.Error(t, err)
}

func TestEvaluator_LogAllTargetsAndBodyTypes(t *testing.T) {
	// Test empty evaluator on TransformLogs & EvalLog
	emptyEv := &Evaluator{}
	ld := plog.NewLogs()
	emptyEv.TransformLogs(ld)
	assert.False(t, emptyEv.EvalLog(LogContext{}))

	ctx := LogContext{
		Record:         plog.NewLogRecord(),
		Resource:       pcommon.NewResource(),
		Scope:          pcommon.NewInstrumentationScope(),
		ScopeSchemaURL: "https://opentelemetry.io/schemas/1.0.0",
	}

	// Nil target
	v, ok := extractLogTarget(ctx, nil)
	assert.False(t, ok)
	assert.Nil(t, v)

	// Unspecified target
	_, ok = extractLogTarget(ctx, &policyv1alpha1.LogFieldSelector{})
	assert.False(t, ok)

	// Body: empty vs string vs int (Review Issue #8)
	_, ok = extractLogTarget(ctx, &policyv1alpha1.LogFieldSelector{
		Target: &policyv1alpha1.LogFieldSelector_RecordField{
			RecordField: policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_BODY,
		},
	})
	assert.False(t, ok)

	ctx.Record.Body().SetStr("")
	v, ok = extractLogTarget(ctx, &policyv1alpha1.LogFieldSelector{
		Target: &policyv1alpha1.LogFieldSelector_RecordField{
			RecordField: policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_BODY,
		},
	})
	assert.False(t, ok)
	assert.Equal(t, "", v) // non-nil string allows equals: ""

	ctx.Record.Body().SetInt(999)
	v, ok = extractLogTarget(ctx, &policyv1alpha1.LogFieldSelector{
		Target: &policyv1alpha1.LogFieldSelector_RecordField{
			RecordField: policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_BODY,
		},
	})
	assert.True(t, ok)
	assert.Equal(t, int64(999), v)

	// SeverityNumber
	ctx.Record.SetSeverityNumber(plog.SeverityNumberInfo)
	v, ok = extractLogTarget(ctx, &policyv1alpha1.LogFieldSelector{
		Target: &policyv1alpha1.LogFieldSelector_RecordField{
			RecordField: policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SEVERITY_NUMBER,
		},
	})
	assert.True(t, ok)
	assert.Equal(t, int64(plog.SeverityNumberInfo), v)

	// TraceID and SpanID (empty vs populated)
	_, ok = extractLogTarget(ctx, &policyv1alpha1.LogFieldSelector{
		Target: &policyv1alpha1.LogFieldSelector_RecordField{
			RecordField: policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_TRACE_ID,
		},
	})
	assert.False(t, ok)

	_, ok = extractLogTarget(ctx, &policyv1alpha1.LogFieldSelector{
		Target: &policyv1alpha1.LogFieldSelector_RecordField{
			RecordField: policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SPAN_ID,
		},
	})
	assert.False(t, ok)

	ctx.Record.SetTraceID([16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16})
	ctx.Record.SetSpanID([8]byte{1, 2, 3, 4, 5, 6, 7, 8})
	v, ok = extractLogTarget(ctx, &policyv1alpha1.LogFieldSelector{
		Target: &policyv1alpha1.LogFieldSelector_RecordField{
			RecordField: policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_TRACE_ID,
		},
	})
	assert.True(t, ok)
	assert.NotEmpty(t, v)

	v, ok = extractLogTarget(ctx, &policyv1alpha1.LogFieldSelector{
		Target: &policyv1alpha1.LogFieldSelector_RecordField{
			RecordField: policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SPAN_ID,
		},
	})
	assert.True(t, ok)
	assert.NotEmpty(t, v)

	// Unspecified RecordField
	_, ok = extractLogTarget(ctx, &policyv1alpha1.LogFieldSelector{
		Target: &policyv1alpha1.LogFieldSelector_RecordField{
			RecordField: policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_UNSPECIFIED,
		},
	})
	assert.False(t, ok)

	// Scope attributes and Scope fields
	ctx.Scope.Attributes().PutStr("sa", "sval")
	ctx.Scope.SetName("my.scope")
	ctx.Scope.SetVersion("v1.2")

	v, ok = extractLogTarget(ctx, &policyv1alpha1.LogFieldSelector{
		Target: &policyv1alpha1.LogFieldSelector_ScopeAttribute{
			ScopeAttribute: &policyv1alpha1.AttributePath{Path: []string{"sa"}},
		},
	})
	assert.True(t, ok)
	assert.Equal(t, "sval", v)

	v, ok = extractLogTarget(ctx, &policyv1alpha1.LogFieldSelector{
		Target: &policyv1alpha1.LogFieldSelector_ScopeField{
			ScopeField: policyv1alpha1.ScopeField_SCOPE_FIELD_NAME,
		},
	})
	assert.True(t, ok)
	assert.Equal(t, "my.scope", v)

	v, ok = extractLogTarget(ctx, &policyv1alpha1.LogFieldSelector{
		Target: &policyv1alpha1.LogFieldSelector_ScopeField{
			ScopeField: policyv1alpha1.ScopeField_SCOPE_FIELD_VERSION,
		},
	})
	assert.True(t, ok)
	assert.Equal(t, "v1.2", v)

	v, ok = extractLogTarget(ctx, &policyv1alpha1.LogFieldSelector{
		Target: &policyv1alpha1.LogFieldSelector_ScopeField{
			ScopeField: policyv1alpha1.ScopeField_SCOPE_FIELD_SCHEMA_URL,
		},
	})
	assert.True(t, ok)
	assert.Equal(t, "https://opentelemetry.io/schemas/1.0.0", v)

	_, ok = extractLogTarget(ctx, &policyv1alpha1.LogFieldSelector{
		Target: &policyv1alpha1.LogFieldSelector_ScopeField{
			ScopeField: policyv1alpha1.ScopeField_SCOPE_FIELD_UNSPECIFIED,
		},
	})
	assert.False(t, ok)
}

func TestEvaluator_MetricAllTargetsAndDatapointEvaluation(t *testing.T) {
	emptyEv := &Evaluator{}
	emptyEv.TransformMetrics(pmetric.NewMetrics())
	assert.False(t, emptyEv.EvalMetric(MetricContext{}))

	// Check enum string helpers directly
	assert.Equal(t, "GAUGE", metricTypeToString(pmetric.MetricTypeGauge))
	assert.Equal(t, "SUM", metricTypeToString(pmetric.MetricTypeSum))
	assert.Equal(t, "HISTOGRAM", metricTypeToString(pmetric.MetricTypeHistogram))
	assert.Equal(t, "EXPONENTIAL_HISTOGRAM", metricTypeToString(pmetric.MetricTypeExponentialHistogram))
	assert.Equal(t, "SUMMARY", metricTypeToString(pmetric.MetricTypeSummary))
	assert.Equal(t, "UNSPECIFIED", metricTypeToString(pmetric.MetricTypeEmpty))

	assert.Equal(t, "DELTA", temporalityToString(pmetric.AggregationTemporalityDelta))
	assert.Equal(t, "CUMULATIVE", temporalityToString(pmetric.AggregationTemporalityCumulative))
	assert.Equal(t, "UNSPECIFIED", temporalityToString(pmetric.AggregationTemporalityUnspecified))

	// Test extractMetricTarget fields (Description, Unit, Temporality, IsMonotonic, Scope)
	mSum := pmetric.NewMetric()
	mSum.SetName("req.count")
	mSum.SetDescription("Total requests")
	mSum.SetUnit("1")
	sum := mSum.SetEmptySum()
	sum.SetIsMonotonic(true)
	sum.SetAggregationTemporality(pmetric.AggregationTemporalityDelta)

	ctx := MetricContext{
		Metric:                 mSum,
		DatapointAttributes:    pcommon.NewMap(),
		AggregationTemporality: pmetric.AggregationTemporalityDelta,
		Resource:               pcommon.NewResource(),
		Scope:                  pcommon.NewInstrumentationScope(),
		ScopeSchemaURL:         "https://schema.test",
	}
	ctx.DatapointAttributes.PutStr("dp_key", "dp_val")
	ctx.Resource.Attributes().PutStr("res_key", "res_val")
	ctx.Scope.Attributes().PutStr("scope_key", "scope_val")
	ctx.Scope.SetName("scope.name")
	ctx.Scope.SetVersion("1.0")

	_, ok := extractMetricTarget(ctx, nil)
	assert.False(t, ok)
	_, ok = extractMetricTarget(ctx, &policyv1alpha1.MetricFieldSelector{})
	assert.False(t, ok)

	v, ok := extractMetricTarget(ctx, &policyv1alpha1.MetricFieldSelector{
		Target: &policyv1alpha1.MetricFieldSelector_DescriptorField{
			DescriptorField: policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_DESCRIPTION,
		},
	})
	assert.True(t, ok)
	assert.Equal(t, "Total requests", v)

	v, ok = extractMetricTarget(ctx, &policyv1alpha1.MetricFieldSelector{
		Target: &policyv1alpha1.MetricFieldSelector_DescriptorField{
			DescriptorField: policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_UNIT,
		},
	})
	assert.True(t, ok)
	assert.Equal(t, "1", v)

	v, ok = extractMetricTarget(ctx, &policyv1alpha1.MetricFieldSelector{
		Target: &policyv1alpha1.MetricFieldSelector_DescriptorField{
			DescriptorField: policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_AGGREGATION_TEMPORALITY,
		},
	})
	assert.True(t, ok)
	assert.Equal(t, "DELTA", v)

	// Unspecified temporality returns nil, false (Review Issue #10)
	ctxUnspecifiedTemp := ctx
	ctxUnspecifiedTemp.AggregationTemporality = pmetric.AggregationTemporalityUnspecified
	_, ok = extractMetricTarget(ctxUnspecifiedTemp, &policyv1alpha1.MetricFieldSelector{
		Target: &policyv1alpha1.MetricFieldSelector_DescriptorField{
			DescriptorField: policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_AGGREGATION_TEMPORALITY,
		},
	})
	assert.False(t, ok)

	// IsMonotonic on Sum vs Gauge (Review Issue #4)
	v, ok = extractMetricTarget(ctx, &policyv1alpha1.MetricFieldSelector{
		Target: &policyv1alpha1.MetricFieldSelector_DescriptorField{
			DescriptorField: policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_IS_MONOTONIC,
		},
	})
	assert.True(t, ok)
	assert.Equal(t, true, v)

	mGauge := pmetric.NewMetric()
	mGauge.SetEmptyGauge()
	ctxGauge := ctx
	ctxGauge.Metric = mGauge
	_, ok = extractMetricTarget(ctxGauge, &policyv1alpha1.MetricFieldSelector{
		Target: &policyv1alpha1.MetricFieldSelector_DescriptorField{
			DescriptorField: policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_IS_MONOTONIC,
		},
	})
	assert.False(t, ok)

	// Unspecified DescriptorField
	_, ok = extractMetricTarget(ctx, &policyv1alpha1.MetricFieldSelector{
		Target: &policyv1alpha1.MetricFieldSelector_DescriptorField{
			DescriptorField: policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_UNSPECIFIED,
		},
	})
	assert.False(t, ok)

	// Attributes & Scope fields
	v, ok = extractMetricTarget(ctx, &policyv1alpha1.MetricFieldSelector{
		Target: &policyv1alpha1.MetricFieldSelector_DatapointAttribute{
			DatapointAttribute: &policyv1alpha1.AttributePath{Path: []string{"dp_key"}},
		},
	})
	assert.True(t, ok)
	assert.Equal(t, "dp_val", v)

	v, ok = extractMetricTarget(ctx, &policyv1alpha1.MetricFieldSelector{
		Target: &policyv1alpha1.MetricFieldSelector_ResourceAttribute{
			ResourceAttribute: &policyv1alpha1.AttributePath{Path: []string{"res_key"}},
		},
	})
	assert.True(t, ok)
	assert.Equal(t, "res_val", v)

	v, ok = extractMetricTarget(ctx, &policyv1alpha1.MetricFieldSelector{
		Target: &policyv1alpha1.MetricFieldSelector_ScopeAttribute{
			ScopeAttribute: &policyv1alpha1.AttributePath{Path: []string{"scope_key"}},
		},
	})
	assert.True(t, ok)
	assert.Equal(t, "scope_val", v)

	v, ok = extractMetricTarget(ctx, &policyv1alpha1.MetricFieldSelector{
		Target: &policyv1alpha1.MetricFieldSelector_ScopeField{
			ScopeField: policyv1alpha1.ScopeField_SCOPE_FIELD_NAME,
		},
	})
	assert.True(t, ok)
	assert.Equal(t, "scope.name", v)

	v, ok = extractMetricTarget(ctx, &policyv1alpha1.MetricFieldSelector{
		Target: &policyv1alpha1.MetricFieldSelector_ScopeField{
			ScopeField: policyv1alpha1.ScopeField_SCOPE_FIELD_VERSION,
		},
	})
	assert.True(t, ok)
	assert.Equal(t, "1.0", v)

	v, ok = extractMetricTarget(ctx, &policyv1alpha1.MetricFieldSelector{
		Target: &policyv1alpha1.MetricFieldSelector_ScopeField{
			ScopeField: policyv1alpha1.ScopeField_SCOPE_FIELD_SCHEMA_URL,
		},
	})
	assert.True(t, ok)
	assert.Equal(t, "https://schema.test", v)

	_, ok = extractMetricTarget(ctx, &policyv1alpha1.MetricFieldSelector{
		Target: &policyv1alpha1.MetricFieldSelector_ScopeField{
			ScopeField: policyv1alpha1.ScopeField_SCOPE_FIELD_UNSPECIFIED,
		},
	})
	assert.False(t, ok)
}

func TestEvaluator_MetricDatapointPruningAndEmptyMetrics(t *testing.T) {
	// Policy 1: Datapoint-level DROP where datapoint attribute "drop_me" == "true"
	dropDpPolicy := &testTransformationPolicy{
		name: "drop-dp",
		pb: &policyv1alpha1.MetricFilterPolicy{
			Id:     "drop-dp",
			Action: policyv1alpha1.Action_ACTION_DROP.Enum(),
			Matches: []*policyv1alpha1.MetricMatcher{
				{
					Target: &policyv1alpha1.MetricFieldSelector{
						Target: &policyv1alpha1.MetricFieldSelector_DatapointAttribute{
							DatapointAttribute: &policyv1alpha1.AttributePath{Path: []string{"drop_me"}},
						},
					},
					Predicate: &policyv1alpha1.MetricMatcher_Equals{
						Equals: &policyv1alpha1.Value{Value: &policyv1alpha1.Value_StringValue{StringValue: "true"}},
					},
				},
			},
		},
	}

	// Policy 2: Instrument-level KEEP where metric name == "exempt.metric"
	keepInstrumentPolicy := &testTransformationPolicy{
		name: "keep-exempt",
		pb: &policyv1alpha1.MetricFilterPolicy{
			Id:     "keep-exempt",
			Action: policyv1alpha1.Action_ACTION_KEEP.Enum(),
			Matches: []*policyv1alpha1.MetricMatcher{
				{
					Target: &policyv1alpha1.MetricFieldSelector{
						Target: &policyv1alpha1.MetricFieldSelector_DescriptorField{
							DescriptorField: policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_NAME,
						},
					},
					Predicate: &policyv1alpha1.MetricMatcher_Equals{
						Equals: &policyv1alpha1.Value{Value: &policyv1alpha1.Value_StringValue{StringValue: "exempt.metric"}},
					},
				},
			},
		},
	}

	ev, err := NewEvaluator([]googlepolicy.TransformationPolicy{dropDpPolicy, keepInstrumentPolicy})
	require.NoError(t, err)

	md := pmetric.NewMetrics()
	rm := md.ResourceMetrics().AppendEmpty()
	sm := rm.ScopeMetrics().AppendEmpty()

	// 1. Metric with 0 datapoints (Review Issue #1): should NOT be dropped by datapoint policy!
	mEmptyGauge := sm.Metrics().AppendEmpty()
	mEmptyGauge.SetName("empty.gauge")
	mEmptyGauge.SetEmptyGauge()

	// 2. Sum metric with 2 datapoints: 1 dropped, 1 kept
	mSum := sm.Metrics().AppendEmpty()
	mSum.SetName("partial.sum")
	dp1 := mSum.SetEmptySum().DataPoints().AppendEmpty()
	dp1.Attributes().PutStr("drop_me", "true")
	dp2 := mSum.Sum().DataPoints().AppendEmpty()
	dp2.Attributes().PutStr("drop_me", "false")

	// 3. Histogram metric where ALL datapoints are dropped -> entire metric pruned
	mHist := sm.Metrics().AppendEmpty()
	mHist.SetName("pruned.hist")
	hdp := mHist.SetEmptyHistogram().DataPoints().AppendEmpty()
	hdp.Attributes().PutStr("drop_me", "true")

	// 4. ExponentialHistogram metric with 1 kept datapoint
	mExpHist := sm.Metrics().AppendEmpty()
	mExpHist.SetName("kept.exphist")
	ehdp := mExpHist.SetEmptyExponentialHistogram().DataPoints().AppendEmpty()
	ehdp.Attributes().PutStr("drop_me", "false")

	// 5. Summary metric with 1 kept datapoint
	mSummary := sm.Metrics().AppendEmpty()
	mSummary.SetName("kept.summary")
	sdp := mSummary.SetEmptySummary().DataPoints().AppendEmpty()
	sdp.Attributes().PutStr("drop_me", "false")

	// 6. Exempt metric (instrument KEEP) with drop_me="true" -> KEPT via short-circuit!
	mExempt := sm.Metrics().AppendEmpty()
	mExempt.SetName("exempt.metric")
	edp := mExempt.SetEmptyGauge().DataPoints().AppendEmpty()
	edp.Attributes().PutStr("drop_me", "true")

	// 7. Unset metric type (MetricTypeEmpty) -> not dropped
	mUnset := sm.Metrics().AppendEmpty()
	mUnset.SetName("unset.type")

	// 8. Empty metric matching KEEP policy -> evalMetricInstrumentOnly KEEP branch
	mEmptyExempt := sm.Metrics().AppendEmpty()
	mEmptyExempt.SetName("exempt.metric")
	mEmptyExempt.SetEmptyGauge()

	// 9. Direct EvalMetric with KEEP policy matching
	assert.False(t, ev.EvalMetric(MetricContext{
		Metric: mEmptyExempt,
	}))

	ev.TransformMetrics(md)

	// pruned.hist should be removed; the other 7 metrics should remain
	metrics := md.ResourceMetrics().At(0).ScopeMetrics().At(0).Metrics()
	require.Equal(t, 7, metrics.Len())
	assert.Equal(t, "empty.gauge", metrics.At(0).Name())
	assert.Equal(t, 0, metrics.At(0).Gauge().DataPoints().Len())
	assert.Equal(t, "partial.sum", metrics.At(1).Name())
	assert.Equal(t, 1, metrics.At(1).Sum().DataPoints().Len())
	assert.Equal(t, "exempt.metric", metrics.At(4).Name())
	assert.Equal(t, 1, metrics.At(4).Gauge().DataPoints().Len())
}

func TestEvaluator_TraceAllTargetsAndRootSpanParentID(t *testing.T) {
	emptyEv := &Evaluator{}
	emptyEv.TransformTraces(ptrace.NewTraces())
	assert.False(t, emptyEv.EvalTrace(TraceContext{}))

	// Check enum string helpers
	assert.Equal(t, "INTERNAL", spanKindToString(ptrace.SpanKindInternal))
	assert.Equal(t, "SERVER", spanKindToString(ptrace.SpanKindServer))
	assert.Equal(t, "CLIENT", spanKindToString(ptrace.SpanKindClient))
	assert.Equal(t, "PRODUCER", spanKindToString(ptrace.SpanKindProducer))
	assert.Equal(t, "CONSUMER", spanKindToString(ptrace.SpanKindConsumer))
	assert.Equal(t, "INTERNAL", spanKindToString(ptrace.SpanKindUnspecified))

	assert.Equal(t, "OK", spanStatusToString(ptrace.StatusCodeOk))
	assert.Equal(t, "ERROR", spanStatusToString(ptrace.StatusCodeError))
	assert.Equal(t, "UNSET", spanStatusToString(ptrace.StatusCodeUnset))

	// Root span parent_span_id matching (Review Issue #6 & #7)
	// Policy: Drop root spans (where parent_span_id == "") unless status_code == "OK" (KEEP)
	dropRootSpans := &testTransformationPolicy{
		name: "drop-root",
		pb: &policyv1alpha1.TraceFilterPolicy{
			Id:     "drop-root",
			Action: policyv1alpha1.Action_ACTION_DROP.Enum(),
			Matches: []*policyv1alpha1.TraceMatcher{
				{
					Target: &policyv1alpha1.TraceFieldSelector{
						Target: &policyv1alpha1.TraceFieldSelector_RecordField{
							RecordField: policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_PARENT_SPAN_ID,
						},
					},
					Predicate: &policyv1alpha1.TraceMatcher_Equals{
						Equals: &policyv1alpha1.Value{Value: &policyv1alpha1.Value_StringValue{StringValue: ""}},
					},
				},
			},
		},
	}

	keepOkSpans := &testTransformationPolicy{
		name: "keep-ok",
		pb: &policyv1alpha1.TraceFilterPolicy{
			Id:     "keep-ok",
			Action: policyv1alpha1.Action_ACTION_KEEP.Enum(),
			Matches: []*policyv1alpha1.TraceMatcher{
				{
					Target: &policyv1alpha1.TraceFieldSelector{
						Target: &policyv1alpha1.TraceFieldSelector_RecordField{
							RecordField: policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_STATUS_CODE,
						},
					},
					Predicate: &policyv1alpha1.TraceMatcher_Equals{
						Equals: &policyv1alpha1.Value{Value: &policyv1alpha1.Value_StringValue{StringValue: "OK"}},
					},
				},
			},
		},
	}

	ev, err := NewEvaluator([]googlepolicy.TransformationPolicy{dropRootSpans, keepOkSpans})
	require.NoError(t, err)

	// Root span with UNSET status -> DROPPED
	rootSpan := ptrace.NewSpan()
	rootSpan.SetName("root")
	assert.True(t, ev.EvalTrace(TraceContext{Span: rootSpan}))

	// Root span with OK status -> KEPT (KEEP overrides DROP)
	rootSpanOk := ptrace.NewSpan()
	rootSpanOk.SetName("root-ok")
	rootSpanOk.Status().SetCode(ptrace.StatusCodeOk)
	assert.False(t, ev.EvalTrace(TraceContext{Span: rootSpanOk}))

	// Child span with ParentSpanID set -> KEPT
	childSpan := ptrace.NewSpan()
	childSpan.SetParentSpanID([8]byte{1, 2, 3, 4, 5, 6, 7, 8})
	assert.False(t, ev.EvalTrace(TraceContext{Span: childSpan}))

	// Test all extractTraceTarget branches
	ctx := TraceContext{
		Span:           childSpan,
		Resource:       pcommon.NewResource(),
		Scope:          pcommon.NewInstrumentationScope(),
		ScopeSchemaURL: "https://trace.schema",
	}
	childSpan.SetTraceID([16]byte{1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1})
	childSpan.SetSpanID([8]byte{2, 2, 2, 2, 2, 2, 2, 2})
	childSpan.Status().SetMessage("err_msg")
	childSpan.Attributes().PutStr("span_k", "span_v")
	ctx.Resource.Attributes().PutStr("res_k", "res_v")
	ctx.Scope.Attributes().PutStr("scope_k", "scope_v")
	ctx.Scope.SetName("trace.scope")
	ctx.Scope.SetVersion("2.0")

	_, ok := extractTraceTarget(ctx, nil)
	assert.False(t, ok)
	_, ok = extractTraceTarget(ctx, &policyv1alpha1.TraceFieldSelector{})
	assert.False(t, ok)

	// Empty TraceID/SpanID
	emptySpanCtx := TraceContext{Span: ptrace.NewSpan()}
	_, ok = extractTraceTarget(emptySpanCtx, &policyv1alpha1.TraceFieldSelector{
		Target: &policyv1alpha1.TraceFieldSelector_RecordField{
			RecordField: policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_TRACE_ID,
		},
	})
	assert.False(t, ok)
	_, ok = extractTraceTarget(emptySpanCtx, &policyv1alpha1.TraceFieldSelector{
		Target: &policyv1alpha1.TraceFieldSelector_RecordField{
			RecordField: policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_SPAN_ID,
		},
	})
	assert.False(t, ok)

	// Populated TraceID, SpanID, ParentSpanID, StatusMessage
	v, ok := extractTraceTarget(ctx, &policyv1alpha1.TraceFieldSelector{
		Target: &policyv1alpha1.TraceFieldSelector_RecordField{
			RecordField: policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_TRACE_ID,
		},
	})
	assert.True(t, ok)
	assert.NotEmpty(t, v)

	v, ok = extractTraceTarget(ctx, &policyv1alpha1.TraceFieldSelector{
		Target: &policyv1alpha1.TraceFieldSelector_RecordField{
			RecordField: policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_SPAN_ID,
		},
	})
	assert.True(t, ok)
	assert.NotEmpty(t, v)

	v, ok = extractTraceTarget(ctx, &policyv1alpha1.TraceFieldSelector{
		Target: &policyv1alpha1.TraceFieldSelector_RecordField{
			RecordField: policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_PARENT_SPAN_ID,
		},
	})
	assert.True(t, ok)
	assert.NotEmpty(t, v)

	v, ok = extractTraceTarget(ctx, &policyv1alpha1.TraceFieldSelector{
		Target: &policyv1alpha1.TraceFieldSelector_RecordField{
			RecordField: policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_STATUS_MESSAGE,
		},
	})
	assert.True(t, ok)
	assert.Equal(t, "err_msg", v)

	_, ok = extractTraceTarget(ctx, &policyv1alpha1.TraceFieldSelector{
		Target: &policyv1alpha1.TraceFieldSelector_RecordField{
			RecordField: policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_UNSPECIFIED,
		},
	})
	assert.False(t, ok)

	// Span, Resource, Scope attributes & Scope fields
	v, ok = extractTraceTarget(ctx, &policyv1alpha1.TraceFieldSelector{
		Target: &policyv1alpha1.TraceFieldSelector_SpanAttribute{
			SpanAttribute: &policyv1alpha1.AttributePath{Path: []string{"span_k"}},
		},
	})
	assert.True(t, ok)
	assert.Equal(t, "span_v", v)

	v, ok = extractTraceTarget(ctx, &policyv1alpha1.TraceFieldSelector{
		Target: &policyv1alpha1.TraceFieldSelector_ResourceAttribute{
			ResourceAttribute: &policyv1alpha1.AttributePath{Path: []string{"res_k"}},
		},
	})
	assert.True(t, ok)
	assert.Equal(t, "res_v", v)

	v, ok = extractTraceTarget(ctx, &policyv1alpha1.TraceFieldSelector{
		Target: &policyv1alpha1.TraceFieldSelector_ScopeAttribute{
			ScopeAttribute: &policyv1alpha1.AttributePath{Path: []string{"scope_k"}},
		},
	})
	assert.True(t, ok)
	assert.Equal(t, "scope_v", v)

	v, ok = extractTraceTarget(ctx, &policyv1alpha1.TraceFieldSelector{
		Target: &policyv1alpha1.TraceFieldSelector_ScopeField{
			ScopeField: policyv1alpha1.ScopeField_SCOPE_FIELD_NAME,
		},
	})
	assert.True(t, ok)
	assert.Equal(t, "trace.scope", v)

	v, ok = extractTraceTarget(ctx, &policyv1alpha1.TraceFieldSelector{
		Target: &policyv1alpha1.TraceFieldSelector_ScopeField{
			ScopeField: policyv1alpha1.ScopeField_SCOPE_FIELD_VERSION,
		},
	})
	assert.True(t, ok)
	assert.Equal(t, "2.0", v)

	v, ok = extractTraceTarget(ctx, &policyv1alpha1.TraceFieldSelector{
		Target: &policyv1alpha1.TraceFieldSelector_ScopeField{
			ScopeField: policyv1alpha1.ScopeField_SCOPE_FIELD_SCHEMA_URL,
		},
	})
	assert.True(t, ok)
	assert.Equal(t, "https://trace.schema", v)

	_, ok = extractTraceTarget(ctx, &policyv1alpha1.TraceFieldSelector{
		Target: &policyv1alpha1.TraceFieldSelector_ScopeField{
			ScopeField: policyv1alpha1.ScopeField_SCOPE_FIELD_UNSPECIFIED,
		},
	})
	assert.False(t, ok)
}
