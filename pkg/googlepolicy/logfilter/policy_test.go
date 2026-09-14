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

package logfilter

import (
	"testing"

	policyv1alpha1 "github.com/GoogleCloudPlatform/opentelemetry-operations-collector/gen/go/policy/v1alpha1"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
)

func newTestLogBundle() (plog.LogRecord, plog.ScopeLogs, plog.ResourceLogs) {
	ld := plog.NewLogs()
	rl := ld.ResourceLogs().AppendEmpty()
	rl.Resource().Attributes().PutStr("cloud.zone", "us-central1-a")

	sl := rl.ScopeLogs().AppendEmpty()
	sl.Scope().SetName("my.library")
	sl.Scope().SetVersion("v1.2.3")
	sl.SetSchemaUrl("https://opentelemetry.io/schemas/1.24.0")
	sl.Scope().Attributes().PutStr("scope.tag", "backend")

	lr := sl.LogRecords().AppendEmpty()
	lr.Body().SetStr("hello world from service")
	lr.SetSeverityText("INFO")
	lr.SetSeverityNumber(plog.SeverityNumberInfo)
	lr.SetTraceID(pcommon.TraceID([16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}))
	lr.SetSpanID(pcommon.SpanID([8]byte{1, 2, 3, 4, 5, 6, 7, 8}))
	lr.Attributes().PutStr("http.method", "GET")
	lr.Attributes().PutInt("http.status_code", 200)
	lr.Attributes().PutDouble("latency_seconds", 1.25)
	lr.Attributes().PutBool("feature.enabled", true)
	lr.Attributes().PutEmptyBytes("binary.id").FromRaw([]byte{0x01, 0x02, 0x03})

	roles := lr.Attributes().PutEmptySlice("user.roles")
	roles.AppendEmpty().SetStr("viewer")
	roles.AppendEmpty().SetStr("admin")

	meta := lr.Attributes().PutEmptyMap("metadata")
	meta.PutStr("env", "prod")

	items := lr.Attributes().PutEmptySlice("items")
	item0 := items.AppendEmpty().SetEmptyMap()
	item0.PutStr("id", "item-001")

	return lr, sl, rl
}

func logContext(log plog.LogRecord, scope plog.ScopeLogs, res plog.ResourceLogs) googlepolicy.LogContext {
	return googlepolicy.LogContext{
		Record:            log,
		Resource:          res.Resource(),
		Scope:             scope.Scope(),
		ResourceSchemaURL: res.SchemaUrl(),
		ScopeSchemaURL:    scope.SchemaUrl(),
	}
}

func evalLog(pol *Policy, log plog.LogRecord, scope plog.ScopeLogs, res plog.ResourceLogs) googlepolicy.EvalResult {
	return pol.EvaluateLog(logContext(log, scope, res))
}

func matchesLog(pol *Policy, log plog.LogRecord, scope plog.ScopeLogs, res plog.ResourceLogs) bool {
	return pol.matchesContext(logContext(log, scope, res))
}

func TestPolicyValidationErrors(t *testing.T) {
	tests := []struct {
		name      string
		proto     *policyv1alpha1.LogFilterPolicy
		errExpect error
	}{
		{
			name:      "nil proto",
			proto:     nil,
			errExpect: assert.AnError,
		},
		{
			name: "missing action",
			proto: &policyv1alpha1.LogFilterPolicy{
				Id:     "p1",
				Action: policyv1alpha1.Action_ACTION_UNSPECIFIED.Enum(),
				Matches: []*policyv1alpha1.LogMatcher{
					{
						Target: &policyv1alpha1.LogFieldSelector{
							Target: &policyv1alpha1.LogFieldSelector_RecordField{
								RecordField: policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_BODY,
							},
						},
						Predicate: &policyv1alpha1.LogMatcher_Exists{},
					},
				},
			},
			errExpect: ErrMissingAction,
		},
		{
			name: "missing matchers",
			proto: &policyv1alpha1.LogFilterPolicy{
				Id:      "p2",
				Action:  policyv1alpha1.Action_ACTION_DROP.Enum(),
				Matches: []*policyv1alpha1.LogMatcher{},
			},
			errExpect: ErrMissingMatchers,
		},
		{
			name: "matcher missing target",
			proto: &policyv1alpha1.LogFilterPolicy{
				Id:     "p3",
				Action: policyv1alpha1.Action_ACTION_DROP.Enum(),
				Matches: []*policyv1alpha1.LogMatcher{
					{
						Predicate: &policyv1alpha1.LogMatcher_Exists{},
					},
				},
			},
			errExpect: ErrMissingTarget,
		},
		{
			name: "matcher unspecified record field",
			proto: &policyv1alpha1.LogFilterPolicy{
				Id:     "p3-unspecified-record",
				Action: policyv1alpha1.Action_ACTION_DROP.Enum(),
				Matches: []*policyv1alpha1.LogMatcher{
					{
						Target: &policyv1alpha1.LogFieldSelector{
							Target: &policyv1alpha1.LogFieldSelector_RecordField{
								RecordField: policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_UNSPECIFIED,
							},
						},
						Predicate: &policyv1alpha1.LogMatcher_Exists{},
					},
				},
			},
			errExpect: assert.AnError,
		},
		{
			name: "matcher unspecified scope field",
			proto: &policyv1alpha1.LogFilterPolicy{
				Id:     "p3-unspecified-scope",
				Action: policyv1alpha1.Action_ACTION_DROP.Enum(),
				Matches: []*policyv1alpha1.LogMatcher{
					{
						Target: &policyv1alpha1.LogFieldSelector{
							Target: &policyv1alpha1.LogFieldSelector_ScopeField{
								ScopeField: policyv1alpha1.ScopeField_SCOPE_FIELD_UNSPECIFIED,
							},
						},
						Predicate: &policyv1alpha1.LogMatcher_Exists{},
					},
				},
			},
			errExpect: assert.AnError,
		},
		{
			name: "matcher missing predicate",
			proto: &policyv1alpha1.LogFilterPolicy{
				Id:     "p4",
				Action: policyv1alpha1.Action_ACTION_DROP.Enum(),
				Matches: []*policyv1alpha1.LogMatcher{
					{
						Target: &policyv1alpha1.LogFieldSelector{
							Target: &policyv1alpha1.LogFieldSelector_RecordField{
								RecordField: policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_BODY,
							},
						},
					},
				},
			},
			errExpect: ErrMissingPredicate,
		},
		{
			name: "matcher invalid regex",
			proto: &policyv1alpha1.LogFilterPolicy{
				Id:     "p5",
				Action: policyv1alpha1.Action_ACTION_DROP.Enum(),
				Matches: []*policyv1alpha1.LogMatcher{
					{
						Target: &policyv1alpha1.LogFieldSelector{
							Target: &policyv1alpha1.LogFieldSelector_RecordField{
								RecordField: policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_BODY,
							},
						},
						Predicate: &policyv1alpha1.LogMatcher_Regex{
							Regex: "[invalid(regex",
						},
					},
				},
			},
			errExpect: assert.AnError,
		},
		{
			name: "invalid action value",
			proto: &policyv1alpha1.LogFilterPolicy{
				Id:     "p6-invalid-action",
				Action: policyv1alpha1.Action(99).Enum(),
				Matches: []*policyv1alpha1.LogMatcher{
					{
						Target:    recordFieldTarget(policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_BODY),
						Predicate: &policyv1alpha1.LogMatcher_Exists{},
					},
				},
			},
			errExpect: ErrMissingAction,
		},
		{
			name: "matcher empty target oneof",
			proto: &policyv1alpha1.LogFilterPolicy{
				Id:     "p7-empty-oneof",
				Action: policyv1alpha1.Action_ACTION_DROP.Enum(),
				Matches: []*policyv1alpha1.LogMatcher{
					{
						Target:    &policyv1alpha1.LogFieldSelector{},
						Predicate: &policyv1alpha1.LogMatcher_Exists{},
					},
				},
			},
			errExpect: ErrMissingTarget,
		},
		{
			name: "matcher empty log attribute path",
			proto: &policyv1alpha1.LogFilterPolicy{
				Id:     "p8-log-attr",
				Action: policyv1alpha1.Action_ACTION_DROP.Enum(),
				Matches: []*policyv1alpha1.LogMatcher{
					{
						Target: &policyv1alpha1.LogFieldSelector{
							Target: &policyv1alpha1.LogFieldSelector_LogAttribute{
								LogAttribute: &policyv1alpha1.AttributePath{},
							},
						},
						Predicate: &policyv1alpha1.LogMatcher_Exists{},
					},
				},
			},
			errExpect: assert.AnError,
		},
		{
			name: "matcher empty resource attribute path",
			proto: &policyv1alpha1.LogFilterPolicy{
				Id:     "p8-res-attr",
				Action: policyv1alpha1.Action_ACTION_DROP.Enum(),
				Matches: []*policyv1alpha1.LogMatcher{
					{
						Target: &policyv1alpha1.LogFieldSelector{
							Target: &policyv1alpha1.LogFieldSelector_ResourceAttribute{
								ResourceAttribute: &policyv1alpha1.AttributePath{},
							},
						},
						Predicate: &policyv1alpha1.LogMatcher_Exists{},
					},
				},
			},
			errExpect: assert.AnError,
		},
		{
			name: "matcher empty scope attribute path",
			proto: &policyv1alpha1.LogFilterPolicy{
				Id:     "p8-scope-attr",
				Action: policyv1alpha1.Action_ACTION_DROP.Enum(),
				Matches: []*policyv1alpha1.LogMatcher{
					{
						Target: &policyv1alpha1.LogFieldSelector{
							Target: &policyv1alpha1.LogFieldSelector_ScopeAttribute{
								ScopeAttribute: &policyv1alpha1.AttributePath{},
							},
						},
						Predicate: &policyv1alpha1.LogMatcher_Exists{},
					},
				},
			},
			errExpect: assert.AnError,
		},
		{
			name: "matcher nil log attribute path",
			proto: &policyv1alpha1.LogFilterPolicy{
				Id:     "p9-nil-log-attr",
				Action: policyv1alpha1.Action_ACTION_DROP.Enum(),
				Matches: []*policyv1alpha1.LogMatcher{
					{
						Target: &policyv1alpha1.LogFieldSelector{
							Target: &policyv1alpha1.LogFieldSelector_LogAttribute{},
						},
						Predicate: &policyv1alpha1.LogMatcher_Exists{},
					},
				},
			},
			errExpect: assert.AnError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewPolicyFromProto(tt.proto)
			require.Error(t, err)
			if tt.errExpect != assert.AnError {
				assert.ErrorIs(t, err, tt.errExpect)
			}
		})
	}
}

func TestPolicyMatchingAndDrop(t *testing.T) {
	log, scope, res := newTestLogBundle()

	t.Run("Action DROP matching record", func(t *testing.T) {
		pb := &policyv1alpha1.LogFilterPolicy{
			Id:     "drop-hello",
			Action: policyv1alpha1.Action_ACTION_DROP.Enum(),
			Matches: []*policyv1alpha1.LogMatcher{
				{
					Target: &policyv1alpha1.LogFieldSelector{
						Target: &policyv1alpha1.LogFieldSelector_RecordField{
							RecordField: policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_BODY,
						},
					},
					Predicate: &policyv1alpha1.LogMatcher_Regex{
						Regex: "hello.*service",
					},
				},
			},
		}

		pol, err := NewPolicyFromProto(pb)
		require.NoError(t, err)
		assert.Equal(t, "drop-hello", pol.PolicyName())
		assert.Equal(t, PolicyType, pol.PolicyType())
		assert.Equal(t, googlepolicy.PolicyClassTransformation, pol.PolicyClass())
		assert.NoError(t, pol.Validate())

		assert.True(t, matchesLog(pol, log, scope, res))
		assert.Equal(t, googlepolicy.EvalDrop, evalLog(pol, log, scope, res))
	})

	t.Run("Action DROP non-matching record", func(t *testing.T) {
		pb := &policyv1alpha1.LogFilterPolicy{
			Id:     "drop-non-matching",
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
								StringValue: "not the body",
							},
						},
					},
				},
			},
		}

		pol, err := NewPolicyFromProto(pb)
		require.NoError(t, err)

		assert.False(t, matchesLog(pol, log, scope, res))
		assert.Equal(t, googlepolicy.EvalNoMatch, evalLog(pol, log, scope, res))
	})

	t.Run("Action KEEP matching record", func(t *testing.T) {
		pb := &policyv1alpha1.LogFilterPolicy{
			Id:     "keep-info",
			Action: policyv1alpha1.Action_ACTION_KEEP.Enum(),
			Matches: []*policyv1alpha1.LogMatcher{
				{
					Target: &policyv1alpha1.LogFieldSelector{
						Target: &policyv1alpha1.LogFieldSelector_RecordField{
							RecordField: policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SEVERITY_TEXT,
						},
					},
					Predicate: &policyv1alpha1.LogMatcher_Equals{
						Equals: &policyv1alpha1.Value{
							Value: &policyv1alpha1.Value_StringValue{
								StringValue: "INFO",
							},
						},
					},
				},
			},
		}

		pol, err := NewPolicyFromProto(pb)
		require.NoError(t, err)

		assert.True(t, matchesLog(pol, log, scope, res))
		// Matching KEEP policy evaluates to EvalKeep (exemption)
		assert.Equal(t, googlepolicy.EvalKeep, evalLog(pol, log, scope, res))
	})

	t.Run("Action KEEP non-matching record", func(t *testing.T) {
		pb := &policyv1alpha1.LogFilterPolicy{
			Id:     "keep-error-only",
			Action: policyv1alpha1.Action_ACTION_KEEP.Enum(),
			Matches: []*policyv1alpha1.LogMatcher{
				{
					Target: &policyv1alpha1.LogFieldSelector{
						Target: &policyv1alpha1.LogFieldSelector_RecordField{
							RecordField: policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SEVERITY_TEXT,
						},
					},
					Predicate: &policyv1alpha1.LogMatcher_Equals{
						Equals: &policyv1alpha1.Value{
							Value: &policyv1alpha1.Value_StringValue{
								StringValue: "ERROR",
							},
						},
					},
				},
			},
		}

		pol, err := NewPolicyFromProto(pb)
		require.NoError(t, err)

		assert.False(t, matchesLog(pol, log, scope, res))
		// Non-matching KEEP policy evaluates to EvalNoMatch (does not prune non-matching records)
		assert.Equal(t, googlepolicy.EvalNoMatch, evalLog(pol, log, scope, res))
	})

	t.Run("Multiple matchers AND semantics", func(t *testing.T) {
		pb := &policyv1alpha1.LogFilterPolicy{
			Id:     "and-matchers",
			Action: policyv1alpha1.Action_ACTION_DROP.Enum(),
			Matches: []*policyv1alpha1.LogMatcher{
				{
					Target: &policyv1alpha1.LogFieldSelector{
						Target: &policyv1alpha1.LogFieldSelector_LogAttribute{
							LogAttribute: &policyv1alpha1.AttributePath{
								Path: []string{"http.method"},
							},
						},
					},
					Predicate: &policyv1alpha1.LogMatcher_Equals{
						Equals: &policyv1alpha1.Value{
							Value: &policyv1alpha1.Value_StringValue{
								StringValue: "GET",
							},
						},
					},
				},
				{
					Target: &policyv1alpha1.LogFieldSelector{
						Target: &policyv1alpha1.LogFieldSelector_LogAttribute{
							LogAttribute: &policyv1alpha1.AttributePath{
								Path: []string{"http.status_code"},
							},
						},
					},
					Predicate: &policyv1alpha1.LogMatcher_Equals{
						Equals: &policyv1alpha1.Value{
							Value: &policyv1alpha1.Value_IntValue{
								IntValue: 200,
							},
						},
					},
				},
			},
		}

		pol, err := NewPolicyFromProto(pb)
		require.NoError(t, err)
		assert.True(t, matchesLog(pol, log, scope, res))

		// If one matcher does not match, the entire policy should not match
		pb.Matches[1].Predicate = &policyv1alpha1.LogMatcher_Equals{
			Equals: &policyv1alpha1.Value{
				Value: &policyv1alpha1.Value_IntValue{
					IntValue: 500,
				},
			},
		}
		pol2, err := NewPolicyFromProto(pb)
		require.NoError(t, err)
		assert.False(t, matchesLog(pol2, log, scope, res))
	})

	t.Run("Negate matcher", func(t *testing.T) {
		pb := &policyv1alpha1.LogFilterPolicy{
			Id:     "negate-test",
			Action: policyv1alpha1.Action_ACTION_DROP.Enum(),
			Matches: []*policyv1alpha1.LogMatcher{
				{
					Target: &policyv1alpha1.LogFieldSelector{
						Target: &policyv1alpha1.LogFieldSelector_LogAttribute{
							LogAttribute: &policyv1alpha1.AttributePath{
								Path: []string{"http.method"},
							},
						},
					},
					Predicate: &policyv1alpha1.LogMatcher_Equals{
						Equals: &policyv1alpha1.Value{
							Value: &policyv1alpha1.Value_StringValue{
								StringValue: "POST", // does not match GET
							},
						},
					},
					Negate: true, // negated -> true
				},
			},
		}

		pol, err := NewPolicyFromProto(pb)
		require.NoError(t, err)
		assert.True(t, matchesLog(pol, log, scope, res))
	})

	t.Run("Resource and Scope selectors", func(t *testing.T) {
		pb := &policyv1alpha1.LogFilterPolicy{
			Id:     "res-scope-test",
			Action: policyv1alpha1.Action_ACTION_DROP.Enum(),
			Matches: []*policyv1alpha1.LogMatcher{
				{
					Target: &policyv1alpha1.LogFieldSelector{
						Target: &policyv1alpha1.LogFieldSelector_ResourceAttribute{
							ResourceAttribute: &policyv1alpha1.AttributePath{
								Path: []string{"cloud.zone"},
							},
						},
					},
					Predicate: &policyv1alpha1.LogMatcher_Equals{
						Equals: &policyv1alpha1.Value{
							Value: &policyv1alpha1.Value_StringValue{
								StringValue: "us-central1-a",
							},
						},
					},
				},
				{
					Target: &policyv1alpha1.LogFieldSelector{
						Target: &policyv1alpha1.LogFieldSelector_ScopeAttribute{
							ScopeAttribute: &policyv1alpha1.AttributePath{
								Path: []string{"scope.tag"},
							},
						},
					},
					Predicate: &policyv1alpha1.LogMatcher_Equals{
						Equals: &policyv1alpha1.Value{
							Value: &policyv1alpha1.Value_StringValue{
								StringValue: "backend",
							},
						},
					},
				},
				{
					Target: &policyv1alpha1.LogFieldSelector{
						Target: &policyv1alpha1.LogFieldSelector_ScopeField{
							ScopeField: policyv1alpha1.ScopeField_SCOPE_FIELD_NAME,
						},
					},
					Predicate: &policyv1alpha1.LogMatcher_Equals{
						Equals: &policyv1alpha1.Value{
							Value: &policyv1alpha1.Value_StringValue{
								StringValue: "my.library",
							},
						},
					},
				},
				{
					Target: &policyv1alpha1.LogFieldSelector{
						Target: &policyv1alpha1.LogFieldSelector_ScopeField{
							ScopeField: policyv1alpha1.ScopeField_SCOPE_FIELD_VERSION,
						},
					},
					Predicate: &policyv1alpha1.LogMatcher_Equals{
						Equals: &policyv1alpha1.Value{
							Value: &policyv1alpha1.Value_StringValue{
								StringValue: "v1.2.3",
							},
						},
					},
				},
				{
					Target: &policyv1alpha1.LogFieldSelector{
						Target: &policyv1alpha1.LogFieldSelector_ScopeField{
							ScopeField: policyv1alpha1.ScopeField_SCOPE_FIELD_SCHEMA_URL,
						},
					},
					Predicate: &policyv1alpha1.LogMatcher_Equals{
						Equals: &policyv1alpha1.Value{
							Value: &policyv1alpha1.Value_StringValue{
								StringValue: "https://opentelemetry.io/schemas/1.24.0",
							},
						},
					},
				},
			},
		}

		pol, err := NewPolicyFromProto(pb)
		require.NoError(t, err)
		assert.True(t, matchesLog(pol, log, scope, res))
	})

	t.Run("Severity number, trace ID, span ID selectors", func(t *testing.T) {
		pb := &policyv1alpha1.LogFilterPolicy{
			Id:     "ids-test",
			Action: policyv1alpha1.Action_ACTION_DROP.Enum(),
			Matches: []*policyv1alpha1.LogMatcher{
				{
					Target: &policyv1alpha1.LogFieldSelector{
						Target: &policyv1alpha1.LogFieldSelector_RecordField{
							RecordField: policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SEVERITY_NUMBER,
						},
					},
					Predicate: &policyv1alpha1.LogMatcher_Equals{
						Equals: &policyv1alpha1.Value{
							Value: &policyv1alpha1.Value_IntValue{
								IntValue: 9, // SeverityNumberInfo = 9
							},
						},
					},
				},
				{
					Target: &policyv1alpha1.LogFieldSelector{
						Target: &policyv1alpha1.LogFieldSelector_RecordField{
							RecordField: policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_TRACE_ID,
						},
					},
					Predicate: &policyv1alpha1.LogMatcher_Exists{},
				},
				{
					Target: &policyv1alpha1.LogFieldSelector{
						Target: &policyv1alpha1.LogFieldSelector_RecordField{
							RecordField: policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SPAN_ID,
						},
					},
					Predicate: &policyv1alpha1.LogMatcher_Exists{},
				},
			},
		}

		pol, err := NewPolicyFromProto(pb)
		require.NoError(t, err)
		assert.True(t, matchesLog(pol, log, scope, res))
	})

	t.Run("Typed attribute matchers and AttributePath traversal", func(t *testing.T) {
		// Test bool, double, bytes, nested map, and array index
		pb := &policyv1alpha1.LogFilterPolicy{
			Id:     "typed-test",
			Action: policyv1alpha1.Action_ACTION_DROP.Enum(),
			Matches: []*policyv1alpha1.LogMatcher{
				{
					Target: &policyv1alpha1.LogFieldSelector{
						Target: &policyv1alpha1.LogFieldSelector_LogAttribute{
							LogAttribute: &policyv1alpha1.AttributePath{Path: []string{"feature.enabled"}},
						},
					},
					Predicate: &policyv1alpha1.LogMatcher_Equals{
						Equals: &policyv1alpha1.Value{Value: &policyv1alpha1.Value_BoolValue{BoolValue: true}},
					},
				},
				{
					Target: &policyv1alpha1.LogFieldSelector{
						Target: &policyv1alpha1.LogFieldSelector_LogAttribute{
							LogAttribute: &policyv1alpha1.AttributePath{Path: []string{"latency_seconds"}},
						},
					},
					Predicate: &policyv1alpha1.LogMatcher_Equals{
						Equals: &policyv1alpha1.Value{Value: &policyv1alpha1.Value_DoubleValue{DoubleValue: 1.25}},
					},
				},
				{
					Target: &policyv1alpha1.LogFieldSelector{
						Target: &policyv1alpha1.LogFieldSelector_LogAttribute{
							LogAttribute: &policyv1alpha1.AttributePath{Path: []string{"binary.id"}},
						},
					},
					Predicate: &policyv1alpha1.LogMatcher_Equals{
						Equals: &policyv1alpha1.Value{Value: &policyv1alpha1.Value_BytesValue{BytesValue: []byte{0x01, 0x02, 0x03}}},
					},
				},
				{
					Target: &policyv1alpha1.LogFieldSelector{
						Target: &policyv1alpha1.LogFieldSelector_LogAttribute{
							LogAttribute: &policyv1alpha1.AttributePath{Path: []string{"metadata", "env"}},
						},
					},
					Predicate: &policyv1alpha1.LogMatcher_Equals{
						Equals: &policyv1alpha1.Value{Value: &policyv1alpha1.Value_StringValue{StringValue: "prod"}},
					},
				},
				{
					Target: &policyv1alpha1.LogFieldSelector{
						Target: &policyv1alpha1.LogFieldSelector_LogAttribute{
							LogAttribute: &policyv1alpha1.AttributePath{Path: []string{"items", "0", "id"}},
						},
					},
					Predicate: &policyv1alpha1.LogMatcher_Equals{
						Equals: &policyv1alpha1.Value{Value: &policyv1alpha1.Value_StringValue{StringValue: "item-001"}},
					},
				},
			},
		}

		pol, err := NewPolicyFromProto(pb)
		require.NoError(t, err)
		assert.True(t, matchesLog(pol, log, scope, res))
	})

	t.Run("Numeric comparisons (gt, gte, lt, lte)", func(t *testing.T) {
		// http.status_code is 200, latency_seconds is 1.25, SeverityNumber is 9
		pb := &policyv1alpha1.LogFilterPolicy{
			Id:     "numeric-test",
			Action: policyv1alpha1.Action_ACTION_DROP.Enum(),
			Matches: []*policyv1alpha1.LogMatcher{
				{
					Target: &policyv1alpha1.LogFieldSelector{
						Target: &policyv1alpha1.LogFieldSelector_LogAttribute{
							LogAttribute: &policyv1alpha1.AttributePath{Path: []string{"http.status_code"}},
						},
					},
					Predicate: &policyv1alpha1.LogMatcher_Gte{
						Gte: &policyv1alpha1.NumericValue{Value: &policyv1alpha1.NumericValue_IntValue{IntValue: 200}},
					},
				},
				{
					Target: &policyv1alpha1.LogFieldSelector{
						Target: &policyv1alpha1.LogFieldSelector_LogAttribute{
							LogAttribute: &policyv1alpha1.AttributePath{Path: []string{"http.status_code"}},
						},
					},
					Predicate: &policyv1alpha1.LogMatcher_Lt{
						Lt: &policyv1alpha1.NumericValue{Value: &policyv1alpha1.NumericValue_IntValue{IntValue: 300}},
					},
				},
				{
					Target: &policyv1alpha1.LogFieldSelector{
						Target: &policyv1alpha1.LogFieldSelector_LogAttribute{
							LogAttribute: &policyv1alpha1.AttributePath{Path: []string{"latency_seconds"}},
						},
					},
					Predicate: &policyv1alpha1.LogMatcher_Gt{
						Gt: &policyv1alpha1.NumericValue{Value: &policyv1alpha1.NumericValue_DoubleValue{DoubleValue: 1.0}},
					},
				},
				{
					Target: &policyv1alpha1.LogFieldSelector{
						Target: &policyv1alpha1.LogFieldSelector_RecordField{
							RecordField: policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SEVERITY_NUMBER,
						},
					},
					Predicate: &policyv1alpha1.LogMatcher_Lte{
						Lte: &policyv1alpha1.NumericValue{Value: &policyv1alpha1.NumericValue_IntValue{IntValue: 9}},
					},
				},
			},
		}

		pol, err := NewPolicyFromProto(pb)
		require.NoError(t, err)
		assert.True(t, matchesLog(pol, log, scope, res))
	})

	t.Run("Contains predicate (list membership, string substring, bytes)", func(t *testing.T) {
		pb := &policyv1alpha1.LogFilterPolicy{
			Id:     "contains-test",
			Action: policyv1alpha1.Action_ACTION_DROP.Enum(),
			Matches: []*policyv1alpha1.LogMatcher{
				// Array membership: user.roles contains "admin"
				{
					Target: &policyv1alpha1.LogFieldSelector{
						Target: &policyv1alpha1.LogFieldSelector_LogAttribute{
							LogAttribute: &policyv1alpha1.AttributePath{Path: []string{"user.roles"}},
						},
					},
					Predicate: &policyv1alpha1.LogMatcher_Contains{
						Contains: &policyv1alpha1.Value{Value: &policyv1alpha1.Value_StringValue{StringValue: "admin"}},
					},
				},
				// Substring: body contains "world from"
				{
					Target: &policyv1alpha1.LogFieldSelector{
						Target: &policyv1alpha1.LogFieldSelector_RecordField{
							RecordField: policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_BODY,
						},
					},
					Predicate: &policyv1alpha1.LogMatcher_Contains{
						Contains: &policyv1alpha1.Value{Value: &policyv1alpha1.Value_StringValue{StringValue: "world from"}},
					},
				},
				// Bytes subsequence: binary.id contains [0x02, 0x03]
				{
					Target: &policyv1alpha1.LogFieldSelector{
						Target: &policyv1alpha1.LogFieldSelector_LogAttribute{
							LogAttribute: &policyv1alpha1.AttributePath{Path: []string{"binary.id"}},
						},
					},
					Predicate: &policyv1alpha1.LogMatcher_Contains{
						Contains: &policyv1alpha1.Value{Value: &policyv1alpha1.Value_BytesValue{BytesValue: []byte{0x02, 0x03}}},
					},
				},
			},
		}

		pol, err := NewPolicyFromProto(pb)
		require.NoError(t, err)
		assert.True(t, matchesLog(pol, log, scope, res))

		// Non-matching list item
		pb.Matches[0].Predicate = &policyv1alpha1.LogMatcher_Contains{
			Contains: &policyv1alpha1.Value{Value: &policyv1alpha1.Value_StringValue{StringValue: "superadmin"}},
		}
		pol2, err := NewPolicyFromProto(pb)
		require.NoError(t, err)
		assert.False(t, matchesLog(pol2, log, scope, res))
	})
}

func TestDriverLoadPolicy(t *testing.T) {
	raw := map[string]any{
		"type":   "log_filter",
		"id":     "loaded-via-driver",
		"action": "ACTION_DROP",
		"matches": []any{
			map[string]any{
				"target": map[string]any{
					"record_field": "LOG_RECORD_FIELD_BODY",
				},
				"equals": map[string]any{
					"string_value": "drop this line",
				},
			},
		},
	}

	p, err := googlepolicy.LoadPolicy("log_filter", raw)
	require.NoError(t, err)
	assert.Equal(t, "loaded-via-driver", p.PolicyName())
	assert.Equal(t, googlepolicy.PolicyClassTransformation, p.PolicyClass())

	lrf, ok := p.(googlepolicy.LogPolicyEvaluator)
	require.True(t, ok)

	log, scope, res := newTestLogBundle()
	log.Body().SetStr("drop this line")
	ctx := googlepolicy.LogContext{
		Record:            log,
		Resource:          res.Resource(),
		Scope:             scope.Scope(),
		ResourceSchemaURL: res.SchemaUrl(),
		ScopeSchemaURL:    scope.SchemaUrl(),
	}
	assert.Equal(t, googlepolicy.EvalDrop, lrf.EvaluateLog(ctx))

	log.Body().SetStr("keep this line")
	assert.Equal(t, googlepolicy.EvalNoMatch, lrf.EvaluateLog(ctx))
}

// --- Helpers for the table-driven tests below, mirroring the conventions used
// by the tracefilter and metricfilter test suites. ---

func recordFieldTarget(f policyv1alpha1.LogRecordField) *policyv1alpha1.LogFieldSelector {
	return &policyv1alpha1.LogFieldSelector{
		Target: &policyv1alpha1.LogFieldSelector_RecordField{RecordField: f},
	}
}

func scopeFieldTarget(f policyv1alpha1.ScopeField) *policyv1alpha1.LogFieldSelector {
	return &policyv1alpha1.LogFieldSelector{
		Target: &policyv1alpha1.LogFieldSelector_ScopeField{ScopeField: f},
	}
}

func logAttrTarget(path ...string) *policyv1alpha1.LogFieldSelector {
	return &policyv1alpha1.LogFieldSelector{
		Target: &policyv1alpha1.LogFieldSelector_LogAttribute{
			LogAttribute: &policyv1alpha1.AttributePath{Path: path},
		},
	}
}

func resourceAttrTarget(path ...string) *policyv1alpha1.LogFieldSelector {
	return &policyv1alpha1.LogFieldSelector{
		Target: &policyv1alpha1.LogFieldSelector_ResourceAttribute{
			ResourceAttribute: &policyv1alpha1.AttributePath{Path: path},
		},
	}
}

func scopeAttrTarget(path ...string) *policyv1alpha1.LogFieldSelector {
	return &policyv1alpha1.LogFieldSelector{
		Target: &policyv1alpha1.LogFieldSelector_ScopeAttribute{
			ScopeAttribute: &policyv1alpha1.AttributePath{Path: path},
		},
	}
}

func equalsString(s string) *policyv1alpha1.LogMatcher_Equals {
	return &policyv1alpha1.LogMatcher_Equals{
		Equals: &policyv1alpha1.Value{Value: &policyv1alpha1.Value_StringValue{StringValue: s}},
	}
}

func equalsInt(n int64) *policyv1alpha1.LogMatcher_Equals {
	return &policyv1alpha1.LogMatcher_Equals{
		Equals: &policyv1alpha1.Value{Value: &policyv1alpha1.Value_IntValue{IntValue: n}},
	}
}

func equalsDouble(f float64) *policyv1alpha1.LogMatcher_Equals {
	return &policyv1alpha1.LogMatcher_Equals{
		Equals: &policyv1alpha1.Value{Value: &policyv1alpha1.Value_DoubleValue{DoubleValue: f}},
	}
}

func equalsBool(b bool) *policyv1alpha1.LogMatcher_Equals {
	return &policyv1alpha1.LogMatcher_Equals{
		Equals: &policyv1alpha1.Value{Value: &policyv1alpha1.Value_BoolValue{BoolValue: b}},
	}
}

func equalsBytes(b []byte) *policyv1alpha1.LogMatcher_Equals {
	return &policyv1alpha1.LogMatcher_Equals{
		Equals: &policyv1alpha1.Value{Value: &policyv1alpha1.Value_BytesValue{BytesValue: b}},
	}
}

func containsString(s string) *policyv1alpha1.LogMatcher_Contains {
	return &policyv1alpha1.LogMatcher_Contains{
		Contains: &policyv1alpha1.Value{Value: &policyv1alpha1.Value_StringValue{StringValue: s}},
	}
}

func containsBytes(b []byte) *policyv1alpha1.LogMatcher_Contains {
	return &policyv1alpha1.LogMatcher_Contains{
		Contains: &policyv1alpha1.Value{Value: &policyv1alpha1.Value_BytesValue{BytesValue: b}},
	}
}

// dropPolicy builds a single-matcher ACTION_DROP policy around target/predicate.
func dropPolicy(id string, target *policyv1alpha1.LogFieldSelector, predicate any, negate bool) *policyv1alpha1.LogFilterPolicy {
	m := &policyv1alpha1.LogMatcher{
		Target: target,
		Negate: negate,
	}
	switch p := predicate.(type) {
	case *policyv1alpha1.LogMatcher_Exists:
		m.Predicate = p
	case *policyv1alpha1.LogMatcher_Equals:
		m.Predicate = p
	case *policyv1alpha1.LogMatcher_Regex:
		m.Predicate = p
	case *policyv1alpha1.LogMatcher_Contains:
		m.Predicate = p
	case *policyv1alpha1.LogMatcher_Gt:
		m.Predicate = p
	case *policyv1alpha1.LogMatcher_Gte:
		m.Predicate = p
	case *policyv1alpha1.LogMatcher_Lt:
		m.Predicate = p
	case *policyv1alpha1.LogMatcher_Lte:
		m.Predicate = p
	}
	return &policyv1alpha1.LogFilterPolicy{
		Id:      id,
		Action:  policyv1alpha1.Action_ACTION_DROP.Enum(),
		Matches: []*policyv1alpha1.LogMatcher{m},
	}
}

// mustDropPolicy compiles a single-matcher ACTION_DROP policy or fails the test.
func mustDropPolicy(t *testing.T, id string, target *policyv1alpha1.LogFieldSelector, predicate any, negate bool) *Policy {
	t.Helper()
	pol, err := NewPolicyFromProto(dropPolicy(id, target, predicate, negate))
	require.NoError(t, err)
	return pol
}

// mustExtract compiles a selector into its target extractor, so tests can probe
// the (value, exists) pair directly instead of inferring it from a predicate.
func mustExtract(t *testing.T, target *policyv1alpha1.LogFieldSelector) targetExtractor {
	t.Helper()
	extract, err := compileExtractor(target)
	require.NoError(t, err)
	return extract
}

func TestPolicyMetadata(t *testing.T) {
	pb := dropPolicy(
		"metadata-policy",
		recordFieldTarget(policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_BODY),
		&policyv1alpha1.LogMatcher_Exists{},
		false,
	)
	pol, err := NewPolicyFromProto(pb)
	require.NoError(t, err)

	assert.Equal(t, "metadata-policy", pol.PolicyName())
	assert.Equal(t, PolicyType, pol.PolicyType())
	assert.Equal(t, googlepolicy.PolicyClassTransformation, pol.PolicyClass())
	assert.Equal(t, []googlepolicy.Signal{googlepolicy.SignalLogs}, pol.TargetSignals())
	assert.Equal(t, policyv1alpha1.Action_ACTION_DROP, pol.Action())
	assert.NoError(t, pol.Validate())

	// Proto() hands back the very message the policy was compiled from, which
	// is what the registry relies on to re-serialize an active policy set.
	got, ok := pol.Proto().(*policyv1alpha1.LogFilterPolicy)
	require.True(t, ok)
	assert.Same(t, pb, got)
}

func TestDriverMetadata(t *testing.T) {
	d := &Driver{}
	assert.Equal(t, PolicyType, d.PolicyName())
}

// TestValidateNilProto covers the defensive nil-proto arm of Validate. A Policy
// can only reach this state by being constructed directly (NewPolicyFromProto
// rejects a nil proto up front), so the guard is exercised in-package.
func TestValidateNilProto(t *testing.T) {
	assert.ErrorIs(t, (&Policy{}).Validate(), ErrNilProto)
}

// TestEvaluateLogUnknownActionFailsClosed covers the defensive default arm of
// EvaluateLog. NewPolicyFromProto rejects any action other than KEEP/DROP, so
// the only way in is direct construction; the contract being pinned is that an
// unrecognized action degrades to EvalNoMatch (telemetry passes through)
// instead of silently dropping records.
func TestEvaluateLogUnknownActionFailsClosed(t *testing.T) {
	pol := &Policy{
		proto: &policyv1alpha1.LogFilterPolicy{
			Id:     "unknown-action",
			Action: policyv1alpha1.Action(99).Enum(),
		},
	}
	// No matchers => matchesContext is vacuously true, so evaluation reaches
	// the action switch.
	log, scope, res := newTestLogBundle()
	require.True(t, matchesLog(pol, log, scope, res))
	assert.Equal(t, googlepolicy.EvalNoMatch, evalLog(pol, log, scope, res))
}

// TestLogBodyValueTypes pins LOG_RECORD_FIELD_BODY across every pcommon value
// type a body can carry. Scalars surface as their plain Go counterparts, slices
// as []any (so `contains` means membership), and maps fall through
// matcher.ValueToAny's default arm to their canonical JSON spelling.
func TestLogBodyValueTypes(t *testing.T) {
	scope := plog.NewScopeLogs()
	res := plog.NewResourceLogs()
	bodyTarget := recordFieldTarget(policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_BODY)

	tests := []struct {
		name       string
		setBody    func(pcommon.Value)
		wantValue  any
		wantExists bool
		predicate  any
		wantMatch  bool
	}{
		{
			name:       "string body",
			setBody:    func(v pcommon.Value) { v.SetStr("hello world") },
			wantValue:  "hello world",
			wantExists: true,
			predicate:  equalsString("hello world"),
			wantMatch:  true,
		},
		{
			name:       "int body",
			setBody:    func(v pcommon.Value) { v.SetInt(42) },
			wantValue:  int64(42),
			wantExists: true,
			predicate:  equalsInt(42),
			wantMatch:  true,
		},
		{
			name:       "double body",
			setBody:    func(v pcommon.Value) { v.SetDouble(1.25) },
			wantValue:  1.25,
			wantExists: true,
			predicate:  equalsDouble(1.25),
			wantMatch:  true,
		},
		{
			name:       "bool body",
			setBody:    func(v pcommon.Value) { v.SetBool(true) },
			wantValue:  true,
			wantExists: true,
			predicate:  equalsBool(true),
			wantMatch:  true,
		},
		{
			name:       "bytes body",
			setBody:    func(v pcommon.Value) { v.SetEmptyBytes().FromRaw([]byte{0x01, 0x02, 0x03}) },
			wantValue:  []byte{0x01, 0x02, 0x03},
			wantExists: true,
			predicate:  equalsBytes([]byte{0x01, 0x02, 0x03}),
			wantMatch:  true,
		},
		{
			name: "bytes body contains subsequence",
			setBody: func(v pcommon.Value) {
				v.SetEmptyBytes().FromRaw([]byte{0x01, 0x02, 0x03})
			},
			wantValue:  []byte{0x01, 0x02, 0x03},
			wantExists: true,
			predicate:  containsBytes([]byte{0x02, 0x03}),
			wantMatch:  true,
		},
		{
			name: "slice body",
			setBody: func(v pcommon.Value) {
				s := v.SetEmptySlice()
				s.AppendEmpty().SetStr("viewer")
				s.AppendEmpty().SetStr("admin")
			},
			wantValue:  []any{"viewer", "admin"},
			wantExists: true,
			predicate:  containsString("admin"),
			wantMatch:  true,
		},
		{
			name: "slice body non-member",
			setBody: func(v pcommon.Value) {
				s := v.SetEmptySlice()
				s.AppendEmpty().SetStr("viewer")
			},
			wantValue:  []any{"viewer"},
			wantExists: true,
			predicate:  containsString("admin"),
			wantMatch:  false,
		},
		{
			name: "map body renders as json",
			setBody: func(v pcommon.Value) {
				v.SetEmptyMap().PutStr("env", "prod")
			},
			wantValue:  `{"env":"prod"}`,
			wantExists: true,
			predicate:  containsString(`"env":"prod"`),
			wantMatch:  true,
		},
		{
			name:       "empty slice body",
			setBody:    func(v pcommon.Value) { v.SetEmptySlice() },
			wantValue:  []any{},
			wantExists: true,
			predicate:  &policyv1alpha1.LogMatcher_Exists{},
			wantMatch:  true,
		},
		{
			name:       "unset body reports absent",
			setBody:    func(pcommon.Value) {},
			wantValue:  nil,
			wantExists: false,
			predicate:  &policyv1alpha1.LogMatcher_Exists{},
			wantMatch:  false,
		},
		{
			name:       "empty string body reports absent",
			setBody:    func(v pcommon.Value) { v.SetStr("") },
			wantValue:  "",
			wantExists: false,
			predicate:  &policyv1alpha1.LogMatcher_Exists{},
			wantMatch:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lr := plog.NewLogRecord()
			tt.setBody(lr.Body())

			val, exists := mustExtract(t, bodyTarget)(logContext(lr, scope, res))
			assert.Equal(t, tt.wantExists, exists)
			assert.Equal(t, tt.wantValue, val)

			pol := mustDropPolicy(t, "body-"+tt.name, bodyTarget, tt.predicate, false)
			assert.Equal(t, tt.wantMatch, matchesLog(pol, lr, scope, res))
		})
	}
}

// TestPresentButEmptyFields pins the matcher.present convention for the
// first-class string/numeric log fields: an empty (default) value reports
// exists=false per log_filter_policy.proto:63-66, yet is still handed to the
// value-comparing predicates, so `equals: {string_value: ""}` matches it.
func TestPresentButEmptyFields(t *testing.T) {
	scope := plog.NewScopeLogs()
	res := plog.NewResourceLogs()

	tests := []struct {
		name         string
		field        policyv1alpha1.LogRecordField
		setup        func(plog.LogRecord)
		wantValue    any
		emptyMatcher any
	}{
		{
			name:         "empty string body",
			field:        policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_BODY,
			setup:        func(lr plog.LogRecord) { lr.Body().SetStr("") },
			wantValue:    "",
			emptyMatcher: equalsString(""),
		},
		{
			name:         "empty severity text",
			field:        policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SEVERITY_TEXT,
			setup:        func(plog.LogRecord) {},
			wantValue:    "",
			emptyMatcher: equalsString(""),
		},
		{
			name:         "unset severity number",
			field:        policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SEVERITY_NUMBER,
			setup:        func(plog.LogRecord) {},
			wantValue:    int64(0),
			emptyMatcher: equalsInt(0),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lr := plog.NewLogRecord()
			tt.setup(lr)
			target := recordFieldTarget(tt.field)

			val, exists := mustExtract(t, target)(logContext(lr, scope, res))
			assert.False(t, exists, "default value must report exists=false")
			assert.Equal(t, tt.wantValue, val, "default value must still be present for value predicates")

			// exists is false ...
			existsPol := mustDropPolicy(t, "exists-"+tt.name, target, &policyv1alpha1.LogMatcher_Exists{}, false)
			assert.False(t, matchesLog(existsPol, lr, scope, res))
			assert.Equal(t, googlepolicy.EvalNoMatch, evalLog(existsPol, lr, scope, res))

			// ... which makes `exists` + negate the "field is unset" selector.
			negatedPol := mustDropPolicy(t, "not-exists-"+tt.name, target, &policyv1alpha1.LogMatcher_Exists{}, true)
			assert.True(t, matchesLog(negatedPol, lr, scope, res))
			assert.Equal(t, googlepolicy.EvalDrop, evalLog(negatedPol, lr, scope, res))

			// ... yet the value predicates still see the zero value.
			equalsPol := mustDropPolicy(t, "equals-empty-"+tt.name, target, tt.emptyMatcher, false)
			assert.True(t, matchesLog(equalsPol, lr, scope, res))
			assert.Equal(t, googlepolicy.EvalDrop, evalLog(equalsPol, lr, scope, res))
		})
	}

	t.Run("empty severity text regex anchored empty", func(t *testing.T) {
		lr := plog.NewLogRecord()
		pol := mustDropPolicy(
			t,
			"severity-text-empty-regex",
			recordFieldTarget(policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SEVERITY_TEXT),
			&policyv1alpha1.LogMatcher_Regex{Regex: "^$"},
			false,
		)
		assert.True(t, matchesLog(pol, lr, scope, res))
	})
}

// TestIDFieldsMatchHexAndBytes pins the dual representation of the native
// pcommon.TraceID / pcommon.SpanID returned by the ID extractors: string
// predicates see the 32/16-character lowercase hex spelling required by
// log_filter_policy.proto:144-154, bytes predicates see the raw bytes.
func TestIDFieldsMatchHexAndBytes(t *testing.T) {
	log, scope, res := newTestLogBundle()

	const (
		traceIDHex = "0102030405060708090a0b0c0d0e0f10"
		spanIDHex  = "0102030405060708"
	)
	traceIDBytes := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	spanIDBytes := []byte{1, 2, 3, 4, 5, 6, 7, 8}

	tests := []struct {
		name      string
		field     policyv1alpha1.LogRecordField
		predicate any
		want      bool
	}{
		{
			name:      "trace id equals hex string",
			field:     policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_TRACE_ID,
			predicate: equalsString(traceIDHex),
			want:      true,
		},
		{
			name:      "trace id regex on hex string",
			field:     policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_TRACE_ID,
			predicate: &policyv1alpha1.LogMatcher_Regex{Regex: "^0102.*0f10$"},
			want:      true,
		},
		{
			name:      "trace id contains hex substring",
			field:     policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_TRACE_ID,
			predicate: containsString("090a0b0c"),
			want:      true,
		},
		{
			name:      "trace id equals raw bytes",
			field:     policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_TRACE_ID,
			predicate: equalsBytes(traceIDBytes),
			want:      true,
		},
		{
			name:      "trace id contains raw byte subsequence",
			field:     policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_TRACE_ID,
			predicate: containsBytes([]byte{0x03, 0x04, 0x05}),
			want:      true,
		},
		{
			name:      "trace id wrong hex string",
			field:     policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_TRACE_ID,
			predicate: equalsString("ffffffffffffffffffffffffffffffff"),
			want:      false,
		},
		{
			name:      "span id equals hex string",
			field:     policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SPAN_ID,
			predicate: equalsString(spanIDHex),
			want:      true,
		},
		{
			name:      "span id regex on hex string",
			field:     policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SPAN_ID,
			predicate: &policyv1alpha1.LogMatcher_Regex{Regex: "^0102030405060708$"},
			want:      true,
		},
		{
			name:      "span id equals raw bytes",
			field:     policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SPAN_ID,
			predicate: equalsBytes(spanIDBytes),
			want:      true,
		},
		{
			name:      "span id contains raw byte subsequence",
			field:     policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SPAN_ID,
			predicate: containsBytes([]byte{0x06, 0x07, 0x08}),
			want:      true,
		},
		{
			name:      "span id wrong raw bytes",
			field:     policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SPAN_ID,
			predicate: equalsBytes([]byte{0xff, 0xff}),
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pol := mustDropPolicy(t, "id-"+tt.name, recordFieldTarget(tt.field), tt.predicate, false)
			assert.Equal(t, tt.want, matchesLog(pol, log, scope, res))
		})
	}
}

// TestZeroIDsReportAbsent covers log records emitted outside a trace: both ID
// extractors return a genuinely absent (nil, false), so `exists` is false and
// — unlike the empty-string fields above — no value predicate matches, not
// even equals "". The proto grants TRACE_ID / SPAN_ID no empty-string contract.
func TestZeroIDsReportAbsent(t *testing.T) {
	lr := plog.NewLogRecord()
	lr.Body().SetStr("no ids here")
	scope := plog.NewScopeLogs()
	res := plog.NewResourceLogs()

	fields := []policyv1alpha1.LogRecordField{
		policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_TRACE_ID,
		policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SPAN_ID,
	}

	for _, field := range fields {
		target := recordFieldTarget(field)

		t.Run(field.String()+" extracts nil", func(t *testing.T) {
			val, exists := mustExtract(t, target)(logContext(lr, scope, res))
			assert.False(t, exists)
			assert.Nil(t, val)
		})

		t.Run(field.String()+" exists is false", func(t *testing.T) {
			pol := mustDropPolicy(t, "exists-"+field.String(), target, &policyv1alpha1.LogMatcher_Exists{}, false)
			assert.False(t, matchesLog(pol, lr, scope, res))
			assert.Equal(t, googlepolicy.EvalNoMatch, evalLog(pol, lr, scope, res))
		})

		t.Run(field.String()+" exists negated is true", func(t *testing.T) {
			pol := mustDropPolicy(t, "not-exists-"+field.String(), target, &policyv1alpha1.LogMatcher_Exists{}, true)
			assert.True(t, matchesLog(pol, lr, scope, res))
			assert.Equal(t, googlepolicy.EvalDrop, evalLog(pol, lr, scope, res))
		})

		t.Run(field.String()+" does not match empty string", func(t *testing.T) {
			pol := mustDropPolicy(t, "zero-equals-"+field.String(), target, equalsString(""), false)
			assert.False(t, matchesLog(pol, lr, scope, res))

			regexPol := mustDropPolicy(t, "zero-regex-"+field.String(), target, &policyv1alpha1.LogMatcher_Regex{Regex: "^$"}, false)
			assert.False(t, matchesLog(regexPol, lr, scope, res))

			containsPol := mustDropPolicy(t, "zero-contains-"+field.String(), target, containsString(""), false)
			assert.False(t, matchesLog(containsPol, lr, scope, res))
		})
	}
}

// TestEmptyContextFields drives every selector against a zero-valued
// LogContext, where Record, Resource and Scope are all unset pdata handles.
// Each extractor must report the target as absent rather than dereferencing a
// nil pdata struct and panicking.
func TestEmptyContextFields(t *testing.T) {
	tests := []struct {
		name   string
		target *policyv1alpha1.LogFieldSelector
		// wantValue is nil for every selector that guards on a zero-valued
		// pdata handle. SCOPE_FIELD_SCHEMA_URL is the one exception: the
		// schema URL is carried as a plain string on LogContext, so there is
		// no handle to test and it reads as present-but-empty. `exists` is
		// false either way.
		wantValue any
	}{
		{name: "body", target: recordFieldTarget(policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_BODY)},
		{name: "severity text", target: recordFieldTarget(policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SEVERITY_TEXT)},
		{name: "severity number", target: recordFieldTarget(policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SEVERITY_NUMBER)},
		{name: "trace id", target: recordFieldTarget(policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_TRACE_ID)},
		{name: "span id", target: recordFieldTarget(policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SPAN_ID)},
		{name: "log attribute", target: logAttrTarget("http.method")},
		{name: "resource attribute", target: resourceAttrTarget("cloud.zone")},
		{name: "scope attribute", target: scopeAttrTarget("scope.tag")},
		{name: "scope name", target: scopeFieldTarget(policyv1alpha1.ScopeField_SCOPE_FIELD_NAME)},
		{name: "scope version", target: scopeFieldTarget(policyv1alpha1.ScopeField_SCOPE_FIELD_VERSION)},
		{name: "scope schema url", target: scopeFieldTarget(policyv1alpha1.ScopeField_SCOPE_FIELD_SCHEMA_URL), wantValue: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val, exists := mustExtract(t, tt.target)(googlepolicy.LogContext{})
			assert.False(t, exists)
			assert.Equal(t, tt.wantValue, val)

			pol := mustDropPolicy(t, "empty-ctx-"+tt.name, tt.target, &policyv1alpha1.LogMatcher_Exists{}, false)
			assert.Equal(t, googlepolicy.EvalNoMatch, pol.EvaluateLog(googlepolicy.LogContext{}))
		})
	}
}

// TestEmptyScopeFieldsReportAbsent covers a scope that exists but carries no
// name, version, or schema URL.
func TestEmptyScopeFieldsReportAbsent(t *testing.T) {
	lr := plog.NewLogRecord()
	scope := plog.NewScopeLogs()
	res := plog.NewResourceLogs()

	fields := []policyv1alpha1.ScopeField{
		policyv1alpha1.ScopeField_SCOPE_FIELD_NAME,
		policyv1alpha1.ScopeField_SCOPE_FIELD_VERSION,
		policyv1alpha1.ScopeField_SCOPE_FIELD_SCHEMA_URL,
	}

	for _, field := range fields {
		t.Run(field.String(), func(t *testing.T) {
			target := scopeFieldTarget(field)

			val, exists := mustExtract(t, target)(logContext(lr, scope, res))
			assert.False(t, exists)
			assert.Equal(t, "", val)

			pol := mustDropPolicy(t, "scope-"+field.String(), target, &policyv1alpha1.LogMatcher_Exists{}, false)
			assert.False(t, matchesLog(pol, lr, scope, res))

			// Present-but-empty: the value predicates still see "".
			equalsPol := mustDropPolicy(t, "scope-empty-"+field.String(), target, equalsString(""), false)
			assert.True(t, matchesLog(equalsPol, lr, scope, res))
		})
	}
}

// TestNestedAttributePaths exercises AttributePath traversal through maps and
// slices, including every degenerate index form.
func TestNestedAttributePaths(t *testing.T) {
	log, scope, res := newTestLogBundle()
	ctx := logContext(log, scope, res)

	tests := []struct {
		name       string
		path       []string
		wantValue  any
		wantExists bool
	}{
		{name: "top-level scalar", path: []string{"http.method"}, wantValue: "GET", wantExists: true},
		{name: "map descent", path: []string{"metadata", "env"}, wantValue: "prod", wantExists: true},
		{name: "slice index scalar element", path: []string{"user.roles", "1"}, wantValue: "admin", wantExists: true},
		{name: "slice index into nested map", path: []string{"items", "0", "id"}, wantValue: "item-001", wantExists: true},
		{name: "whole slice value", path: []string{"user.roles"}, wantValue: []any{"viewer", "admin"}, wantExists: true},
		{name: "missing top-level key", path: []string{"nope"}, wantValue: nil, wantExists: false},
		{name: "missing nested key", path: []string{"metadata", "nope"}, wantValue: nil, wantExists: false},
		{name: "slice index out of range", path: []string{"items", "5", "id"}, wantValue: nil, wantExists: false},
		{name: "non-numeric index into slice", path: []string{"user.roles", "not_an_int"}, wantValue: nil, wantExists: false},
		{name: "negative index into slice", path: []string{"user.roles", "-1"}, wantValue: nil, wantExists: false},
		{name: "descend into scalar", path: []string{"http.method", "deeper"}, wantValue: nil, wantExists: false},
		{name: "descend into bytes", path: []string{"binary.id", "0"}, wantValue: nil, wantExists: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val, exists := mustExtract(t, logAttrTarget(tt.path...))(ctx)
			assert.Equal(t, tt.wantExists, exists)
			assert.Equal(t, tt.wantValue, val)
		})
	}

	t.Run("resource attribute path", func(t *testing.T) {
		val, exists := mustExtract(t, resourceAttrTarget("cloud.zone"))(ctx)
		assert.True(t, exists)
		assert.Equal(t, "us-central1-a", val)

		val, exists = mustExtract(t, resourceAttrTarget("cloud.zone", "nested"))(ctx)
		assert.False(t, exists)
		assert.Nil(t, val)
	})

	t.Run("scope attribute path", func(t *testing.T) {
		val, exists := mustExtract(t, scopeAttrTarget("scope.tag"))(ctx)
		assert.True(t, exists)
		assert.Equal(t, "backend", val)

		val, exists = mustExtract(t, scopeAttrTarget("missing"))(ctx)
		assert.False(t, exists)
		assert.Nil(t, val)
	})
}

// TestDriverLoadPolicyErrors covers the failure paths of Driver.LoadPolicy.
// Loading goes through googlepolicy.LoadPolicy, the way the policy manager
// does it, so the "type" routing envelope is stripped by the registry before
// the driver decodes with DiscardUnknown: false.
func TestDriverLoadPolicyErrors(t *testing.T) {
	tests := []struct {
		name string
		raw  map[string]any
	}{
		{
			name: "unknown top-level field",
			raw: map[string]any{
				"type":    "log_filter",
				"id":      "typo-policy",
				"actions": "ACTION_DROP", // typo: should be "action"
				"matches": []any{
					map[string]any{
						"target": map[string]any{"record_field": "LOG_RECORD_FIELD_BODY"},
						"exists": map[string]any{},
					},
				},
			},
		},
		{
			name: "typo'd nested field",
			raw: map[string]any{
				"type":   "log_filter",
				"id":     "typo-nested",
				"action": "ACTION_DROP",
				"matches": []any{
					map[string]any{
						"target": map[string]any{
							"recordfield": "LOG_RECORD_FIELD_BODY", // typo
						},
						"exists": map[string]any{},
					},
				},
			},
		},
		{
			name: "invalid enum value",
			raw: map[string]any{
				"type":   "log_filter",
				"id":     "bad-enum",
				"action": "ACTION_NOPE",
			},
		},
		{
			name: "valid json but invalid policy",
			raw: map[string]any{
				"type":   "log_filter",
				"id":     "no-matchers",
				"action": "ACTION_DROP",
			},
		},
		{
			// A config value that cannot be JSON-encoded must surface as an
			// error from the marshal step rather than panicking.
			name: "unmarshalable config value",
			raw: map[string]any{
				"type": "log_filter",
				"id":   make(chan int),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := googlepolicy.LoadPolicy(PolicyType, tt.raw)
			require.Error(t, err)
		})
	}
}

// TestDriverLoadPolicyRejectsTypeEnvelope pins the division of labour between
// the registry and the driver: stripping the "type" routing key is the
// registry's job (googlepolicy.LoadPolicy), and the driver decodes strictly, so
// a caller that hands the envelope straight to the driver gets an error rather
// than a silently ignored field.
func TestDriverLoadPolicyRejectsTypeEnvelope(t *testing.T) {
	raw := map[string]any{
		"type":   "log_filter",
		"id":     "envelope-not-stripped",
		"action": "ACTION_DROP",
		"matches": []any{
			map[string]any{
				"target": map[string]any{"record_field": "LOG_RECORD_FIELD_BODY"},
				"exists": map[string]any{},
			},
		},
	}

	_, err := (&Driver{}).LoadPolicy(raw)
	require.Error(t, err)

	// The same config with the envelope stripped — exactly what
	// googlepolicy.LoadPolicy hands the driver — loads cleanly.
	delete(raw, "type")
	p, err := (&Driver{}).LoadPolicy(raw)
	require.NoError(t, err)
	assert.Equal(t, "envelope-not-stripped", p.PolicyName())
}
