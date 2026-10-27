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
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy/logfilter"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy/logtransform"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy/metricfilter"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy/tracefilter"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"google.golang.org/protobuf/proto"
)

// All predicate compilation, target extraction and value matching now live in
// pkg/googlepolicy/{logfilter,metricfilter,tracefilter} and their shared
// internal/matcher package, and are unit-tested there. The processor is
// pipeline plumbing, so everything below drives it through its public surface:
// NewEvaluator + Transform{Logs,Metrics,Traces} + Eval{Log,Metric,Trace}.

// nonEvaluatorPolicy is a TransformationPolicy that implements none of the
// per-signal evaluator interfaces. NewEvaluator has no way to evaluate such a
// policy, so it must reject it at load time.
type nonEvaluatorPolicy struct {
	name string
}

func (p *nonEvaluatorPolicy) PolicyName() string { return p.name }
func (p *nonEvaluatorPolicy) PolicyType() string { return "non_evaluator" }
func (p *nonEvaluatorPolicy) PolicyClass() googlepolicy.PolicyClass {
	return googlepolicy.PolicyClassTransformation
}
func (p *nonEvaluatorPolicy) Validate() error                      { return nil }
func (p *nonEvaluatorPolicy) TargetSignals() []googlepolicy.Signal { return nil }

// fakeMetricPolicy is a hand-written MetricFilterPolicyEvaluator used to observe how
// often, and with which datapoint attributes, the Evaluator invokes a metric
// policy. It is the only way to assert the two-tier instrument-vs-datapoint
// optimisation from outside the package.
type fakeMetricPolicy struct {
	name           string
	datapointLevel bool
	result         googlepolicy.FilterResult
	// eval, when set, overrides result.
	eval func(ctx googlepolicy.MetricContext) googlepolicy.FilterResult

	calls              int
	seenDatapointAttrs []map[string]any
}

func (p *fakeMetricPolicy) PolicyName() string { return p.name }
func (p *fakeMetricPolicy) PolicyType() string { return "fake_metric" }
func (p *fakeMetricPolicy) PolicyClass() googlepolicy.PolicyClass {
	return googlepolicy.PolicyClassTransformation
}
func (p *fakeMetricPolicy) Validate() error { return nil }
func (p *fakeMetricPolicy) TargetSignals() []googlepolicy.Signal {
	return []googlepolicy.Signal{googlepolicy.SignalMetrics}
}
func (p *fakeMetricPolicy) IsDatapointLevel() bool { return p.datapointLevel }
func (p *fakeMetricPolicy) FilterMetric(ctx googlepolicy.MetricContext) googlepolicy.FilterResult {
	p.calls++
	p.seenDatapointAttrs = append(p.seenDatapointAttrs, ctx.DatapointAttributes.AsRaw())
	if p.eval != nil {
		return p.eval(ctx)
	}
	return p.result
}

// fakeLogPolicy is a hand-written LogFilterPolicyEvaluator that is not a
// logfilter.Policy, pinning that NewEvaluator dispatches on the evaluator
// interface rather than on a concrete filter-package type.
type fakeLogPolicy struct {
	result googlepolicy.FilterResult
}

func (p *fakeLogPolicy) PolicyName() string { return "fake-log" }
func (p *fakeLogPolicy) PolicyType() string { return "fake_log" }
func (p *fakeLogPolicy) PolicyClass() googlepolicy.PolicyClass {
	return googlepolicy.PolicyClassTransformation
}
func (p *fakeLogPolicy) Validate() error { return nil }
func (p *fakeLogPolicy) TargetSignals() []googlepolicy.Signal {
	return []googlepolicy.Signal{googlepolicy.SignalLogs}
}
func (p *fakeLogPolicy) FilterLog(googlepolicy.LogContext) googlepolicy.FilterResult {
	return p.result
}

func TestEvaluator_LogFiltering(t *testing.T) {
	// Policy 1: Drop logs where severity_text glob matches "DEBUG*"
	dropDebug := mustPolicy(t, &policyv1alpha1.LogFilterPolicy{
		Id:     "drop-debug",
		Action: policyv1alpha1.Action_ACTION_DROP.Enum(),
		Matchers: []*policyv1alpha1.LogMatcher{
			{
				Target: &policyv1alpha1.LogFieldSelector{
					Target: &policyv1alpha1.LogFieldSelector_RecordField{
						RecordField: policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SEVERITY_TEXT,
					},
				},
				Predicate: &policyv1alpha1.LogMatcher_Glob{Glob: "DEBUG*"},
			},
		},
	})

	// Policy 2: Keep logs (exemption) where resource attribute env == "production"
	keepProd := mustPolicy(t, &policyv1alpha1.LogFilterPolicy{
		Id:     "keep-prod",
		Action: policyv1alpha1.Action_ACTION_KEEP.Enum(),
		Matchers: []*policyv1alpha1.LogMatcher{
			{
				Target: &policyv1alpha1.LogFieldSelector{
					Target: &policyv1alpha1.LogFieldSelector_ResourceAttributePath{
						ResourceAttributePath: &policyv1alpha1.AttributePath{Segments: []string{"env"}},
					},
				},
				Predicate: &policyv1alpha1.LogMatcher_Equality{
					Equality: &policyv1alpha1.Value{
						Kind: &policyv1alpha1.Value_StringValue{StringValue: "production"},
					},
				},
			},
		},
	})

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
	// Drop logs where http.method != "GET" (negated equals)
	dropNonGet := mustPolicy(t, &policyv1alpha1.LogFilterPolicy{
		Id:     "drop-non-get",
		Action: policyv1alpha1.Action_ACTION_DROP.Enum(),
		Matchers: []*policyv1alpha1.LogMatcher{
			{
				Target: &policyv1alpha1.LogFieldSelector{
					Target: &policyv1alpha1.LogFieldSelector_LogAttributePath{
						LogAttributePath: &policyv1alpha1.AttributePath{Segments: []string{"http", "method"}},
					},
				},
				Predicate: &policyv1alpha1.LogMatcher_Equality{
					Equality: &policyv1alpha1.Value{
						Kind: &policyv1alpha1.Value_StringValue{StringValue: "GET"},
					},
				},
				Negated: true,
			},
		},
	})

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
	dropHist := mustPolicy(t, &policyv1alpha1.MetricFilterPolicy{
		Id:     "drop-duration-histogram",
		Action: policyv1alpha1.Action_ACTION_DROP.Enum(),
		Matchers: []*policyv1alpha1.MetricMatcher{
			{
				Target: &policyv1alpha1.MetricFieldSelector{
					Target: &policyv1alpha1.MetricFieldSelector_DescriptorField{
						DescriptorField: policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_NAME,
					},
				},
				Predicate: &policyv1alpha1.MetricMatcher_Equality{
					Equality: &policyv1alpha1.Value{
						Kind: &policyv1alpha1.Value_StringValue{StringValue: "http.server.duration"},
					},
				},
			},
			{
				Target: &policyv1alpha1.MetricFieldSelector{
					Target: &policyv1alpha1.MetricFieldSelector_DescriptorField{
						DescriptorField: policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_TYPE,
					},
				},
				Predicate: &policyv1alpha1.MetricMatcher_Equality{
					Equality: &policyv1alpha1.Value{
						Kind: &policyv1alpha1.Value_StringValue{StringValue: "HISTOGRAM"},
					},
				},
			},
		},
	})

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
	dropHealthcheck := mustPolicy(t, &policyv1alpha1.TraceFilterPolicy{
		Id: "drop-healthcheck",
		Matchers: []*policyv1alpha1.TraceMatcher{
			{
				Target: &policyv1alpha1.TraceFieldSelector{
					Target: &policyv1alpha1.TraceFieldSelector_RecordField{
						RecordField: policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_NAME,
					},
				},
				Predicate: &policyv1alpha1.TraceMatcher_Glob{Glob: "healthcheck*"},
			},
			{
				Target: &policyv1alpha1.TraceFieldSelector{
					Target: &policyv1alpha1.TraceFieldSelector_RecordField{
						RecordField: policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_KIND,
					},
				},
				Predicate: &policyv1alpha1.TraceMatcher_Equality{
					Equality: &policyv1alpha1.Value{
						Kind: &policyv1alpha1.Value_StringValue{StringValue: "INTERNAL"},
					},
				},
			},
		},
	})

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

// TestNewEvaluator_Dispatch covers how NewEvaluator routes each kind of
// TransformationPolicy it can be handed.
func TestNewEvaluator_Dispatch(t *testing.T) {
	t.Run("nil policies are skipped", func(t *testing.T) {
		ev, err := NewEvaluator([]googlepolicy.TransformationPolicy{nil})
		require.NoError(t, err)
		require.NotNil(t, ev)
		assert.Empty(t, ev.logFilterPolicies)
		assert.Empty(t, ev.metricFilterPolicies)
		assert.Empty(t, ev.traceFilterPolicies)
	})

	t.Run("compiled policies are routed to their signal", func(t *testing.T) {
		ev, err := NewEvaluator([]googlepolicy.TransformationPolicy{
			mustPolicy(t, newDropLogBodyProto("pre-log", "BAD")),
			mustPolicy(t, newDropMetricNameProto("pre-metric", "drop.me")),
			mustPolicy(t, newDropSpanNameProto("pre-trace", "drop.span")),
		})
		require.NoError(t, err)
		require.Len(t, ev.logFilterPolicies, 1)
		require.Len(t, ev.metricFilterPolicies, 1)
		require.Len(t, ev.traceFilterPolicies, 1)

		lr := plog.NewLogRecord()
		lr.Body().SetStr("BAD")
		assert.True(t, ev.EvalLog(LogContext{Record: lr}))

		m := pmetric.NewMetric()
		m.SetName("drop.me")
		m.SetEmptyGauge()
		assert.True(t, ev.EvalMetric(MetricContext{Metric: m, DatapointAttributes: pcommon.NewMap()}))

		span := ptrace.NewSpan()
		span.SetName("drop.span")
		assert.True(t, ev.EvalTrace(TraceContext{Span: span}))
	})

	t.Run("dispatch is by evaluator interface, not by concrete type", func(t *testing.T) {
		// fakeLogPolicy is not a logfilter.Policy; implementing
		// LogFilterPolicyEvaluator is all it takes to land on the log path.
		ev, err := NewEvaluator([]googlepolicy.TransformationPolicy{
			&fakeLogPolicy{result: googlepolicy.FilterDrop},
		})
		require.NoError(t, err)
		require.Len(t, ev.logFilterPolicies, 1)
		assert.True(t, ev.EvalLog(LogContext{Record: plog.NewLogRecord()}))
	})

	t.Run("datapoint-level metric policies are flagged", func(t *testing.T) {
		instrumentOnly, err := NewEvaluator([]googlepolicy.TransformationPolicy{
			mustPolicy(t, newDropMetricNameProto("by-name", "drop.me")),
		})
		require.NoError(t, err)
		assert.False(t, instrumentOnly.hasDatapointMetricFilterPolicies)

		withDatapoint, err := NewEvaluator([]googlepolicy.TransformationPolicy{
			mustPolicy(t, newDropMetricNameProto("by-name", "drop.me")),
			mustPolicy(t, newDropDatapointAttrProto("by-attr", "drop_me", "true")),
		})
		require.NoError(t, err)
		assert.True(t, withDatapoint.hasDatapointMetricFilterPolicies)
	})
}

// TestNewEvaluator_RejectsPolicyWithoutEvaluator pins the fail-fast contract: a
// transformation policy that implements none of the three per-signal evaluator
// interfaces can never be evaluated, so NewEvaluator must surface a descriptive
// error at load time rather than panicking or silently skipping the policy.
//
// Rejecting a *malformed* policy proto is the owning filter package's job (each
// NewPolicyFromProto validates before returning a policy) and is covered there.
func TestNewEvaluator_RejectsPolicyWithoutEvaluator(t *testing.T) {
	bad := &nonEvaluatorPolicy{name: "not-an-evaluator"}

	ev, err := NewEvaluator([]googlepolicy.TransformationPolicy{
		mustPolicy(t, newDropLogBodyProto("good-log", "BAD")),
		bad,
	})

	require.Error(t, err)
	assert.Nil(t, ev, "a rejected policy must not yield a half-populated evaluator")
	assert.ErrorContains(t, err, "not-an-evaluator")
	assert.ErrorContains(t, err, "*googlepolicyprocessor.nonEvaluatorPolicy")
	assert.ErrorContains(t, err, "googlepolicy.LogFilterPolicyEvaluator")
	assert.ErrorContains(t, err, "googlepolicy.MetricFilterPolicyEvaluator")
	assert.ErrorContains(t, err, "googlepolicy.TraceFilterPolicyEvaluator")
}

// TestEvaluator_NoPoliciesIsNoOp pins the fast paths taken when a signal has no
// policies at all: the pdata tree must be left completely untouched.
func TestEvaluator_NoPoliciesIsNoOp(t *testing.T) {
	ev := &Evaluator{}

	ld := plog.NewLogs()
	ld.ResourceLogs().AppendEmpty().ScopeLogs().AppendEmpty().LogRecords().AppendEmpty()
	ev.TransformLogs(ld)
	assert.Equal(t, 1, ld.ResourceLogs().Len())
	assert.Equal(t, 1, ld.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().Len())
	assert.False(t, ev.EvalLog(LogContext{Record: plog.NewLogRecord()}))

	md := pmetric.NewMetrics()
	emptyMetric := md.ResourceMetrics().AppendEmpty().ScopeMetrics().AppendEmpty().Metrics().AppendEmpty()
	emptyMetric.SetEmptyGauge()
	ev.TransformMetrics(md)
	assert.Equal(t, 1, md.ResourceMetrics().Len())
	assert.Equal(t, 1, md.ResourceMetrics().At(0).ScopeMetrics().At(0).Metrics().Len())
	assert.False(t, ev.EvalMetric(MetricContext{
		Metric:              pmetric.NewMetric(),
		DatapointAttributes: pcommon.NewMap(),
	}))

	td := ptrace.NewTraces()
	td.ResourceSpans().AppendEmpty().ScopeSpans().AppendEmpty().Spans().AppendEmpty()
	ev.TransformTraces(td)
	assert.Equal(t, 1, td.ResourceSpans().Len())
	assert.Equal(t, 1, td.ResourceSpans().At(0).ScopeSpans().At(0).Spans().Len())
	assert.False(t, ev.EvalTrace(TraceContext{Span: ptrace.NewSpan()}))
}

func TestEvaluator_MetricDatapointPruningAndEmptyMetrics(t *testing.T) {
	// Policy 1: Datapoint-level DROP where datapoint attribute "drop_me" == "true"
	dropDpPolicy := mustPolicy(t, newDropDatapointAttrProto("drop-dp", "drop_me", "true"))

	// Policy 2: Instrument-level KEEP where metric name == "exempt.metric"
	keepInstrumentPolicy := mustPolicy(t, &policyv1alpha1.MetricFilterPolicy{
		Id:     "keep-exempt",
		Action: policyv1alpha1.Action_ACTION_KEEP.Enum(),
		Matchers: []*policyv1alpha1.MetricMatcher{
			{
				Target: &policyv1alpha1.MetricFieldSelector{
					Target: &policyv1alpha1.MetricFieldSelector_DescriptorField{
						DescriptorField: policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_NAME,
					},
				},
				Predicate: &policyv1alpha1.MetricMatcher_Equality{
					Equality: &policyv1alpha1.Value{Kind: &policyv1alpha1.Value_StringValue{StringValue: "exempt.metric"}},
				},
			},
		},
	})

	ev, err := NewEvaluator([]googlepolicy.TransformationPolicy{dropDpPolicy, keepInstrumentPolicy})
	require.NoError(t, err)
	require.True(t, ev.hasDatapointMetricFilterPolicies)

	md := pmetric.NewMetrics()
	rm := md.ResourceMetrics().AppendEmpty()
	sm := rm.ScopeMetrics().AppendEmpty()

	// 1. Metric with 0 datapoints: must NOT be dropped by the datapoint policy.
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

	// 6. Exempt metric (instrument KEEP) with drop_me="true" -> KEPT via short-circuit
	mExempt := sm.Metrics().AppendEmpty()
	mExempt.SetName("exempt.metric")
	edp := mExempt.SetEmptyGauge().DataPoints().AppendEmpty()
	edp.Attributes().PutStr("drop_me", "true")

	// 7. Unset metric type (MetricTypeEmpty) -> not dropped
	mUnset := sm.Metrics().AppendEmpty()
	mUnset.SetName("unset.type")

	// 8. Empty metric matching the KEEP policy -> instrument-only KEEP branch
	mEmptyExempt := sm.Metrics().AppendEmpty()
	mEmptyExempt.SetName("exempt.metric")
	mEmptyExempt.SetEmptyGauge()

	// 9. Direct EvalMetric with the KEEP policy matching
	assert.False(t, ev.EvalMetric(MetricContext{
		Metric:              mEmptyExempt,
		DatapointAttributes: pcommon.NewMap(),
	}))

	ev.TransformMetrics(md)

	// pruned.hist should be removed; the other 7 metrics should remain
	metrics := md.ResourceMetrics().At(0).ScopeMetrics().At(0).Metrics()
	require.Equal(t, 7, metrics.Len())
	assert.Equal(t, "empty.gauge", metrics.At(0).Name())
	assert.Equal(t, 0, metrics.At(0).Gauge().DataPoints().Len())
	assert.Equal(t, "partial.sum", metrics.At(1).Name())
	require.Equal(t, 1, metrics.At(1).Sum().DataPoints().Len())
	dropMe, ok := metrics.At(1).Sum().DataPoints().At(0).Attributes().Get("drop_me")
	require.True(t, ok)
	assert.Equal(t, "false", dropMe.Str())
	assert.Equal(t, "kept.exphist", metrics.At(2).Name())
	assert.Equal(t, 1, metrics.At(2).ExponentialHistogram().DataPoints().Len())
	assert.Equal(t, "kept.summary", metrics.At(3).Name())
	assert.Equal(t, 1, metrics.At(3).Summary().DataPoints().Len())
	assert.Equal(t, "exempt.metric", metrics.At(4).Name())
	assert.Equal(t, 1, metrics.At(4).Gauge().DataPoints().Len())
	assert.Equal(t, "unset.type", metrics.At(5).Name())
	assert.Equal(t, "exempt.metric", metrics.At(6).Name())
}

// TestEvaluator_MetricInstrumentLevelDropByType drives every pmetric type
// through TransformMetrics, asserting that the processor surfaces the right
// instrument type to the policy and prunes only the intended instrument.
func TestEvaluator_MetricInstrumentLevelDropByType(t *testing.T) {
	tests := []struct {
		typeName string
		expected []string
	}{
		{"GAUGE", []string{"sum.metric", "histogram.metric", "exphistogram.metric", "summary.metric"}},
		{"SUM", []string{"gauge.metric", "histogram.metric", "exphistogram.metric", "summary.metric"}},
		{"HISTOGRAM", []string{"gauge.metric", "sum.metric", "exphistogram.metric", "summary.metric"}},
		{"EXPONENTIAL_HISTOGRAM", []string{"gauge.metric", "sum.metric", "histogram.metric", "summary.metric"}},
		{"SUMMARY", []string{"gauge.metric", "sum.metric", "histogram.metric", "exphistogram.metric"}},
	}

	for _, tc := range tests {
		t.Run(tc.typeName, func(t *testing.T) {
			ev, err := NewEvaluator([]googlepolicy.TransformationPolicy{
				mustPolicy(t, newDropMetricDescriptorProto("drop-by-type",
					policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_TYPE, tc.typeName)),
			})
			require.NoError(t, err)
			require.False(t, ev.hasDatapointMetricFilterPolicies)

			md := newAllMetricTypes()
			ev.TransformMetrics(md)
			assert.Equal(t, tc.expected, metricNames(md))
		})
	}

	t.Run("unset instrument type matches UNSPECIFIED", func(t *testing.T) {
		ev, err := NewEvaluator([]googlepolicy.TransformationPolicy{
			mustPolicy(t, newDropMetricDescriptorProto("drop-unspecified",
				policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_TYPE, "UNSPECIFIED")),
		})
		require.NoError(t, err)

		md := pmetric.NewMetrics()
		sm := md.ResourceMetrics().AppendEmpty().ScopeMetrics().AppendEmpty()
		sm.Metrics().AppendEmpty().SetName("unset.type")

		ev.TransformMetrics(md)
		assert.Empty(t, metricNames(md), "untyped MetricTypeEmpty matches UNSPECIFIED and is dropped")
	})
}

// TestEvaluator_MetricTemporalityIsPerInstrument asserts the processor fills
// MetricContext.AggregationTemporality from the concrete instrument, and leaves
// it unspecified for instruments that have no temporality (gauge, summary).
func TestEvaluator_MetricTemporalityIsPerInstrument(t *testing.T) {
	tests := []struct {
		temporality string
		expected    []string
	}{
		{"DELTA", []string{"gauge.metric", "sum.metric", "histogram.metric", "summary.metric"}},
		{"CUMULATIVE", []string{"gauge.metric", "exphistogram.metric", "summary.metric"}},
	}

	for _, tc := range tests {
		t.Run(tc.temporality, func(t *testing.T) {
			ev, err := NewEvaluator([]googlepolicy.TransformationPolicy{
				mustPolicy(t, newDropMetricDescriptorProto("drop-by-temporality",
					policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_AGGREGATION_TEMPORALITY, tc.temporality)),
			})
			require.NoError(t, err)

			md := newAllMetricTypes()
			ev.TransformMetrics(md)
			assert.Equal(t, tc.expected, metricNames(md))
		})
	}
}

// TestEvaluator_MetricTwoTierEvaluation pins the instrument-level vs
// datapoint-level optimisation by counting policy invocations.
func TestEvaluator_MetricTwoTierEvaluation(t *testing.T) {
	newSumWithDatapoints := func(names ...string) pmetric.Metrics {
		md := pmetric.NewMetrics()
		sm := md.ResourceMetrics().AppendEmpty().ScopeMetrics().AppendEmpty()
		m := sm.Metrics().AppendEmpty()
		m.SetName("the.sum")
		dps := m.SetEmptySum().DataPoints()
		for _, n := range names {
			dps.AppendEmpty().Attributes().PutStr("dp", n)
		}
		return md
	}

	t.Run("instrument-level policy is evaluated once per instrument", func(t *testing.T) {
		fake := &fakeMetricPolicy{name: "instrument", result: googlepolicy.FilterDrop}
		ev, err := NewEvaluator([]googlepolicy.TransformationPolicy{fake})
		require.NoError(t, err)
		require.False(t, ev.hasDatapointMetricFilterPolicies)

		md := newSumWithDatapoints("a", "b", "c")
		ev.TransformMetrics(md)

		assert.Equal(t, 1, fake.calls, "instrument-level policy must not be re-evaluated per datapoint")
		assert.Equal(t, []map[string]any{{}}, fake.seenDatapointAttrs,
			"instrument-level evaluation must not see datapoint attributes")
		assert.Empty(t, metricNames(md), "instrument DROP removes the whole instrument, scope and resource")
	})

	t.Run("datapoint-level policy is evaluated once per datapoint", func(t *testing.T) {
		fake := &fakeMetricPolicy{
			name:           "datapoint",
			datapointLevel: true,
			eval: func(ctx googlepolicy.MetricContext) googlepolicy.FilterResult {
				if v, ok := ctx.DatapointAttributes.Get("dp"); ok && v.Str() != "keep" {
					return googlepolicy.FilterDrop
				}
				return googlepolicy.FilterNoMatch
			},
		}
		ev, err := NewEvaluator([]googlepolicy.TransformationPolicy{fake})
		require.NoError(t, err)
		require.True(t, ev.hasDatapointMetricFilterPolicies)

		md := newSumWithDatapoints("a", "keep", "c")
		ev.TransformMetrics(md)

		assert.Equal(t, 3, fake.calls)
		require.Equal(t, []string{"the.sum"}, metricNames(md))
		dps := md.ResourceMetrics().At(0).ScopeMetrics().At(0).Metrics().At(0).Sum().DataPoints()
		require.Equal(t, 1, dps.Len())
		v, ok := dps.At(0).Attributes().Get("dp")
		require.True(t, ok)
		assert.Equal(t, "keep", v.Str())
	})

	t.Run("instrument-level KEEP short-circuits datapoint pruning", func(t *testing.T) {
		keep := &fakeMetricPolicy{name: "keep", result: googlepolicy.FilterKeep}
		drop := &fakeMetricPolicy{name: "drop", datapointLevel: true, result: googlepolicy.FilterDrop}
		ev, err := NewEvaluator([]googlepolicy.TransformationPolicy{keep, drop})
		require.NoError(t, err)

		md := newSumWithDatapoints("a", "b", "c")
		ev.TransformMetrics(md)

		assert.Equal(t, 1, keep.calls)
		assert.Equal(t, 0, drop.calls, "an instrument-level KEEP must exempt every datapoint below it")
		require.Equal(t, []string{"the.sum"}, metricNames(md))
		assert.Equal(t, 3, md.ResourceMetrics().At(0).ScopeMetrics().At(0).Metrics().At(0).Sum().DataPoints().Len())
	})

	t.Run("datapoint-level policies are skipped for instruments with no datapoints", func(t *testing.T) {
		drop := &fakeMetricPolicy{name: "drop", datapointLevel: true, result: googlepolicy.FilterDrop}
		ev, err := NewEvaluator([]googlepolicy.TransformationPolicy{drop})
		require.NoError(t, err)

		md := pmetric.NewMetrics()
		sm := md.ResourceMetrics().AppendEmpty().ScopeMetrics().AppendEmpty()
		m := sm.Metrics().AppendEmpty()
		m.SetName("empty.gauge")
		m.SetEmptyGauge()

		ev.TransformMetrics(md)

		assert.Equal(t, 0, drop.calls,
			"a datapoint-level policy must never decide an instrument that has no datapoints")
		assert.Equal(t, []string{"empty.gauge"}, metricNames(md))
	})

	t.Run("instrument-level policy is evaluated once even when datapoint-level policy is present", func(t *testing.T) {
		instDrop := &fakeMetricPolicy{name: "inst-drop", result: googlepolicy.FilterDrop}
		dpKeep := &fakeMetricPolicy{
			name:           "dp-keep",
			datapointLevel: true,
			eval: func(ctx googlepolicy.MetricContext) googlepolicy.FilterResult {
				if v, ok := ctx.DatapointAttributes.Get("dp"); ok && v.Str() == "exempt" {
					return googlepolicy.FilterKeep
				}
				return googlepolicy.FilterNoMatch
			},
		}
		ev, err := NewEvaluator([]googlepolicy.TransformationPolicy{instDrop, dpKeep})
		require.NoError(t, err)
		require.True(t, ev.hasDatapointMetricFilterPolicies)

		md := newSumWithDatapoints("a", "exempt", "c")
		ev.TransformMetrics(md)

		assert.Equal(t, 1, instDrop.calls, "instrument-level policy must only be evaluated once per metric, not per datapoint")
		assert.Equal(t, 3, dpKeep.calls, "datapoint-level policy must be evaluated once per datapoint")
		require.Equal(t, []string{"the.sum"}, metricNames(md))
		dps := md.ResourceMetrics().At(0).ScopeMetrics().At(0).Metrics().At(0).Sum().DataPoints()
		require.Equal(t, 1, dps.Len())
		v, ok := dps.At(0).Attributes().Get("dp")
		require.True(t, ok)
		assert.Equal(t, "exempt", v.Str(), "datapoint-level KEEP must exempt matching datapoint from instrument-level DROP")
	})

	t.Run("untyped metric (MetricTypeEmpty) is evaluated and dropped by instrument policy", func(t *testing.T) {
		instDrop := &fakeMetricPolicy{name: "inst-drop", result: googlepolicy.FilterDrop}
		ev, err := NewEvaluator([]googlepolicy.TransformationPolicy{instDrop})
		require.NoError(t, err)

		md := pmetric.NewMetrics()
		sm := md.ResourceMetrics().AppendEmpty().ScopeMetrics().AppendEmpty()
		m := sm.Metrics().AppendEmpty()
		m.SetName("untyped.metric")
		require.Equal(t, pmetric.MetricTypeEmpty, m.Type())

		ev.TransformMetrics(md)

		assert.Equal(t, 1, instDrop.calls)
		assert.Empty(t, metricNames(md), "untyped MetricTypeEmpty metric must be dropped when instrument policy returns FilterDrop")
	})
}

// TestEvaluator_MultipleTracePoliciesDrop verifies that multiple TraceFilterPolicy
// instances evaluate with OR-drop semantics and that root spans (empty parent_span_id)
// can be exempted via a negated parent_span_id matcher.
func TestEvaluator_MultipleTracePoliciesDrop(t *testing.T) {
	dropNonRootInternal := mustPolicy(t, &policyv1alpha1.TraceFilterPolicy{
		Id: "drop-non-root-internal",
		Matchers: []*policyv1alpha1.TraceMatcher{
			{
				Target: &policyv1alpha1.TraceFieldSelector{
					Target: &policyv1alpha1.TraceFieldSelector_RecordField{
						RecordField: policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_PARENT_SPAN_ID,
					},
				},
				Negated: true,
				Predicate: &policyv1alpha1.TraceMatcher_Equality{
					Equality: &policyv1alpha1.Value{Kind: &policyv1alpha1.Value_StringValue{StringValue: ""}},
				},
			},
			{
				Target: &policyv1alpha1.TraceFieldSelector{
					Target: &policyv1alpha1.TraceFieldSelector_RecordField{
						RecordField: policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_KIND,
					},
				},
				Predicate: &policyv1alpha1.TraceMatcher_Equality{
					Equality: &policyv1alpha1.Value{Kind: &policyv1alpha1.Value_StringValue{StringValue: "INTERNAL"}},
				},
			},
		},
	})

	dropHealthcheckRoot := mustPolicy(t, &policyv1alpha1.TraceFilterPolicy{
		Id: "drop-healthcheck-root",
		Matchers: []*policyv1alpha1.TraceMatcher{
			{
				Target: &policyv1alpha1.TraceFieldSelector{
					Target: &policyv1alpha1.TraceFieldSelector_RecordField{
						RecordField: policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_NAME,
					},
				},
				Predicate: &policyv1alpha1.TraceMatcher_Glob{
					Glob: "healthz.*",
				},
			},
		},
	})

	ev, err := NewEvaluator([]googlepolicy.TransformationPolicy{dropNonRootInternal, dropHealthcheckRoot})
	require.NoError(t, err)

	// Child span with INTERNAL kind -> DROPPED by first policy
	childInternal := ptrace.NewSpan()
	childInternal.SetName("cache.lookup")
	childInternal.SetKind(ptrace.SpanKindInternal)
	childInternal.SetParentSpanID([8]byte{1, 2, 3, 4, 5, 6, 7, 8})
	assert.True(t, ev.EvalTrace(TraceContext{Span: childInternal}))

	// Root span with INTERNAL kind -> KEPT (exempted by negated parent_span_id == "")
	rootInternal := ptrace.NewSpan()
	rootInternal.SetName("worker.loop")
	rootInternal.SetKind(ptrace.SpanKindInternal)
	assert.False(t, ev.EvalTrace(TraceContext{Span: rootInternal}))

	// Root span matching healthz.* -> DROPPED by second policy
	rootHealth := ptrace.NewSpan()
	rootHealth.SetName("healthz.check")
	rootHealth.SetKind(ptrace.SpanKindServer)
	assert.True(t, ev.EvalTrace(TraceContext{Span: rootHealth}))

	// Child span with SERVER kind -> KEPT
	childServer := ptrace.NewSpan()
	childServer.SetName("rpc.call")
	childServer.SetKind(ptrace.SpanKindServer)
	childServer.SetParentSpanID([8]byte{1, 2, 3, 4, 5, 6, 7, 8})
	assert.False(t, ev.EvalTrace(TraceContext{Span: childServer}))

	// Same spans through the batch walk.
	td := ptrace.NewTraces()
	ss := td.ResourceSpans().AppendEmpty().ScopeSpans().AppendEmpty()
	childInternal.CopyTo(ss.Spans().AppendEmpty())
	rootInternal.CopyTo(ss.Spans().AppendEmpty())
	rootHealth.CopyTo(ss.Spans().AppendEmpty())
	childServer.CopyTo(ss.Spans().AppendEmpty())

	ev.TransformTraces(td)
	require.Equal(t, 1, td.ResourceSpans().Len())
	spans := td.ResourceSpans().At(0).ScopeSpans().At(0).Spans()
	require.Equal(t, 2, spans.Len())
	assert.Equal(t, "worker.loop", spans.At(0).Name())
	assert.Equal(t, "rpc.call", spans.At(1).Name())
}

// TestEvaluator_TransformPrunesEmptyScopesAndResources asserts that each
// Transform* walk removes scopes that lose all their items and resources that
// lose all their scopes, for all three signals.
func TestEvaluator_TransformPrunesEmptyScopesAndResources(t *testing.T) {
	t.Run("logs", func(t *testing.T) {
		ev, err := NewEvaluator([]googlepolicy.TransformationPolicy{
			mustPolicy(t, newDropLogBodyProto("drop-log", "DROP")),
		})
		require.NoError(t, err)

		ld := plog.NewLogs()
		rl0 := ld.ResourceLogs().AppendEmpty()
		rl0.Resource().Attributes().PutStr("host", "survivor")
		sl00 := rl0.ScopeLogs().AppendEmpty()
		sl00.Scope().SetName("scope.keeps")
		sl00.LogRecords().AppendEmpty().Body().SetStr("KEEP")
		sl00.LogRecords().AppendEmpty().Body().SetStr("DROP")
		sl01 := rl0.ScopeLogs().AppendEmpty()
		sl01.Scope().SetName("scope.empties")
		sl01.LogRecords().AppendEmpty().Body().SetStr("DROP")

		rl1 := ld.ResourceLogs().AppendEmpty()
		rl1.Resource().Attributes().PutStr("host", "doomed")
		rl1.ScopeLogs().AppendEmpty().LogRecords().AppendEmpty().Body().SetStr("DROP")

		ev.TransformLogs(ld)

		require.Equal(t, 1, ld.ResourceLogs().Len())
		host, ok := ld.ResourceLogs().At(0).Resource().Attributes().Get("host")
		require.True(t, ok)
		assert.Equal(t, "survivor", host.Str())
		require.Equal(t, 1, ld.ResourceLogs().At(0).ScopeLogs().Len())
		assert.Equal(t, "scope.keeps", ld.ResourceLogs().At(0).ScopeLogs().At(0).Scope().Name())
		records := ld.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords()
		require.Equal(t, 1, records.Len())
		assert.Equal(t, "KEEP", records.At(0).Body().Str())
	})

	t.Run("metrics", func(t *testing.T) {
		ev, err := NewEvaluator([]googlepolicy.TransformationPolicy{
			mustPolicy(t, newDropMetricNameProto("drop-metric", "drop.me")),
		})
		require.NoError(t, err)

		md := pmetric.NewMetrics()
		rm0 := md.ResourceMetrics().AppendEmpty()
		rm0.Resource().Attributes().PutStr("host", "survivor")
		sm00 := rm0.ScopeMetrics().AppendEmpty()
		sm00.Scope().SetName("scope.keeps")
		appendGauge(sm00, "keep.me")
		appendGauge(sm00, "drop.me")
		sm01 := rm0.ScopeMetrics().AppendEmpty()
		sm01.Scope().SetName("scope.empties")
		appendGauge(sm01, "drop.me")

		rm1 := md.ResourceMetrics().AppendEmpty()
		rm1.Resource().Attributes().PutStr("host", "doomed")
		appendGauge(rm1.ScopeMetrics().AppendEmpty(), "drop.me")

		ev.TransformMetrics(md)

		require.Equal(t, 1, md.ResourceMetrics().Len())
		host, ok := md.ResourceMetrics().At(0).Resource().Attributes().Get("host")
		require.True(t, ok)
		assert.Equal(t, "survivor", host.Str())
		require.Equal(t, 1, md.ResourceMetrics().At(0).ScopeMetrics().Len())
		assert.Equal(t, "scope.keeps", md.ResourceMetrics().At(0).ScopeMetrics().At(0).Scope().Name())
		assert.Equal(t, []string{"keep.me"}, metricNames(md))
	})

	t.Run("traces", func(t *testing.T) {
		ev, err := NewEvaluator([]googlepolicy.TransformationPolicy{
			mustPolicy(t, newDropSpanNameProto("drop-trace", "drop.span")),
		})
		require.NoError(t, err)

		td := ptrace.NewTraces()
		rs0 := td.ResourceSpans().AppendEmpty()
		rs0.Resource().Attributes().PutStr("host", "survivor")
		ss00 := rs0.ScopeSpans().AppendEmpty()
		ss00.Scope().SetName("scope.keeps")
		ss00.Spans().AppendEmpty().SetName("keep.span")
		ss00.Spans().AppendEmpty().SetName("drop.span")
		ss01 := rs0.ScopeSpans().AppendEmpty()
		ss01.Scope().SetName("scope.empties")
		ss01.Spans().AppendEmpty().SetName("drop.span")

		rs1 := td.ResourceSpans().AppendEmpty()
		rs1.Resource().Attributes().PutStr("host", "doomed")
		rs1.ScopeSpans().AppendEmpty().Spans().AppendEmpty().SetName("drop.span")

		ev.TransformTraces(td)

		require.Equal(t, 1, td.ResourceSpans().Len())
		host, ok := td.ResourceSpans().At(0).Resource().Attributes().Get("host")
		require.True(t, ok)
		assert.Equal(t, "survivor", host.Str())
		require.Equal(t, 1, td.ResourceSpans().At(0).ScopeSpans().Len())
		assert.Equal(t, "scope.keeps", td.ResourceSpans().At(0).ScopeSpans().At(0).Scope().Name())
		spans := td.ResourceSpans().At(0).ScopeSpans().At(0).Spans()
		require.Equal(t, 1, spans.Len())
		assert.Equal(t, "keep.span", spans.At(0).Name())
	})
}

func TestEvaluator_LogTransform(t *testing.T) {
	eventID4625 := &policyv1alpha1.LogMatcher{
		Target: &policyv1alpha1.LogFieldSelector{
			Target: &policyv1alpha1.LogFieldSelector_LogAttributePath{
				LogAttributePath: &policyv1alpha1.AttributePath{Segments: []string{"winlog", "event_id"}},
			},
		},
		Predicate: &policyv1alpha1.LogMatcher_Equality{
			Equality: &policyv1alpha1.Value{Kind: &policyv1alpha1.Value_IntValue{IntValue: 4625}},
		},
	}

	// Transform 1a: Add flat log attribute threat_tier when event_id == 4625
	labelSOC := mustPolicy(t, &policyv1alpha1.LogTransformPolicy{
		Id:       "01-label-soc-logs",
		Matchers: []*policyv1alpha1.LogMatcher{eventID4625},
		Action: &policyv1alpha1.LogTransformPolicy_Addition{
			Addition: &policyv1alpha1.LogAddAction{
				Target: &policyv1alpha1.LogTargetSelector{
					Target: &policyv1alpha1.LogTargetSelector_LogAttributePath{
						LogAttributePath: &policyv1alpha1.AttributePath{Segments: []string{"threat_tier"}},
					},
				},
				Value:         &policyv1alpha1.Value{Kind: &policyv1alpha1.Value_StringValue{StringValue: "HOT_ACTIVE_SOC"}},
				UpsertEnabled: proto.Bool(true),
			},
		},
	})

	// Transform 1b: Add nested log attribute gcp.routing.priority when event_id == 4625
	labelSOCPriority := mustPolicy(t, &policyv1alpha1.LogTransformPolicy{
		Id:       "02-label-soc-priority",
		Matchers: []*policyv1alpha1.LogMatcher{eventID4625},
		Action: &policyv1alpha1.LogTransformPolicy_Addition{
			Addition: &policyv1alpha1.LogAddAction{
				Target: &policyv1alpha1.LogTargetSelector{
					Target: &policyv1alpha1.LogTargetSelector_LogAttributePath{
						LogAttributePath: &policyv1alpha1.AttributePath{Segments: []string{"gcp", "routing", "priority"}},
					},
				},
				Value:         &policyv1alpha1.Value{Kind: &policyv1alpha1.Value_IntValue{IntValue: 1}},
				UpsertEnabled: proto.Bool(false),
			},
		},
	})

	// Transform 2: Add default threat_tier = "DEFAULT_TIER" with upsert_enabled: false for all logs with winlog.event_id
	defaultTier := mustPolicy(t, &policyv1alpha1.LogTransformPolicy{
		Id: "03-default-tier",
		Matchers: []*policyv1alpha1.LogMatcher{
			{
				Target: &policyv1alpha1.LogFieldSelector{
					Target: &policyv1alpha1.LogFieldSelector_LogAttributePath{
						LogAttributePath: &policyv1alpha1.AttributePath{Segments: []string{"winlog", "event_id"}},
					},
				},
				Predicate: &policyv1alpha1.LogMatcher_Existence{},
			},
		},
		Action: &policyv1alpha1.LogTransformPolicy_Addition{
			Addition: &policyv1alpha1.LogAddAction{
				Target: &policyv1alpha1.LogTargetSelector{
					Target: &policyv1alpha1.LogTargetSelector_LogAttributePath{
						LogAttributePath: &policyv1alpha1.AttributePath{Segments: []string{"threat_tier"}},
					},
				},
				Value:         &policyv1alpha1.Value{Kind: &policyv1alpha1.Value_StringValue{StringValue: "DEFAULT_TIER"}},
				UpsertEnabled: proto.Bool(false),
			},
		},
	})

	ev, err := NewEvaluator([]googlepolicy.TransformationPolicy{labelSOC, labelSOCPriority, defaultTier})
	require.NoError(t, err)

	ld := plog.NewLogs()
	sl := ld.ResourceLogs().AppendEmpty().ScopeLogs().AppendEmpty()

	// Record 0: event_id = 4625 -> matches labelSOC (sets threat_tier="HOT_ACTIVE_SOC") and labelSOCPriority (sets gcp.routing.priority=1);
	// defaultTier also matches on event_id exists, but upsert=false does NOT overwrite threat_tier!
	lr0 := sl.LogRecords().AppendEmpty()
	lr0.Attributes().PutEmptyMap("winlog").PutInt("event_id", 4625)

	// Record 1: event_id = 4624 -> does not match labelSOC; matches defaultTier -> sets threat_tier="DEFAULT_TIER"
	lr1 := sl.LogRecords().AppendEmpty()
	lr1.Attributes().PutEmptyMap("winlog").PutInt("event_id", 4624)

	// Record 2: no winlog.event_id -> matches no transform policy -> NoMatch
	lr2 := sl.LogRecords().AppendEmpty()
	lr2.Body().SetStr("unrelated log")

	ev.TransformLogs(ld)

	records := ld.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords()
	require.Equal(t, 3, records.Len())

	tier0, ok := records.At(0).Attributes().Get("threat_tier")
	require.True(t, ok)
	assert.Equal(t, "HOT_ACTIVE_SOC", tier0.Str())

	gcpMap, ok := records.At(0).Attributes().Get("gcp")
	require.True(t, ok)
	routingMap, ok := gcpMap.Map().Get("routing")
	require.True(t, ok)
	prio, ok := routingMap.Map().Get("priority")
	require.True(t, ok)
	assert.Equal(t, int64(1), prio.Int())

	tier1, ok := records.At(1).Attributes().Get("threat_tier")
	require.True(t, ok)
	assert.Equal(t, "DEFAULT_TIER", tier1.Str())

	_, ok = records.At(2).Attributes().Get("threat_tier")
	assert.False(t, ok)
}

func TestEvaluator_LogTransformDeterministicOrdering(t *testing.T) {
	// Two add policies: "z-policy" and "a-policy". Both set the same attribute with upsert=false.
	// Both specify empty matchers (applying unconditionally to all records).
	// Since "a-policy" has an alphabetically earlier ID, it must execute before "z-policy",
	// winning the upsert: false race even when passed out of order to NewEvaluator.
	policyZ := mustPolicy(t, &policyv1alpha1.LogTransformPolicy{
		Id: "z-policy",
		Action: &policyv1alpha1.LogTransformPolicy_Addition{
			Addition: &policyv1alpha1.LogAddAction{
				Target: &policyv1alpha1.LogTargetSelector{
					Target: &policyv1alpha1.LogTargetSelector_LogAttributePath{
						LogAttributePath: &policyv1alpha1.AttributePath{Segments: []string{"winner"}},
					},
				},
				Value:         &policyv1alpha1.Value{Kind: &policyv1alpha1.Value_StringValue{StringValue: "z"}},
				UpsertEnabled: proto.Bool(false),
			},
		},
	})

	policyA := mustPolicy(t, &policyv1alpha1.LogTransformPolicy{
		Id: "a-policy",
		Action: &policyv1alpha1.LogTransformPolicy_Addition{
			Addition: &policyv1alpha1.LogAddAction{
				Target: &policyv1alpha1.LogTargetSelector{
					Target: &policyv1alpha1.LogTargetSelector_LogAttributePath{
						LogAttributePath: &policyv1alpha1.AttributePath{Segments: []string{"winner"}},
					},
				},
				Value:         &policyv1alpha1.Value{Kind: &policyv1alpha1.Value_StringValue{StringValue: "a"}},
				UpsertEnabled: proto.Bool(false),
			},
		},
	})

	// Pass out of order: policyZ before policyA
	ev, err := NewEvaluator([]googlepolicy.TransformationPolicy{policyZ, policyA})
	require.NoError(t, err)

	ld := plog.NewLogs()
	lr := ld.ResourceLogs().AppendEmpty().ScopeLogs().AppendEmpty().LogRecords().AppendEmpty()
	lr.Body().SetStr("test ordering")

	ev.TransformLogs(ld)

	val, ok := lr.Attributes().Get("winner")
	require.True(t, ok)
	assert.Equal(t, "a", val.Str(), "a-policy must execute before z-policy due to lexicographical id ordering")
}

func TestEvaluator_LogFilterBeforeTransformOrdering(t *testing.T) {
	// Filter 1 (DROP): Drop DEBUG logs
	dropDebug := mustPolicy(t, &policyv1alpha1.LogFilterPolicy{
		Id:     "drop-debug",
		Action: policyv1alpha1.Action_ACTION_DROP.Enum(),
		Matchers: []*policyv1alpha1.LogMatcher{
			{
				Target: &policyv1alpha1.LogFieldSelector{
					Target: &policyv1alpha1.LogFieldSelector_RecordField{
						RecordField: policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SEVERITY_TEXT,
					},
				},
				Predicate: &policyv1alpha1.LogMatcher_Equality{
					Equality: &policyv1alpha1.Value{Kind: &policyv1alpha1.Value_StringValue{StringValue: "DEBUG"}},
				},
			},
		},
	})

	// Filter 2 (KEEP exemption): Keep logs with log_attribute exempt == true
	keepExempt := mustPolicy(t, &policyv1alpha1.LogFilterPolicy{
		Id:     "keep-exempt",
		Action: policyv1alpha1.Action_ACTION_KEEP.Enum(),
		Matchers: []*policyv1alpha1.LogMatcher{
			{
				Target: &policyv1alpha1.LogFieldSelector{
					Target: &policyv1alpha1.LogFieldSelector_LogAttributePath{
						LogAttributePath: &policyv1alpha1.AttributePath{Segments: []string{"exempt"}},
					},
				},
				Predicate: &policyv1alpha1.LogMatcher_Equality{
					Equality: &policyv1alpha1.Value{Kind: &policyv1alpha1.Value_BoolValue{BoolValue: true}},
				},
			},
		},
	})

	// Transform: Add utr_route = "security_lakehouse" to all logs where event_id == 4625
	addUTRRoute := mustPolicy(t, &policyv1alpha1.LogTransformPolicy{
		Id: "add-utr-route",
		Matchers: []*policyv1alpha1.LogMatcher{
			{
				Target: &policyv1alpha1.LogFieldSelector{
					Target: &policyv1alpha1.LogFieldSelector_LogAttributePath{
						LogAttributePath: &policyv1alpha1.AttributePath{Segments: []string{"event_id"}},
					},
				},
				Predicate: &policyv1alpha1.LogMatcher_Equality{
					Equality: &policyv1alpha1.Value{Kind: &policyv1alpha1.Value_IntValue{IntValue: 4625}},
				},
			},
		},
		Action: &policyv1alpha1.LogTransformPolicy_Addition{
			Addition: &policyv1alpha1.LogAddAction{
				Target: &policyv1alpha1.LogTargetSelector{
					Target: &policyv1alpha1.LogTargetSelector_LogAttributePath{
						LogAttributePath: &policyv1alpha1.AttributePath{Segments: []string{"utr_route"}},
					},
				},
				Value:         &policyv1alpha1.Value{Kind: &policyv1alpha1.Value_StringValue{StringValue: "security_lakehouse"}},
				UpsertEnabled: proto.Bool(true),
			},
		},
	})

	ev, err := NewEvaluator([]googlepolicy.TransformationPolicy{dropDebug, keepExempt, addUTRRoute})
	require.NoError(t, err)

	ld := plog.NewLogs()
	sl := ld.ResourceLogs().AppendEmpty().ScopeLogs().AppendEmpty()

	// Record A: DEBUG + event_id=4625 (not exempt) -> DROPPED in Stage 1; must NOT be transformed in Stage 2!
	lrDropped := sl.LogRecords().AppendEmpty()
	lrDropped.SetSeverityText("DEBUG")
	lrDropped.Attributes().PutInt("event_id", 4625)

	// Record B: DEBUG + exempt=true + event_id=4625 -> KEPT in Stage 1 (keep-exempt overrides drop-debug)
	// AND TRANSFORMED in Stage 2 by add-utr-route!
	lrKeptAndTransformed := sl.LogRecords().AppendEmpty()
	lrKeptAndTransformed.SetSeverityText("DEBUG")
	lrKeptAndTransformed.Attributes().PutBool("exempt", true)
	lrKeptAndTransformed.Attributes().PutInt("event_id", 4625)

	// Record C: DEBUG + exempt=true + event_id=1000 -> KEPT in Stage 1, does not match transform in Stage 2
	lrKeptOnly := sl.LogRecords().AppendEmpty()
	lrKeptOnly.SetSeverityText("DEBUG")
	lrKeptOnly.Attributes().PutBool("exempt", true)
	lrKeptOnly.Attributes().PutInt("event_id", 1000)

	// Record D: INFO + event_id=4625 -> No filter match in Stage 1 (default allow), TRANSFORMED in Stage 2
	lrDefaultAndTransformed := sl.LogRecords().AppendEmpty()
	lrDefaultAndTransformed.SetSeverityText("INFO")
	lrDefaultAndTransformed.Attributes().PutInt("event_id", 4625)

	ev.TransformLogs(ld)

	// Verify surviving records and their attributes
	surviving := ld.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords()
	require.Equal(t, 3, surviving.Len())

	routeB, ok := surviving.At(0).Attributes().Get("utr_route")
	require.True(t, ok)
	assert.Equal(t, "security_lakehouse", routeB.Str())

	_, ok = surviving.At(1).Attributes().Get("utr_route")
	assert.False(t, ok)

	routeD, ok := surviving.At(2).Attributes().Get("utr_route")
	require.True(t, ok)
	assert.Equal(t, "security_lakehouse", routeD.Str())
}

func TestEvaluator_LogTransformBodyFieldAndRecordFields(t *testing.T) {
	adMatcher := []*policyv1alpha1.LogMatcher{
		{
			Target: &policyv1alpha1.LogFieldSelector{
				Target: &policyv1alpha1.LogFieldSelector_ResourceAttributePath{
					ResourceAttributePath: &policyv1alpha1.AttributePath{Segments: []string{"service.name"}},
				},
			},
			Predicate: &policyv1alpha1.LogMatcher_Equality{
				Equality: &policyv1alpha1.Value{Kind: &policyv1alpha1.Value_StringValue{StringValue: "active-directory"}},
			},
		},
	}

	enrichBodyField := mustPolicy(t, &policyv1alpha1.LogTransformPolicy{
		Id:       "01-enrich-body-field",
		Matchers: adMatcher,
		Action: &policyv1alpha1.LogTransformPolicy_Addition{
			Addition: &policyv1alpha1.LogAddAction{
				Target: &policyv1alpha1.LogTargetSelector{
					Target: &policyv1alpha1.LogTargetSelector_BodyFieldPath{
						BodyFieldPath: &policyv1alpha1.AttributePath{Segments: []string{"gcp", "utr", "stream"}},
					},
				},
				Value:         &policyv1alpha1.Value{Kind: &policyv1alpha1.Value_StringValue{StringValue: "secops-ad"}},
				UpsertEnabled: proto.Bool(true),
			},
		},
	})

	enrichSeverity := mustPolicy(t, &policyv1alpha1.LogTransformPolicy{
		Id:       "02-enrich-severity",
		Matchers: adMatcher,
		Action: &policyv1alpha1.LogTransformPolicy_Addition{
			Addition: &policyv1alpha1.LogAddAction{
				Target: &policyv1alpha1.LogTargetSelector{
					Target: &policyv1alpha1.LogTargetSelector_RecordField{
						RecordField: policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SEVERITY_TEXT,
					},
				},
				Value:         &policyv1alpha1.Value{Kind: &policyv1alpha1.Value_StringValue{StringValue: "WARN"}},
				UpsertEnabled: proto.Bool(true),
			},
		},
	})

	enrichLog := mustPolicy(t, &policyv1alpha1.LogTransformPolicy{
		Id:       "03-enrich-log",
		Matchers: adMatcher,
		Action: &policyv1alpha1.LogTransformPolicy_Addition{
			Addition: &policyv1alpha1.LogAddAction{
				Target: &policyv1alpha1.LogTargetSelector{
					Target: &policyv1alpha1.LogTargetSelector_LogAttributePath{
						LogAttributePath: &policyv1alpha1.AttributePath{Segments: []string{"threat_tier"}},
					},
				},
				Value:         &policyv1alpha1.Value{Kind: &policyv1alpha1.Value_StringValue{StringValue: "HOT_ACTIVE_SOC"}},
				UpsertEnabled: proto.Bool(true),
			},
		},
	})

	ev, err := NewEvaluator([]googlepolicy.TransformationPolicy{enrichBodyField, enrichSeverity, enrichLog})
	require.NoError(t, err)

	ld := plog.NewLogs()

	// Resource 0: service.name = "active-directory" (matches)
	rl0 := ld.ResourceLogs().AppendEmpty()
	rl0.Resource().Attributes().PutStr("service.name", "active-directory")
	sl0 := rl0.ScopeLogs().AppendEmpty()
	sl0.Scope().SetName("winlog")
	lr0 := sl0.LogRecords().AppendEmpty()
	lr0.Body().SetEmptyMap()
	lr0.Body().Map().PutStr("event_id", "4625")

	// Resource 1: service.name = "nginx" (does not match)
	rl1 := ld.ResourceLogs().AppendEmpty()
	rl1.Resource().Attributes().PutStr("service.name", "nginx")
	sl1 := rl1.ScopeLogs().AppendEmpty()
	lr1 := sl1.LogRecords().AppendEmpty()
	lr1.Body().SetStr("GET /index.html")

	ev.TransformLogs(ld)

	// Verify Resource 0 record has nested body_field gcp.utr.stream = "secops-ad",
	// severity_text = "WARN", and log_attribute threat_tier = "HOT_ACTIVE_SOC"
	bodyVal, ok := lr0.Body().Map().Get("gcp")
	require.True(t, ok)
	utrVal, ok := bodyVal.Map().Get("utr")
	require.True(t, ok)
	streamVal, ok := utrVal.Map().Get("stream")
	require.True(t, ok)
	assert.Equal(t, "secops-ad", streamVal.Str())

	assert.Equal(t, "WARN", lr0.SeverityText())

	tierAttr, ok := lr0.Attributes().Get("threat_tier")
	require.True(t, ok)
	assert.Equal(t, "HOT_ACTIVE_SOC", tierAttr.Str())

	// Verify Resource 1 was not modified
	assert.Equal(t, pcommon.ValueTypeStr, lr1.Body().Type())
	assert.Equal(t, "GET /index.html", lr1.Body().Str())
	assert.Equal(t, "", lr1.SeverityText())
	assert.Equal(t, 0, lr1.Attributes().Len())
}

func TestEvaluator_LogTransformPostTransformFiltering(t *testing.T) {
	// Filter policy that drops any log record where "threat_tier" == "DROP_ME".
	dropFilter := mustPolicy(t, &policyv1alpha1.LogFilterPolicy{
		Id:     "drop-noise-tier",
		Action: policyv1alpha1.Action_ACTION_DROP.Enum(),
		Matchers: []*policyv1alpha1.LogMatcher{
			{
				Target: &policyv1alpha1.LogFieldSelector{
					Target: &policyv1alpha1.LogFieldSelector_LogAttributePath{
						LogAttributePath: &policyv1alpha1.AttributePath{Segments: []string{"threat_tier"}},
					},
				},
				Predicate: &policyv1alpha1.LogMatcher_Equality{
					Equality: &policyv1alpha1.Value{Kind: &policyv1alpha1.Value_StringValue{StringValue: "DROP_ME"}},
				},
			},
		},
	})

	// Transform policy that adds "threat_tier" = "DROP_ME" when "event_id" == 4634.
	tagNoise := mustPolicy(t, &policyv1alpha1.LogTransformPolicy{
		Id: "tag-noise-event",
		Matchers: []*policyv1alpha1.LogMatcher{
			{
				Target: &policyv1alpha1.LogFieldSelector{
					Target: &policyv1alpha1.LogFieldSelector_LogAttributePath{
						LogAttributePath: &policyv1alpha1.AttributePath{Segments: []string{"event_id"}},
					},
				},
				Predicate: &policyv1alpha1.LogMatcher_Equality{
					Equality: &policyv1alpha1.Value{Kind: &policyv1alpha1.Value_IntValue{IntValue: 4634}},
				},
			},
		},
		Action: &policyv1alpha1.LogTransformPolicy_Addition{
			Addition: &policyv1alpha1.LogAddAction{
				Target: &policyv1alpha1.LogTargetSelector{
					Target: &policyv1alpha1.LogTargetSelector_LogAttributePath{
						LogAttributePath: &policyv1alpha1.AttributePath{Segments: []string{"threat_tier"}},
					},
				},
				Value:         &policyv1alpha1.Value{Kind: &policyv1alpha1.Value_StringValue{StringValue: "DROP_ME"}},
				UpsertEnabled: proto.Bool(true),
			},
		},
	})

	// Transform policy that adds "threat_tier" = "KEEP_ME" when "event_id" == 4625.
	tagImportant := mustPolicy(t, &policyv1alpha1.LogTransformPolicy{
		Id: "tag-important-event",
		Matchers: []*policyv1alpha1.LogMatcher{
			{
				Target: &policyv1alpha1.LogFieldSelector{
					Target: &policyv1alpha1.LogFieldSelector_LogAttributePath{
						LogAttributePath: &policyv1alpha1.AttributePath{Segments: []string{"event_id"}},
					},
				},
				Predicate: &policyv1alpha1.LogMatcher_Equality{
					Equality: &policyv1alpha1.Value{Kind: &policyv1alpha1.Value_IntValue{IntValue: 4625}},
				},
			},
		},
		Action: &policyv1alpha1.LogTransformPolicy_Addition{
			Addition: &policyv1alpha1.LogAddAction{
				Target: &policyv1alpha1.LogTargetSelector{
					Target: &policyv1alpha1.LogTargetSelector_LogAttributePath{
						LogAttributePath: &policyv1alpha1.AttributePath{Segments: []string{"threat_tier"}},
					},
				},
				Value:         &policyv1alpha1.Value{Kind: &policyv1alpha1.Value_StringValue{StringValue: "KEEP_ME"}},
				UpsertEnabled: proto.Bool(true),
			},
		},
	})

	ev, err := NewEvaluator([]googlepolicy.TransformationPolicy{dropFilter, tagNoise, tagImportant})
	require.NoError(t, err)

	ld := plog.NewLogs()
	sl := ld.ResourceLogs().AppendEmpty().ScopeLogs().AppendEmpty()

	// Record 0: event_id = 4634. Has no threat_tier initially, so survives Pass 1.
	// In Stage 2, tagNoise adds threat_tier = "DROP_ME".
	// In Pass 2, dropFilter evaluates against the mutated record and DROPS it.
	lr0 := sl.LogRecords().AppendEmpty()
	lr0.Attributes().PutInt("event_id", 4634)
	lr0.Body().SetStr("logoff event")

	// Record 1: event_id = 4625. Has no threat_tier initially, so survives Pass 1.
	// In Stage 2, tagImportant adds threat_tier = "KEEP_ME".
	// In Pass 2, dropFilter evaluates, does NOT match, and the record is KEPT.
	lr1 := sl.LogRecords().AppendEmpty()
	lr1.Attributes().PutInt("event_id", 4625)
	lr1.Body().SetStr("failed logon event")

	ev.TransformLogs(ld)

	surviving := ld.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords()
	require.Equal(t, 1, surviving.Len())
	assert.Equal(t, "failed logon event", surviving.At(0).Body().Str())
	tier, ok := surviving.At(0).Attributes().Get("threat_tier")
	require.True(t, ok)
	assert.Equal(t, "KEEP_ME", tier.Str())
}

func TestEvaluator_LogTransformRejectUnspecifiedStage(t *testing.T) {
	fake := &fakeUnspecifiedStageTransformPolicy{name: "unspecified-stage"}
	_, err := NewEvaluator([]googlepolicy.TransformationPolicy{fake})
	require.Error(t, err)
	assert.ErrorContains(t, err, "unspecified transform stage")
}

type fakeUnspecifiedStageTransformPolicy struct {
	name string
}

func (f *fakeUnspecifiedStageTransformPolicy) PolicyName() string { return f.name }
func (f *fakeUnspecifiedStageTransformPolicy) PolicyType() string { return "fake" }
func (f *fakeUnspecifiedStageTransformPolicy) PolicyClass() googlepolicy.PolicyClass {
	return googlepolicy.PolicyClassTransformation
}
func (f *fakeUnspecifiedStageTransformPolicy) TargetSignals() []googlepolicy.Signal {
	return []googlepolicy.Signal{googlepolicy.SignalLogs}
}
func (f *fakeUnspecifiedStageTransformPolicy) Validate() error { return nil }
func (f *fakeUnspecifiedStageTransformPolicy) TransformStage() googlepolicy.TransformStage {
	return googlepolicy.TransformStageUnspecified
}
func (f *fakeUnspecifiedStageTransformPolicy) TransformLog(ctx LogContext) googlepolicy.TransformResult {
	return googlepolicy.TransformNoMatch
}

func TestEvaluator_LogTransformPass2Interaction_KeptByAttributeThenRemovedAndDropped(t *testing.T) {
	// ACTION_KEEP on "auth.exempt" == "true".
	keepExempt := mustPolicy(t, &policyv1alpha1.LogFilterPolicy{
		Id:     "keep-auth-exempt",
		Action: policyv1alpha1.Action_ACTION_KEEP.Enum(),
		Matchers: []*policyv1alpha1.LogMatcher{
			{
				Target: &policyv1alpha1.LogFieldSelector{
					Target: &policyv1alpha1.LogFieldSelector_LogAttributePath{
						LogAttributePath: &policyv1alpha1.AttributePath{Segments: []string{"auth.exempt"}},
					},
				},
				Predicate: &policyv1alpha1.LogMatcher_Equality{
					Equality: &policyv1alpha1.Value{Kind: &policyv1alpha1.Value_StringValue{StringValue: "true"}},
				},
			},
		},
	})

	// ACTION_DROP on "env" == "dev".
	dropDev := mustPolicy(t, &policyv1alpha1.LogFilterPolicy{
		Id:     "drop-dev-env",
		Action: policyv1alpha1.Action_ACTION_DROP.Enum(),
		Matchers: []*policyv1alpha1.LogMatcher{
			{
				Target: &policyv1alpha1.LogFieldSelector{
					Target: &policyv1alpha1.LogFieldSelector_LogAttributePath{
						LogAttributePath: &policyv1alpha1.AttributePath{Segments: []string{"env"}},
					},
				},
				Predicate: &policyv1alpha1.LogMatcher_Equality{
					Equality: &policyv1alpha1.Value{Kind: &policyv1alpha1.Value_StringValue{StringValue: "dev"}},
				},
			},
		},
	})

	// LogRemoveAction removes "auth.exempt".
	removeExempt := mustPolicy(t, &policyv1alpha1.LogTransformPolicy{
		Id: "remove-exempt-attr",
		Action: &policyv1alpha1.LogTransformPolicy_Removal{
			Removal: &policyv1alpha1.LogRemoveAction{
				Target: &policyv1alpha1.LogAttributeSelector{
					Target: &policyv1alpha1.LogAttributeSelector_LogAttributePath{
						LogAttributePath: &policyv1alpha1.AttributePath{Segments: []string{"auth.exempt"}},
					},
				},
			},
		},
	})

	ev, err := NewEvaluator([]googlepolicy.TransformationPolicy{keepExempt, dropDev, removeExempt})
	require.NoError(t, err)

	ld := plog.NewLogs()
	sl := ld.ResourceLogs().AppendEmpty().ScopeLogs().AppendEmpty()

	// In Pass 1: Record has both auth.exempt = "true" and env = "dev".
	// ACTION_KEEP wins over ACTION_DROP, so the record survives Pass 1.
	// In Transform: removeExempt strips "auth.exempt", modifying the record.
	// In Pass 2: Re-evaluation occurs without "auth.exempt". ACTION_KEEP no longer matches,
	// so ACTION_DROP on env = "dev" drops the record.
	lr := sl.LogRecords().AppendEmpty()
	lr.Attributes().PutStr("auth.exempt", "true")
	lr.Attributes().PutStr("env", "dev")
	lr.Body().SetStr("sensitive transaction")

	ev.TransformLogs(ld)

	// Entire scope and resource logs should be pruned because all records were dropped.
	assert.Equal(t, 0, ld.ResourceLogs().Len())
}

func TestEvaluator_LogTransformNonMatchingMatcherLeavesTargetIntact(t *testing.T) {
	removeIfAdmin := mustPolicy(t, &policyv1alpha1.LogTransformPolicy{
		Id: "remove-if-admin",
		Matchers: []*policyv1alpha1.LogMatcher{
			{
				Target: &policyv1alpha1.LogFieldSelector{
					Target: &policyv1alpha1.LogFieldSelector_LogAttributePath{
						LogAttributePath: &policyv1alpha1.AttributePath{Segments: []string{"user.role"}},
					},
				},
				Predicate: &policyv1alpha1.LogMatcher_Equality{
					Equality: &policyv1alpha1.Value{Kind: &policyv1alpha1.Value_StringValue{StringValue: "admin"}},
				},
			},
		},
		Action: &policyv1alpha1.LogTransformPolicy_Removal{
			Removal: &policyv1alpha1.LogRemoveAction{
				Target: &policyv1alpha1.LogAttributeSelector{
					Target: &policyv1alpha1.LogAttributeSelector_LogAttributePath{
						LogAttributePath: &policyv1alpha1.AttributePath{Segments: []string{"temp.secret"}},
					},
				},
			},
		},
	})

	ev, err := NewEvaluator([]googlepolicy.TransformationPolicy{removeIfAdmin})
	require.NoError(t, err)

	ld := plog.NewLogs()
	sl := ld.ResourceLogs().AppendEmpty().ScopeLogs().AppendEmpty()
	lr := sl.LogRecords().AppendEmpty()
	lr.Attributes().PutStr("user.role", "viewer")
	lr.Attributes().PutStr("temp.secret", "supersecret")

	ev.TransformLogs(ld)

	require.Equal(t, 1, ld.ResourceLogs().Len())
	surviving := ld.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0)
	val, ok := surviving.Attributes().Get("temp.secret")
	require.True(t, ok)
	assert.Equal(t, "supersecret", val.Str())
}

func TestEvaluator_LogTransformStageOrdering(t *testing.T) {
	logTarget := func(path ...string) *policyv1alpha1.LogTargetSelector {
		return &policyv1alpha1.LogTargetSelector{
			Target: &policyv1alpha1.LogTargetSelector_LogAttributePath{
				LogAttributePath: &policyv1alpha1.AttributePath{Segments: path},
			},
		}
	}
	logAttr := func(path ...string) *policyv1alpha1.LogAttributeSelector {
		return &policyv1alpha1.LogAttributeSelector{
			Target: &policyv1alpha1.LogAttributeSelector_LogAttributePath{
				LogAttributePath: &policyv1alpha1.AttributePath{Segments: path},
			},
		}
	}

	// Give the remove stage policy a lexicographically EARLIER ID ("b-remove-temp")
	// than the add stage policies, and the rename stage policy a lexicographically
	// LAST ID ("z-rename-legacy") to prove that stage ordering (rename -> add -> remove)
	// takes precedence over ID order, while ID order breaks ties within the same stage.
	renameLegacy := mustPolicy(t, &policyv1alpha1.LogTransformPolicy{
		Id: "z-rename-legacy",
		Action: &policyv1alpha1.LogTransformPolicy_Renaming{
			Renaming: &policyv1alpha1.LogRenameAction{
				Source:        logAttr("legacy_user"),
				Target:        logAttr("user_name"),
				UpsertEnabled: proto.Bool(false),
			},
		},
	})

	removeTemp := mustPolicy(t, &policyv1alpha1.LogTransformPolicy{
		Id: "b-remove-temp",
		Action: &policyv1alpha1.LogTransformPolicy_Removal{
			Removal: &policyv1alpha1.LogRemoveAction{
				Target: logAttr("temp_flag"),
			},
		},
	})

	// Add policy with fallback: adds "user_name" = "guest" if not present.
	// Since "z-rename-legacy" runs in Stage 1 before add policies in Stage 2,
	// "user_name" will already exist, so "m-add-fallback" will skip it.
	addFallback := mustPolicy(t, &policyv1alpha1.LogTransformPolicy{
		Id: "m-add-fallback",
		Action: &policyv1alpha1.LogTransformPolicy_Addition{
			Addition: &policyv1alpha1.LogAddAction{
				Target:        logTarget("user_name"),
				Value:         &policyv1alpha1.Value{Kind: &policyv1alpha1.Value_StringValue{StringValue: "guest"}},
				UpsertEnabled: proto.Bool(false),
			},
		},
	})

	// Two add policies in the same stage: "m-add-1" runs before "m-add-2".
	// Both have upsert=false on "stage_winner", so "m-add-1" must win.
	addWinner1 := mustPolicy(t, &policyv1alpha1.LogTransformPolicy{
		Id: "m-add-1",
		Action: &policyv1alpha1.LogTransformPolicy_Addition{
			Addition: &policyv1alpha1.LogAddAction{
				Target:        logTarget("stage_winner"),
				Value:         &policyv1alpha1.Value{Kind: &policyv1alpha1.Value_StringValue{StringValue: "first"}},
				UpsertEnabled: proto.Bool(false),
			},
		},
	})
	addWinner2 := mustPolicy(t, &policyv1alpha1.LogTransformPolicy{
		Id: "m-add-2",
		Action: &policyv1alpha1.LogTransformPolicy_Addition{
			Addition: &policyv1alpha1.LogAddAction{
				Target:        logTarget("stage_winner"),
				Value:         &policyv1alpha1.Value{Kind: &policyv1alpha1.Value_StringValue{StringValue: "second"}},
				UpsertEnabled: proto.Bool(false),
			},
		},
	})

	// Add policy that sets "temp_flag" = "to-be-removed" in Stage 2, which is then
	// removed in Stage 3 by "b-remove-temp" (even though "b-remove-temp" < "n-add-temp" lexicographically).
	addTemp := mustPolicy(t, &policyv1alpha1.LogTransformPolicy{
		Id: "n-add-temp",
		Action: &policyv1alpha1.LogTransformPolicy_Addition{
			Addition: &policyv1alpha1.LogAddAction{
				Target:        logTarget("temp_flag"),
				Value:         &policyv1alpha1.Value{Kind: &policyv1alpha1.Value_StringValue{StringValue: "to-be-removed"}},
				UpsertEnabled: proto.Bool(true),
			},
		},
	})

	// Redact policy that masks "user_name" in Stage 4: replaces "alice" with "[REDACTED_USER]".
	// It has the lexicographically FIRST ID ("a-redact-user") among all policies, proving that
	// Stage 4 (redact) runs LAST after rename (Stage 1), add (Stage 2), and remove (Stage 3).
	redactUser := mustPolicy(t, &policyv1alpha1.LogTransformPolicy{
		Id: "a-redact-user",
		Action: &policyv1alpha1.LogTransformPolicy_Redaction{
			Redaction: &policyv1alpha1.LogRedactAction{
				Target:      logTarget("user_name"),
				Replacement: proto.String("[REDACTED_USER]"),
			},
		},
	})

	ev, err := NewEvaluator([]googlepolicy.TransformationPolicy{
		removeTemp,
		renameLegacy,
		addWinner2,
		addWinner1,
		addTemp,
		addFallback,
		redactUser,
	})
	require.NoError(t, err)

	ld := plog.NewLogs()
	sl := ld.ResourceLogs().AppendEmpty().ScopeLogs().AppendEmpty()
	lr0 := sl.LogRecords().AppendEmpty()
	lr0.Body().SetStr("hello")
	lr0.Attributes().PutStr("legacy_user", "alice")

	ev.TransformLogs(ld)

	records := ld.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords()
	require.Equal(t, 1, records.Len())

	_, ok := records.At(0).Attributes().Get("legacy_user")
	assert.False(t, ok, "legacy_user must be renamed")

	userName, ok := records.At(0).Attributes().Get("user_name")
	require.True(t, ok)
	assert.Equal(t, "[REDACTED_USER]", userName.Str(), "rename in Stage 1 moved value to user_name, which was then redacted in Stage 4")

	winner0, ok := records.At(0).Attributes().Get("stage_winner")
	require.True(t, ok)
	assert.Equal(t, "first", winner0.Str(), "m-add-1 must run before m-add-2")

	_, ok = records.At(0).Attributes().Get("temp_flag")
	assert.False(t, ok, "temp_flag added in Stage 2 must be removed in Stage 3")
}

// --- policy helpers --------------------------------------------------------

// mustPolicy compiles a filter or transform policy proto with its owning
// package and returns the resulting TransformationPolicy. This is exactly what
// the googlepolicy registry hands NewEvaluator in production: an
// already-compiled policy that implements the matching per-signal evaluator
// interface.
func mustPolicy(t *testing.T, pb proto.Message) googlepolicy.TransformationPolicy {
	t.Helper()
	switch p := pb.(type) {
	case *policyv1alpha1.LogFilterPolicy:
		pol, err := logfilter.NewPolicyFromProto(p)
		require.NoError(t, err)
		return pol
	case *policyv1alpha1.LogTransformPolicy:
		pol, err := logtransform.NewPolicyFromProto(p)
		require.NoError(t, err)
		return pol
	case *policyv1alpha1.MetricFilterPolicy:
		pol, err := metricfilter.NewPolicyFromProto(p)
		require.NoError(t, err)
		return pol
	case *policyv1alpha1.TraceFilterPolicy:
		pol, err := tracefilter.NewPolicyFromProto(p)
		require.NoError(t, err)
		return pol
	default:
		t.Fatalf("mustPolicy: unsupported policy proto %T", pb)
		return nil
	}
}

// --- policy proto builders -------------------------------------------------

func newDropLogBodyProto(id, body string) *policyv1alpha1.LogFilterPolicy {
	return &policyv1alpha1.LogFilterPolicy{
		Id:     id,
		Action: policyv1alpha1.Action_ACTION_DROP.Enum(),
		Matchers: []*policyv1alpha1.LogMatcher{
			{
				Target: &policyv1alpha1.LogFieldSelector{
					Target: &policyv1alpha1.LogFieldSelector_RecordField{
						RecordField: policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_BODY,
					},
				},
				Predicate: &policyv1alpha1.LogMatcher_Equality{
					Equality: &policyv1alpha1.Value{Kind: &policyv1alpha1.Value_StringValue{StringValue: body}},
				},
			},
		},
	}
}

func newDropMetricDescriptorProto(id string, field policyv1alpha1.MetricDescriptorField, value string) *policyv1alpha1.MetricFilterPolicy {
	return &policyv1alpha1.MetricFilterPolicy{
		Id:     id,
		Action: policyv1alpha1.Action_ACTION_DROP.Enum(),
		Matchers: []*policyv1alpha1.MetricMatcher{
			{
				Target: &policyv1alpha1.MetricFieldSelector{
					Target: &policyv1alpha1.MetricFieldSelector_DescriptorField{DescriptorField: field},
				},
				Predicate: &policyv1alpha1.MetricMatcher_Equality{
					Equality: &policyv1alpha1.Value{Kind: &policyv1alpha1.Value_StringValue{StringValue: value}},
				},
			},
		},
	}
}

func newDropMetricNameProto(id, name string) *policyv1alpha1.MetricFilterPolicy {
	return newDropMetricDescriptorProto(id, policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_NAME, name)
}

func newDropDatapointAttrProto(id, key, value string) *policyv1alpha1.MetricFilterPolicy {
	return &policyv1alpha1.MetricFilterPolicy{
		Id:     id,
		Action: policyv1alpha1.Action_ACTION_DROP.Enum(),
		Matchers: []*policyv1alpha1.MetricMatcher{
			{
				Target: &policyv1alpha1.MetricFieldSelector{
					Target: &policyv1alpha1.MetricFieldSelector_DataPointAttributePath{
						DataPointAttributePath: &policyv1alpha1.AttributePath{Segments: []string{key}},
					},
				},
				Predicate: &policyv1alpha1.MetricMatcher_Equality{
					Equality: &policyv1alpha1.Value{Kind: &policyv1alpha1.Value_StringValue{StringValue: value}},
				},
			},
		},
	}
}

func newDropSpanNameProto(id, name string) *policyv1alpha1.TraceFilterPolicy {
	return &policyv1alpha1.TraceFilterPolicy{
		Id: id,
		Matchers: []*policyv1alpha1.TraceMatcher{
			{
				Target: &policyv1alpha1.TraceFieldSelector{
					Target: &policyv1alpha1.TraceFieldSelector_RecordField{
						RecordField: policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_NAME,
					},
				},
				Predicate: &policyv1alpha1.TraceMatcher_Equality{
					Equality: &policyv1alpha1.Value{Kind: &policyv1alpha1.Value_StringValue{StringValue: name}},
				},
			},
		},
	}
}

// --- pdata builders --------------------------------------------------------

// appendGauge appends a gauge instrument carrying a single datapoint. An
// instrument with no type set is skipped by transformMetricDataPoints before
// any policy runs, so fixtures must always pick a concrete type.
func appendGauge(sm pmetric.ScopeMetrics, name string) pmetric.Metric {
	m := sm.Metrics().AppendEmpty()
	m.SetName(name)
	m.SetEmptyGauge().DataPoints().AppendEmpty()
	return m
}

// newAllMetricTypes returns a batch holding one instrument of each pmetric
// type, each with a single datapoint. Sum and Histogram are CUMULATIVE,
// ExponentialHistogram is DELTA; Gauge and Summary have no temporality.
func newAllMetricTypes() pmetric.Metrics {
	md := pmetric.NewMetrics()
	sm := md.ResourceMetrics().AppendEmpty().ScopeMetrics().AppendEmpty()

	gauge := sm.Metrics().AppendEmpty()
	gauge.SetName("gauge.metric")
	gauge.SetEmptyGauge().DataPoints().AppendEmpty()

	sum := sm.Metrics().AppendEmpty()
	sum.SetName("sum.metric")
	sumData := sum.SetEmptySum()
	sumData.SetAggregationTemporality(pmetric.AggregationTemporalityCumulative)
	sumData.DataPoints().AppendEmpty()

	hist := sm.Metrics().AppendEmpty()
	hist.SetName("histogram.metric")
	histData := hist.SetEmptyHistogram()
	histData.SetAggregationTemporality(pmetric.AggregationTemporalityCumulative)
	histData.DataPoints().AppendEmpty()

	expHist := sm.Metrics().AppendEmpty()
	expHist.SetName("exphistogram.metric")
	expHistData := expHist.SetEmptyExponentialHistogram()
	expHistData.SetAggregationTemporality(pmetric.AggregationTemporalityDelta)
	expHistData.DataPoints().AppendEmpty()

	summary := sm.Metrics().AppendEmpty()
	summary.SetName("summary.metric")
	summary.SetEmptySummary().DataPoints().AppendEmpty()

	return md
}

// metricNames flattens every surviving instrument name in a batch.
func metricNames(md pmetric.Metrics) []string {
	var names []string
	for i := 0; i < md.ResourceMetrics().Len(); i++ {
		sms := md.ResourceMetrics().At(i).ScopeMetrics()
		for j := 0; j < sms.Len(); j++ {
			ms := sms.At(j).Metrics()
			for k := 0; k < ms.Len(); k++ {
				names = append(names, ms.At(k).Name())
			}
		}
	}
	return names
}
