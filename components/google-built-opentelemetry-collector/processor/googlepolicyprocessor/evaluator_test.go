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

// fakeMetricPolicy is a hand-written MetricPolicyEvaluator used to observe how
// often, and with which datapoint attributes, the Evaluator invokes a metric
// policy. It is the only way to assert the two-tier instrument-vs-datapoint
// optimisation from outside the package.
type fakeMetricPolicy struct {
	name           string
	datapointLevel bool
	result         googlepolicy.EvalResult
	// eval, when set, overrides result.
	eval func(ctx googlepolicy.MetricContext) googlepolicy.EvalResult

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
func (p *fakeMetricPolicy) EvaluateMetric(ctx googlepolicy.MetricContext) googlepolicy.EvalResult {
	p.calls++
	p.seenDatapointAttrs = append(p.seenDatapointAttrs, ctx.DatapointAttributes.AsRaw())
	if p.eval != nil {
		return p.eval(ctx)
	}
	return p.result
}

// fakeLogPolicy is a hand-written LogPolicyEvaluator that is not a
// logfilter.Policy, pinning that NewEvaluator dispatches on the evaluator
// interface rather than on a concrete filter-package type.
type fakeLogPolicy struct {
	result googlepolicy.EvalResult
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
func (p *fakeLogPolicy) EvaluateLog(googlepolicy.LogContext) googlepolicy.EvalResult {
	return p.result
}

func TestEvaluator_LogFiltering(t *testing.T) {
	// Policy 1: Drop logs where severity_text regex matches "^DEBUG"
	dropDebug := mustPolicy(t, &policyv1alpha1.LogFilterPolicy{
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
	})

	// Policy 2: Keep logs (exemption) where resource attribute env == "production"
	keepProd := mustPolicy(t, &policyv1alpha1.LogFilterPolicy{
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
		assert.Empty(t, ev.logPolicies)
		assert.Empty(t, ev.metricPolicies)
		assert.Empty(t, ev.tracePolicies)
	})

	t.Run("compiled policies are routed to their signal", func(t *testing.T) {
		ev, err := NewEvaluator([]googlepolicy.TransformationPolicy{
			mustPolicy(t, newDropLogBodyProto("pre-log", "BAD")),
			mustPolicy(t, newDropMetricNameProto("pre-metric", "drop.me")),
			mustPolicy(t, newDropSpanNameProto("pre-trace", "drop.span")),
		})
		require.NoError(t, err)
		require.Len(t, ev.logPolicies, 1)
		require.Len(t, ev.metricPolicies, 1)
		require.Len(t, ev.tracePolicies, 1)

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
		// LogPolicyEvaluator is all it takes to land on the log path.
		ev, err := NewEvaluator([]googlepolicy.TransformationPolicy{
			&fakeLogPolicy{result: googlepolicy.EvalDrop},
		})
		require.NoError(t, err)
		require.Len(t, ev.logPolicies, 1)
		assert.True(t, ev.EvalLog(LogContext{Record: plog.NewLogRecord()}))
	})

	t.Run("datapoint-level metric policies are flagged", func(t *testing.T) {
		instrumentOnly, err := NewEvaluator([]googlepolicy.TransformationPolicy{
			mustPolicy(t, newDropMetricNameProto("by-name", "drop.me")),
		})
		require.NoError(t, err)
		assert.False(t, instrumentOnly.hasDatapointMetricPolicies)

		withDatapoint, err := NewEvaluator([]googlepolicy.TransformationPolicy{
			mustPolicy(t, newDropMetricNameProto("by-name", "drop.me")),
			mustPolicy(t, newDropDatapointAttrProto("by-attr", "drop_me", "true")),
		})
		require.NoError(t, err)
		assert.True(t, withDatapoint.hasDatapointMetricPolicies)
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
	assert.ErrorContains(t, err, "googlepolicy.LogPolicyEvaluator")
	assert.ErrorContains(t, err, "googlepolicy.MetricPolicyEvaluator")
	assert.ErrorContains(t, err, "googlepolicy.TracePolicyEvaluator")
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
	})

	ev, err := NewEvaluator([]googlepolicy.TransformationPolicy{dropDpPolicy, keepInstrumentPolicy})
	require.NoError(t, err)
	require.True(t, ev.hasDatapointMetricPolicies)

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
			require.False(t, ev.hasDatapointMetricPolicies)

			md := newAllMetricTypes()
			ev.TransformMetrics(md)
			assert.Equal(t, tc.expected, metricNames(md))
		})
	}

	t.Run("unset instrument type never matches", func(t *testing.T) {
		ev, err := NewEvaluator([]googlepolicy.TransformationPolicy{
			mustPolicy(t, newDropMetricDescriptorProto("drop-unspecified",
				policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_TYPE, "UNSPECIFIED")),
		})
		require.NoError(t, err)

		md := pmetric.NewMetrics()
		sm := md.ResourceMetrics().AppendEmpty().ScopeMetrics().AppendEmpty()
		sm.Metrics().AppendEmpty().SetName("unset.type")

		ev.TransformMetrics(md)
		// transformMetricDataPoints bails out on MetricTypeEmpty before any
		// policy runs, so the instrument survives regardless of the policy.
		assert.Equal(t, []string{"unset.type"}, metricNames(md))
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
		fake := &fakeMetricPolicy{name: "instrument", result: googlepolicy.EvalDrop}
		ev, err := NewEvaluator([]googlepolicy.TransformationPolicy{fake})
		require.NoError(t, err)
		require.False(t, ev.hasDatapointMetricPolicies)

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
			eval: func(ctx googlepolicy.MetricContext) googlepolicy.EvalResult {
				if v, ok := ctx.DatapointAttributes.Get("dp"); ok && v.Str() != "keep" {
					return googlepolicy.EvalDrop
				}
				return googlepolicy.EvalNoMatch
			},
		}
		ev, err := NewEvaluator([]googlepolicy.TransformationPolicy{fake})
		require.NoError(t, err)
		require.True(t, ev.hasDatapointMetricPolicies)

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
		keep := &fakeMetricPolicy{name: "keep", result: googlepolicy.EvalKeep}
		drop := &fakeMetricPolicy{name: "drop", datapointLevel: true, result: googlepolicy.EvalDrop}
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
		drop := &fakeMetricPolicy{name: "drop", datapointLevel: true, result: googlepolicy.EvalDrop}
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
}

// TestEvaluator_TraceKeepOverridesDrop covers the KEEP-as-exemption rule on the
// trace path, including the root-span (empty parent_span_id) case.
func TestEvaluator_TraceKeepOverridesDrop(t *testing.T) {
	dropRootSpans := mustPolicy(t, &policyv1alpha1.TraceFilterPolicy{
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
	})

	keepOkSpans := mustPolicy(t, &policyv1alpha1.TraceFilterPolicy{
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
	})

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
	childSpan.SetName("child")
	childSpan.SetParentSpanID([8]byte{1, 2, 3, 4, 5, 6, 7, 8})
	assert.False(t, ev.EvalTrace(TraceContext{Span: childSpan}))

	// Same three spans through the batch walk.
	td := ptrace.NewTraces()
	ss := td.ResourceSpans().AppendEmpty().ScopeSpans().AppendEmpty()
	rootSpan.CopyTo(ss.Spans().AppendEmpty())
	rootSpanOk.CopyTo(ss.Spans().AppendEmpty())
	childSpan.CopyTo(ss.Spans().AppendEmpty())

	ev.TransformTraces(td)
	require.Equal(t, 1, td.ResourceSpans().Len())
	spans := td.ResourceSpans().At(0).ScopeSpans().At(0).Spans()
	require.Equal(t, 2, spans.Len())
	assert.Equal(t, "root-ok", spans.At(0).Name())
	assert.Equal(t, "child", spans.At(1).Name())
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

// --- policy helpers --------------------------------------------------------

// mustPolicy compiles a filter policy proto with its owning filter package and
// returns the resulting TransformationPolicy. This is exactly what the
// googlepolicy registry hands NewEvaluator in production: an already-compiled
// policy that implements the matching per-signal evaluator interface.
func mustPolicy(t *testing.T, pb proto.Message) googlepolicy.TransformationPolicy {
	t.Helper()
	switch p := pb.(type) {
	case *policyv1alpha1.LogFilterPolicy:
		pol, err := logfilter.NewPolicyFromProto(p)
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
		Matches: []*policyv1alpha1.LogMatcher{
			{
				Target: &policyv1alpha1.LogFieldSelector{
					Target: &policyv1alpha1.LogFieldSelector_RecordField{
						RecordField: policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_BODY,
					},
				},
				Predicate: &policyv1alpha1.LogMatcher_Equals{
					Equals: &policyv1alpha1.Value{Value: &policyv1alpha1.Value_StringValue{StringValue: body}},
				},
			},
		},
	}
}

func newDropMetricDescriptorProto(id string, field policyv1alpha1.MetricDescriptorField, value string) *policyv1alpha1.MetricFilterPolicy {
	return &policyv1alpha1.MetricFilterPolicy{
		Id:     id,
		Action: policyv1alpha1.Action_ACTION_DROP.Enum(),
		Matches: []*policyv1alpha1.MetricMatcher{
			{
				Target: &policyv1alpha1.MetricFieldSelector{
					Target: &policyv1alpha1.MetricFieldSelector_DescriptorField{DescriptorField: field},
				},
				Predicate: &policyv1alpha1.MetricMatcher_Equals{
					Equals: &policyv1alpha1.Value{Value: &policyv1alpha1.Value_StringValue{StringValue: value}},
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
		Matches: []*policyv1alpha1.MetricMatcher{
			{
				Target: &policyv1alpha1.MetricFieldSelector{
					Target: &policyv1alpha1.MetricFieldSelector_DatapointAttribute{
						DatapointAttribute: &policyv1alpha1.AttributePath{Path: []string{key}},
					},
				},
				Predicate: &policyv1alpha1.MetricMatcher_Equals{
					Equals: &policyv1alpha1.Value{Value: &policyv1alpha1.Value_StringValue{StringValue: value}},
				},
			},
		},
	}
}

func newDropSpanNameProto(id, name string) *policyv1alpha1.TraceFilterPolicy {
	return &policyv1alpha1.TraceFilterPolicy{
		Id:     id,
		Action: policyv1alpha1.Action_ACTION_DROP.Enum(),
		Matches: []*policyv1alpha1.TraceMatcher{
			{
				Target: &policyv1alpha1.TraceFieldSelector{
					Target: &policyv1alpha1.TraceFieldSelector_RecordField{
						RecordField: policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_NAME,
					},
				},
				Predicate: &policyv1alpha1.TraceMatcher_Equals{
					Equals: &policyv1alpha1.Value{Value: &policyv1alpha1.Value_StringValue{StringValue: name}},
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
