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
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy/logfilter"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.uber.org/zap"
)

func TestProcessTraces(t *testing.T) {
	p := newGooglePolicyProcessor(&Config{}, zap.NewNop())
	td := ptrace.NewTraces()
	rs := td.ResourceSpans().AppendEmpty()
	rs.ScopeSpans().AppendEmpty().Spans().AppendEmpty()

	out, err := p.processTraces(context.Background(), td)
	require.NoError(t, err)
	assert.Equal(t, td, out)
}

func TestProcessMetrics(t *testing.T) {
	p := newGooglePolicyProcessor(&Config{}, zap.NewNop())
	md := pmetric.NewMetrics()
	rm := md.ResourceMetrics().AppendEmpty()
	rm.ScopeMetrics().AppendEmpty().Metrics().AppendEmpty()

	out, err := p.processMetrics(context.Background(), md)
	require.NoError(t, err)
	assert.Equal(t, md, out)
}

func TestProcessLogs(t *testing.T) {
	p := newGooglePolicyProcessor(&Config{}, zap.NewNop())
	ld := plog.NewLogs()
	rl := ld.ResourceLogs().AppendEmpty()
	rl.ScopeLogs().AppendEmpty().LogRecords().AppendEmpty()

	out, err := p.processLogs(context.Background(), ld)
	require.NoError(t, err)
	assert.Equal(t, ld, out)
}

func TestProcessLogs_FilterPolicyDrop(t *testing.T) {
	p := newGooglePolicyProcessor(&Config{}, zap.NewNop())

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
		RevisionID: "rev-drop",
		Policies: map[string]*googlepolicy.PolicySetEntry{
			dropPolicy.PolicyName(): {PolicyObj: dropPolicy},
		},
	}
	googlepolicy.SetActivePolicySet(ps)
	t.Cleanup(func() {
		googlepolicy.SetActivePolicySet(&googlepolicy.PolicySet{RevisionID: "clean"})
	})

	ld := plog.NewLogs()
	rl := ld.ResourceLogs().AppendEmpty()
	sl := rl.ScopeLogs().AppendEmpty()

	lr1 := sl.LogRecords().AppendEmpty()
	lr1.Body().SetStr("DROP_THIS_LINE")

	lr2 := sl.LogRecords().AppendEmpty()
	lr2.Body().SetStr("KEEP_THIS_LINE")

	require.Equal(t, 2, ld.LogRecordCount())

	out, err := p.processLogs(context.Background(), ld)
	require.NoError(t, err)

	require.Equal(t, 1, out.LogRecordCount())
	require.Equal(t, 1, out.ResourceLogs().Len())
	require.Equal(t, 1, out.ResourceLogs().At(0).ScopeLogs().Len())
	assert.Equal(t, "KEEP_THIS_LINE", out.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0).Body().AsString())
}

func TestProcessLogs_FilterPolicyKeep(t *testing.T) {
	p := newGooglePolicyProcessor(&Config{}, zap.NewNop())

	keepPolicy, err := logfilter.NewPolicyFromProto(&policyv1alpha1.LogFilterPolicy{
		Id:     "keep-important-logs",
		Action: policyv1alpha1.Action_ACTION_KEEP.Enum(),
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
							StringValue: "IMPORTANT_LINE",
						},
					},
				},
			},
		},
	})
	require.NoError(t, err)

	ps := &googlepolicy.PolicySet{
		RevisionID: "rev-keep",
		Policies: map[string]*googlepolicy.PolicySetEntry{
			keepPolicy.PolicyName(): {PolicyObj: keepPolicy},
		},
	}
	googlepolicy.SetActivePolicySet(ps)
	t.Cleanup(func() {
		googlepolicy.SetActivePolicySet(&googlepolicy.PolicySet{RevisionID: "clean"})
	})

	ld := plog.NewLogs()
	rl := ld.ResourceLogs().AppendEmpty()
	sl := rl.ScopeLogs().AppendEmpty()

	lr1 := sl.LogRecords().AppendEmpty()
	lr1.Body().SetStr("IMPORTANT_LINE")

	lr2 := sl.LogRecords().AppendEmpty()
	lr2.Body().SetStr("OTHER_LINE")

	require.Equal(t, 2, ld.LogRecordCount())

	out, err := p.processLogs(context.Background(), ld)
	require.NoError(t, err)

	require.Equal(t, 1, out.LogRecordCount())
	assert.Equal(t, "IMPORTANT_LINE", out.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0).Body().AsString())
}

func TestProcessLogs_DynamicPolicyPropagationViaWatcherChannel(t *testing.T) {
	p := newGooglePolicyProcessor(&Config{}, zap.NewNop())
	ctx := context.Background()

	err := p.start(ctx, nil)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = p.shutdown(ctx)
		googlepolicy.SetActivePolicySet(&googlepolicy.PolicySet{RevisionID: "clean"})
	})

	// 1. Initially without any policies, logs pass through.
	ld1 := plog.NewLogs()
	sl1 := ld1.ResourceLogs().AppendEmpty().ScopeLogs().AppendEmpty()
	sl1.LogRecords().AppendEmpty().Body().SetStr("DROP_CANDIDATE")
	out1, err := p.processLogs(ctx, ld1)
	require.NoError(t, err)
	assert.Equal(t, 1, out1.LogRecordCount())

	// 2. Discover / publish a new policy via googlepolicy.SetActivePolicySet.
	// This triggers WatcherChannel notification to p.watchPolicies() goroutine.
	dropPolicy, err := logfilter.NewPolicyFromProto(&policyv1alpha1.LogFilterPolicy{
		Id:     "dynamic-drop",
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
							StringValue: "DROP_CANDIDATE",
						},
					},
				},
			},
		},
	})
	require.NoError(t, err)

	psDrop := &googlepolicy.PolicySet{
		RevisionID: "rev-dynamic-1",
		Policies: map[string]*googlepolicy.PolicySetEntry{
			dropPolicy.PolicyName(): {PolicyObj: dropPolicy},
		},
	}
	googlepolicy.SetActivePolicySet(psDrop)

	// Wait for WatcherChannel to propagate the policy update to the processor.
	assert.Eventually(t, func() bool {
		return len(p.getLogFilters()) == 1
	}, 1*time.Second, 10*time.Millisecond)

	// 3. Now the DROP policy is active and propagated via WatcherChannel.
	ld2 := plog.NewLogs()
	sl2 := ld2.ResourceLogs().AppendEmpty().ScopeLogs().AppendEmpty()
	lrDrop := sl2.LogRecords().AppendEmpty()
	lrDrop.Body().SetStr("DROP_CANDIDATE")
	lrKeep := sl2.LogRecords().AppendEmpty()
	lrKeep.Body().SetStr("KEEP_CANDIDATE")

	out2, err := p.processLogs(ctx, ld2)
	require.NoError(t, err)
	require.Equal(t, 1, out2.LogRecordCount())
	assert.Equal(t, "KEEP_CANDIDATE", out2.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0).Body().AsString())

	// 4. Rollback active policy set.
	googlepolicy.RollbackActivePolicySet()

	// Wait for WatcherChannel to propagate the rollback to the processor.
	assert.Eventually(t, func() bool {
		return len(p.getLogFilters()) == 0
	}, 1*time.Second, 10*time.Millisecond)

	// 5. Now without the drop policy, logs pass through again.
	ld3 := plog.NewLogs()
	sl3 := ld3.ResourceLogs().AppendEmpty().ScopeLogs().AppendEmpty()
	sl3.LogRecords().AppendEmpty().Body().SetStr("DROP_CANDIDATE")
	out3, err := p.processLogs(ctx, ld3)
	require.NoError(t, err)
	assert.Equal(t, 1, out3.LogRecordCount())
}
