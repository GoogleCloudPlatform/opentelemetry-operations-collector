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
							Value: &policyv1alpha1.Value_StringValue{StringValue: "histogram"},
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
