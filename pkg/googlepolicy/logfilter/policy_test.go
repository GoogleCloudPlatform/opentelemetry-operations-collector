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
			errExpect: ErrMissingMatches,
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
		assert.Equal(t, PolicyTypeShort, pol.PolicyType())
		assert.Equal(t, googlepolicy.PolicyClassTransformation, pol.PolicyClass())
		assert.NoError(t, pol.Validate())

		assert.True(t, pol.Matches(log, scope, res))
		assert.True(t, pol.ShouldDrop(log, scope, res))
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

		assert.False(t, pol.Matches(log, scope, res))
		assert.False(t, pol.ShouldDrop(log, scope, res))
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

		assert.True(t, pol.Matches(log, scope, res))
		// Since it matches the KEEP policy, it should NOT be dropped!
		assert.False(t, pol.ShouldDrop(log, scope, res))
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

		assert.False(t, pol.Matches(log, scope, res))
		// Since it does not match the KEEP policy, it SHOULD be dropped!
		assert.True(t, pol.ShouldDrop(log, scope, res))
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
		assert.True(t, pol.Matches(log, scope, res))

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
		assert.False(t, pol2.Matches(log, scope, res))
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
		assert.True(t, pol.Matches(log, scope, res))
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
		assert.True(t, pol.Matches(log, scope, res))
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
		assert.True(t, pol.Matches(log, scope, res))
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
		assert.True(t, pol.Matches(log, scope, res))
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
		assert.True(t, pol.Matches(log, scope, res))
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
		assert.True(t, pol.Matches(log, scope, res))

		// Non-matching list item
		pb.Matches[0].Predicate = &policyv1alpha1.LogMatcher_Contains{
			Contains: &policyv1alpha1.Value{Value: &policyv1alpha1.Value_StringValue{StringValue: "superadmin"}},
		}
		pol2, err := NewPolicyFromProto(pb)
		require.NoError(t, err)
		assert.False(t, pol2.Matches(log, scope, res))
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

	lrf, ok := p.(googlepolicy.LogRecordFilter)
	require.True(t, ok)

	log, scope, res := newTestLogBundle()
	log.Body().SetStr("drop this line")
	assert.True(t, lrf.ShouldDrop(log, scope, res))

	log.Body().SetStr("keep this line")
	assert.False(t, lrf.ShouldDrop(log, scope, res))
}
