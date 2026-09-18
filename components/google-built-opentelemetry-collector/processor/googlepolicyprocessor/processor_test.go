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
	"context"
	"testing"
	"time"

	policyv1alpha1 "github.com/GoogleCloudPlatform/opentelemetry-operations-collector/gen/go/policy/v1alpha1"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/component/componenttest"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.uber.org/zap"

	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy/logfilter"
)

func TestProcessTraces_NilEngine(t *testing.T) {
	p := newGooglePolicyProcessor(&Config{}, zap.NewNop())
	td := ptrace.NewTraces()
	rs := td.ResourceSpans().AppendEmpty()
	rs.ScopeSpans().AppendEmpty().Spans().AppendEmpty()

	out, err := p.processTraces(context.Background(), td)
	require.NoError(t, err)
	assert.Equal(t, td, out)
}

func TestProcessMetrics_NilEngine(t *testing.T) {
	p := newGooglePolicyProcessor(&Config{}, zap.NewNop())
	md := pmetric.NewMetrics()
	rm := md.ResourceMetrics().AppendEmpty()
	rm.ScopeMetrics().AppendEmpty().Metrics().AppendEmpty()

	out, err := p.processMetrics(context.Background(), md)
	require.NoError(t, err)
	assert.Equal(t, md, out)
}

func TestProcessLogs_NilEngine(t *testing.T) {
	p := newGooglePolicyProcessor(&Config{}, zap.NewNop())
	ld := plog.NewLogs()
	rl := ld.ResourceLogs().AppendEmpty()
	rl.ScopeLogs().AppendEmpty().LogRecords().AppendEmpty()

	out, err := p.processLogs(context.Background(), ld)
	require.NoError(t, err)
	assert.Equal(t, ld, out)
}

func TestProcessLogs_WithActivePolicy(t *testing.T) {
	dropPolicy := mustPolicy(t, &policyv1alpha1.LogFilterPolicy{
		Id:     "drop-secret-logs",
		Action: policyv1alpha1.Action_ACTION_DROP.Enum(),
		Matches: []*policyv1alpha1.LogMatcher{
			{
				Target: &policyv1alpha1.LogFieldSelector{
					Target: &policyv1alpha1.LogFieldSelector_RecordField{
						RecordField: policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_BODY,
					},
				},
				Predicate: &policyv1alpha1.LogMatcher_Equals{
					Equals: &policyv1alpha1.Value{
						Value: &policyv1alpha1.Value_StringValue{StringValue: "secret message"},
					},
				},
			},
		},
	})

	ps := &googlepolicy.PolicySet{
		RevisionID: "rev-1",
		Policies: map[string]*googlepolicy.PolicySetEntry{
			"drop-secret-logs": {
				PolicyObj: dropPolicy,
			},
		},
	}
	googlepolicy.SetActivePolicySet(ps)
	defer googlepolicy.SetActivePolicySet(nil)

	p := newGooglePolicyProcessor(&Config{}, zap.NewNop())
	require.NoError(t, p.start(context.Background(), componenttest.NewNopHost()))
	defer func() { _ = p.shutdown(context.Background()) }()

	ld := plog.NewLogs()
	rl := ld.ResourceLogs().AppendEmpty()
	sl := rl.ScopeLogs().AppendEmpty()

	lr1 := sl.LogRecords().AppendEmpty()
	lr1.Body().SetStr("public message")

	lr2 := sl.LogRecords().AppendEmpty()
	lr2.Body().SetStr("secret message")

	out, err := p.processLogs(context.Background(), ld)
	require.NoError(t, err)

	require.Equal(t, 1, out.ResourceLogs().Len())
	require.Equal(t, 1, out.ResourceLogs().At(0).ScopeLogs().Len())
	records := out.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords()
	require.Equal(t, 1, records.Len())
	assert.Equal(t, "public message", records.At(0).Body().AsString())
}

func TestProcessMetrics_WithActivePolicy(t *testing.T) {
	dropPolicy := mustPolicy(t, &policyv1alpha1.MetricFilterPolicy{
		Id:     "drop-internal-metric",
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
						Value: &policyv1alpha1.Value_StringValue{StringValue: "internal.heartbeat"},
					},
				},
			},
		},
	})

	ps := &googlepolicy.PolicySet{
		RevisionID: "rev-metrics-1",
		Policies: map[string]*googlepolicy.PolicySetEntry{
			"drop-internal-metric": {
				PolicyObj: dropPolicy,
			},
		},
	}
	googlepolicy.SetActivePolicySet(ps)
	defer googlepolicy.SetActivePolicySet(nil)

	p := newGooglePolicyProcessor(&Config{}, zap.NewNop())
	require.NoError(t, p.start(context.Background(), componenttest.NewNopHost()))
	defer func() { _ = p.shutdown(context.Background()) }()

	md := pmetric.NewMetrics()
	rm := md.ResourceMetrics().AppendEmpty()
	sm := rm.ScopeMetrics().AppendEmpty()

	m1 := sm.Metrics().AppendEmpty()
	m1.SetName("user.requests")
	m1.SetEmptyGauge().DataPoints().AppendEmpty()

	m2 := sm.Metrics().AppendEmpty()
	m2.SetName("internal.heartbeat")
	m2.SetEmptyGauge().DataPoints().AppendEmpty()

	out, err := p.processMetrics(context.Background(), md)
	require.NoError(t, err)

	require.Equal(t, 1, out.ResourceMetrics().Len())
	require.Equal(t, 1, out.ResourceMetrics().At(0).ScopeMetrics().Len())
	metrics := out.ResourceMetrics().At(0).ScopeMetrics().At(0).Metrics()
	require.Equal(t, 1, metrics.Len())
	assert.Equal(t, "user.requests", metrics.At(0).Name())
}

func TestProcessTraces_WithActivePolicy(t *testing.T) {
	dropPolicy := mustPolicy(t, &policyv1alpha1.TraceFilterPolicy{
		Id:     "drop-healthcheck-span",
		Action: policyv1alpha1.Action_ACTION_DROP.Enum(),
		Matches: []*policyv1alpha1.TraceMatcher{
			{
				Target: &policyv1alpha1.TraceFieldSelector{
					Target: &policyv1alpha1.TraceFieldSelector_RecordField{
						RecordField: policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_NAME,
					},
				},
				Predicate: &policyv1alpha1.TraceMatcher_Equals{
					Equals: &policyv1alpha1.Value{
						Value: &policyv1alpha1.Value_StringValue{StringValue: "/healthz"},
					},
				},
			},
		},
	})

	ps := &googlepolicy.PolicySet{
		RevisionID: "rev-traces-1",
		Policies: map[string]*googlepolicy.PolicySetEntry{
			"drop-healthcheck-span": {
				PolicyObj: dropPolicy,
			},
		},
	}
	googlepolicy.SetActivePolicySet(ps)
	defer googlepolicy.SetActivePolicySet(nil)

	p := newGooglePolicyProcessor(&Config{}, zap.NewNop())
	require.NoError(t, p.start(context.Background(), componenttest.NewNopHost()))
	defer func() { _ = p.shutdown(context.Background()) }()

	td := ptrace.NewTraces()
	rs := td.ResourceSpans().AppendEmpty()
	ss := rs.ScopeSpans().AppendEmpty()

	s1 := ss.Spans().AppendEmpty()
	s1.SetName("/api/v1/checkout")

	s2 := ss.Spans().AppendEmpty()
	s2.SetName("/healthz")

	out, err := p.processTraces(context.Background(), td)
	require.NoError(t, err)

	require.Equal(t, 1, out.ResourceSpans().Len())
	require.Equal(t, 1, out.ResourceSpans().At(0).ScopeSpans().Len())
	spans := out.ResourceSpans().At(0).ScopeSpans().At(0).Spans()
	require.Equal(t, 1, spans.Len())
	assert.Equal(t, "/api/v1/checkout", spans.At(0).Name())
}

func TestProcessLogs_DynamicPolicyUpdate(t *testing.T) {
	// Start processor with empty policy set
	googlepolicy.SetActivePolicySet(nil)

	p := newGooglePolicyProcessor(&Config{}, zap.NewNop())
	require.NoError(t, p.start(context.Background(), componenttest.NewNopHost()))
	defer func() { _ = p.shutdown(context.Background()) }()

	newLog := func(body string) plog.Logs {
		ld := plog.NewLogs()
		rl := ld.ResourceLogs().AppendEmpty()
		sl := rl.ScopeLogs().AppendEmpty()
		lr := sl.LogRecords().AppendEmpty()
		lr.Body().SetStr(body)
		return ld
	}

	// 1. Initial evaluation -> not dropped
	out1, err := p.processLogs(context.Background(), newLog("test log"))
	require.NoError(t, err)
	assert.Equal(t, 1, out1.ResourceLogs().Len())

	// 2. Set new policy set that drops "test log"
	dropPolicy := mustPolicy(t, &policyv1alpha1.LogFilterPolicy{
		Id:     "drop-test-log",
		Action: policyv1alpha1.Action_ACTION_DROP.Enum(),
		Matches: []*policyv1alpha1.LogMatcher{
			{
				Target: &policyv1alpha1.LogFieldSelector{
					Target: &policyv1alpha1.LogFieldSelector_RecordField{
						RecordField: policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_BODY,
					},
				},
				Predicate: &policyv1alpha1.LogMatcher_Equals{
					Equals: &policyv1alpha1.Value{
						Value: &policyv1alpha1.Value_StringValue{StringValue: "test log"},
					},
				},
			},
		},
	})

	googlepolicy.SetActivePolicySet(&googlepolicy.PolicySet{
		RevisionID: "rev-update-1",
		Policies: map[string]*googlepolicy.PolicySetEntry{
			"drop-test-log": {
				PolicyObj: dropPolicy,
			},
		},
	})
	defer googlepolicy.SetActivePolicySet(nil)

	// Wait briefly for background watcher to recompile
	assert.Eventually(t, func() bool {
		out, err := p.processLogs(context.Background(), newLog("test log"))
		return err == nil && out.ResourceLogs().Len() == 0
	}, 1*time.Second, 10*time.Millisecond)
}

func TestProcessLogs_WithRealLogFilterPolicy(t *testing.T) {
	p := newGooglePolicyProcessor(&Config{}, zap.NewNop())
	require.NoError(t, p.start(context.Background(), componenttest.NewNopHost()))
	defer func() { _ = p.shutdown(context.Background()) }()

	dropPolicy, err := logfilter.NewPolicyFromProto(&policyv1alpha1.LogFilterPolicy{
		Id:     "drop-debug-logs",
		Action: policyv1alpha1.Action_ACTION_DROP.Enum(),
		Matches: []*policyv1alpha1.LogMatcher{
			{
				Target: &policyv1alpha1.LogFieldSelector{
					Target: &policyv1alpha1.LogFieldSelector_RecordField{
						RecordField: policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_BODY,
					},
				},
				Predicate: &policyv1alpha1.LogMatcher_Equals{
					Equals: &policyv1alpha1.Value{
						Value: &policyv1alpha1.Value_StringValue{
							StringValue: "DROP_THIS_LINE",
						},
					},
				},
			},
		},
	})
	require.NoError(t, err)

	ps := &googlepolicy.PolicySet{
		RevisionID: "rev-real-filter",
		Policies: map[string]*googlepolicy.PolicySetEntry{
			dropPolicy.PolicyName(): {PolicyObj: dropPolicy},
		},
	}
	googlepolicy.SetActivePolicySet(ps)
	defer googlepolicy.SetActivePolicySet(nil)

	assert.Eventually(t, func() bool {
		ev := p.evaluator.Load()
		return ev != nil && len(ev.logPolicies) > 0
	}, 1*time.Second, 10*time.Millisecond)

	ld := plog.NewLogs()
	rl := ld.ResourceLogs().AppendEmpty()
	sl := rl.ScopeLogs().AppendEmpty()

	lr1 := sl.LogRecords().AppendEmpty()
	lr1.Body().SetStr("DROP_THIS_LINE")

	lr2 := sl.LogRecords().AppendEmpty()
	lr2.Body().SetStr("KEEP_THIS_LINE")

	out, err := p.processLogs(context.Background(), ld)
	require.NoError(t, err)

	require.Equal(t, 1, out.ResourceLogs().Len())
	require.Equal(t, 1, out.ResourceLogs().At(0).ScopeLogs().Len())
	records := out.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords()
	require.Equal(t, 1, records.Len())
	assert.Equal(t, "KEEP_THIS_LINE", records.At(0).Body().AsString())
}

func TestProcessLogs_LoadFromRawJSONPolicySet(t *testing.T) {
	p := newGooglePolicyProcessor(&Config{}, zap.NewNop())
	require.NoError(t, p.start(context.Background(), componenttest.NewNopHost()))
	defer func() { _ = p.shutdown(context.Background()) }()

	rawPolicy := map[string]any{
		"type":   "log_filter",
		"id":     "drop-warn-logs",
		"action": "ACTION_DROP",
		"matches": []any{
			map[string]any{
				"target": map[string]any{
					"record_field": "LOG_RECORD_FIELD_SEVERITY_TEXT",
				},
				"equals": map[string]any{
					"string_value": "WARN",
				},
			},
		},
	}

	ps, err := googlepolicy.MakePolicySet("rev-raw", []map[string]any{rawPolicy})
	require.NoError(t, err)
	googlepolicy.SetActivePolicySet(ps)
	defer googlepolicy.SetActivePolicySet(nil)

	assert.Eventually(t, func() bool {
		ev := p.evaluator.Load()
		return ev != nil && len(ev.logPolicies) > 0
	}, 1*time.Second, 10*time.Millisecond)

	ld := plog.NewLogs()
	rl := ld.ResourceLogs().AppendEmpty()
	sl := rl.ScopeLogs().AppendEmpty()

	lWarn := sl.LogRecords().AppendEmpty()
	lWarn.SetSeverityText("WARN")

	lInfo := sl.LogRecords().AppendEmpty()
	lInfo.SetSeverityText("INFO")

	out, err := p.processLogs(context.Background(), ld)
	require.NoError(t, err)

	require.Equal(t, 1, out.ResourceLogs().Len())
	records := out.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords()
	require.Equal(t, 1, records.Len())
	assert.Equal(t, "INFO", records.At(0).SeverityText())
}

func TestProcessor_DoubleShutdownAndReloadFailure(t *testing.T) {
	// Set a TransformationPolicy that implements none of the per-signal evaluator
	// interfaces so NewEvaluator rejects it and reloadPolicies fails.
	invalidPolicy := &nonEvaluatorPolicy{
		name: "invalid-log-policy",
	}

	googlepolicy.SetActivePolicySet(&googlepolicy.PolicySet{
		RevisionID: "rev-invalid",
		Policies: map[string]*googlepolicy.PolicySetEntry{
			"invalid-log-policy": {
				PolicyObj: invalidPolicy,
			},
		},
	})
	defer googlepolicy.SetActivePolicySet(nil)

	p := newGooglePolicyProcessor(&Config{}, zap.NewNop())
	require.NoError(t, p.start(context.Background(), componenttest.NewNopHost()))

	// Evaluator should remain nil due to compilation error
	assert.Nil(t, p.evaluator.Load())

	// Calling shutdown multiple times must not panic (Review Issue #12)
	require.NoError(t, p.shutdown(context.Background()))
	require.NoError(t, p.shutdown(context.Background()))
}

func TestProcessor_WatcherChannelClosed(t *testing.T) {
	p := newGooglePolicyProcessor(&Config{}, zap.NewNop())
	p.stopCh = make(chan struct{})
	ch := make(chan struct{})
	p.watcherCh = ch

	p.wg.Add(1)
	go p.watchPolicies()

	// Closing watcherCh directly should cause watchPolicies goroutine to exit cleanly
	close(ch)
	p.wg.Wait()
}
