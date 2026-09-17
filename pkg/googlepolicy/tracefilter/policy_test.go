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

package tracefilter

import (
	"testing"

	policyv1alpha1 "github.com/GoogleCloudPlatform/opentelemetry-operations-collector/gen/go/policy/v1alpha1"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"
)

var (
	// testTraceIDBytes renders as "0102030405060708090a0b0c0d0e0f10".
	testTraceIDBytes = [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	// testSpanIDBytes renders as "0102030405060708".
	testSpanIDBytes = [8]byte{1, 2, 3, 4, 5, 6, 7, 8}
	// testParentSpanIDBytes renders as "0a0b0c0d0e0f1011".
	testParentSpanIDBytes = [8]byte{10, 11, 12, 13, 14, 15, 16, 17}
)

const (
	testTraceIDHex      = "0102030405060708090a0b0c0d0e0f10"
	testSpanIDHex       = "0102030405060708"
	testParentSpanIDHex = "0a0b0c0d0e0f1011"
)

func newTestSpanBundle() (ptrace.Span, ptrace.ScopeSpans, ptrace.ResourceSpans) {
	td := ptrace.NewTraces()
	rs := td.ResourceSpans().AppendEmpty()
	rs.Resource().Attributes().PutStr("cloud.zone", "us-central1-a")

	ss := rs.ScopeSpans().AppendEmpty()
	ss.Scope().SetName("my.library")
	ss.Scope().SetVersion("v1.2.3")
	ss.SetSchemaUrl("https://opentelemetry.io/schemas/1.24.0")
	ss.Scope().Attributes().PutStr("scope.tag", "backend")

	span := ss.Spans().AppendEmpty()
	span.SetName("healthcheck.ping")
	span.SetKind(ptrace.SpanKindServer)
	span.SetTraceID(pcommon.TraceID(testTraceIDBytes))
	span.SetSpanID(pcommon.SpanID(testSpanIDBytes))
	span.SetParentSpanID(pcommon.SpanID(testParentSpanIDBytes))
	span.Status().SetCode(ptrace.StatusCodeError)
	span.Status().SetMessage("upstream unavailable")
	span.Attributes().PutStr("http.method", "GET")
	span.Attributes().PutInt("http.status_code", 200)
	span.Attributes().PutDouble("latency_seconds", 1.25)
	span.Attributes().PutBool("feature.enabled", true)
	span.Attributes().PutEmptyBytes("binary.id").FromRaw([]byte{0x01, 0x02, 0x03})

	roles := span.Attributes().PutEmptySlice("user.roles")
	roles.AppendEmpty().SetStr("viewer")
	roles.AppendEmpty().SetStr("admin")

	meta := span.Attributes().PutEmptyMap("metadata")
	meta.PutStr("env", "prod")

	items := span.Attributes().PutEmptySlice("items")
	item0 := items.AppendEmpty().SetEmptyMap()
	item0.PutStr("id", "item-001")

	return span, ss, rs
}

func traceContext(span ptrace.Span, scope ptrace.ScopeSpans, res ptrace.ResourceSpans) googlepolicy.TraceContext {
	return googlepolicy.TraceContext{
		Span:              span,
		Resource:          res.Resource(),
		Scope:             scope.Scope(),
		ResourceSchemaURL: res.SchemaUrl(),
		ScopeSchemaURL:    scope.SchemaUrl(),
	}
}

func evalTrace(pol *Policy, span ptrace.Span, scope ptrace.ScopeSpans, res ptrace.ResourceSpans) googlepolicy.EvalResult {
	return pol.EvaluateTrace(traceContext(span, scope, res))
}

func matchesTrace(pol *Policy, span ptrace.Span, scope ptrace.ScopeSpans, res ptrace.ResourceSpans) bool {
	return pol.matchesContext(traceContext(span, scope, res))
}

// dropPolicy builds a single-matcher ACTION_DROP policy around target/predicate.
func dropPolicy(id string, target *policyv1alpha1.TraceFieldSelector, predicate any, negate bool) *policyv1alpha1.TraceFilterPolicy {
	m := &policyv1alpha1.TraceMatcher{
		Target: target,
		Negate: negate,
	}
	switch p := predicate.(type) {
	case *policyv1alpha1.TraceMatcher_Exists:
		m.Predicate = p
	case *policyv1alpha1.TraceMatcher_Equals:
		m.Predicate = p
	case *policyv1alpha1.TraceMatcher_Regex:
		m.Predicate = p
	case *policyv1alpha1.TraceMatcher_Contains:
		m.Predicate = p
	case *policyv1alpha1.TraceMatcher_Gt:
		m.Predicate = p
	case *policyv1alpha1.TraceMatcher_Gte:
		m.Predicate = p
	case *policyv1alpha1.TraceMatcher_Lt:
		m.Predicate = p
	case *policyv1alpha1.TraceMatcher_Lte:
		m.Predicate = p
	}
	return &policyv1alpha1.TraceFilterPolicy{
		Id:      id,
		Action:  policyv1alpha1.Action_ACTION_DROP.Enum(),
		Matches: []*policyv1alpha1.TraceMatcher{m},
	}
}

func recordFieldTarget(f policyv1alpha1.SpanRecordField) *policyv1alpha1.TraceFieldSelector {
	return &policyv1alpha1.TraceFieldSelector{
		Target: &policyv1alpha1.TraceFieldSelector_RecordField{RecordField: f},
	}
}

func scopeFieldTarget(f policyv1alpha1.ScopeField) *policyv1alpha1.TraceFieldSelector {
	return &policyv1alpha1.TraceFieldSelector{
		Target: &policyv1alpha1.TraceFieldSelector_ScopeField{ScopeField: f},
	}
}

func spanAttrTarget(path ...string) *policyv1alpha1.TraceFieldSelector {
	return &policyv1alpha1.TraceFieldSelector{
		Target: &policyv1alpha1.TraceFieldSelector_SpanAttribute{
			SpanAttribute: &policyv1alpha1.AttributePath{Path: path},
		},
	}
}

func equalsString(s string) *policyv1alpha1.TraceMatcher_Equals {
	return &policyv1alpha1.TraceMatcher_Equals{
		Equals: &policyv1alpha1.Value{Value: &policyv1alpha1.Value_StringValue{StringValue: s}},
	}
}

func TestPolicyValidationErrors(t *testing.T) {
	tests := []struct {
		name      string
		proto     *policyv1alpha1.TraceFilterPolicy
		errExpect error
	}{
		{
			name:      "nil proto",
			proto:     nil,
			errExpect: ErrNilProto,
		},
		{
			name: "missing id",
			proto: &policyv1alpha1.TraceFilterPolicy{
				Id:     "",
				Action: policyv1alpha1.Action_ACTION_DROP.Enum(),
				Matches: []*policyv1alpha1.TraceMatcher{
					{
						Target:    recordFieldTarget(policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_NAME),
						Predicate: &policyv1alpha1.TraceMatcher_Exists{},
					},
				},
			},
			errExpect: ErrMissingID,
		},
		{
			name: "missing action",
			proto: &policyv1alpha1.TraceFilterPolicy{
				Id:     "p1",
				Action: policyv1alpha1.Action_ACTION_UNSPECIFIED.Enum(),
				Matches: []*policyv1alpha1.TraceMatcher{
					{
						Target:    recordFieldTarget(policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_NAME),
						Predicate: &policyv1alpha1.TraceMatcher_Exists{},
					},
				},
			},
			errExpect: ErrMissingAction,
		},
		{
			name: "invalid action value",
			proto: &policyv1alpha1.TraceFilterPolicy{
				Id:     "p1-invalid-action",
				Action: policyv1alpha1.Action(99).Enum(),
				Matches: []*policyv1alpha1.TraceMatcher{
					{
						Target:    recordFieldTarget(policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_NAME),
						Predicate: &policyv1alpha1.TraceMatcher_Exists{},
					},
				},
			},
			errExpect: ErrMissingAction,
		},
		{
			name: "missing matchers",
			proto: &policyv1alpha1.TraceFilterPolicy{
				Id:      "p2",
				Action:  policyv1alpha1.Action_ACTION_DROP.Enum(),
				Matches: []*policyv1alpha1.TraceMatcher{},
			},
			errExpect: ErrMissingMatchers,
		},
		{
			name: "matcher missing target",
			proto: &policyv1alpha1.TraceFilterPolicy{
				Id:     "p3",
				Action: policyv1alpha1.Action_ACTION_DROP.Enum(),
				Matches: []*policyv1alpha1.TraceMatcher{
					{
						Predicate: &policyv1alpha1.TraceMatcher_Exists{},
					},
				},
			},
			errExpect: ErrMissingTarget,
		},
		{
			name: "matcher empty target oneof",
			proto: &policyv1alpha1.TraceFilterPolicy{
				Id:     "p3-empty-oneof",
				Action: policyv1alpha1.Action_ACTION_DROP.Enum(),
				Matches: []*policyv1alpha1.TraceMatcher{
					{
						Target:    &policyv1alpha1.TraceFieldSelector{},
						Predicate: &policyv1alpha1.TraceMatcher_Exists{},
					},
				},
			},
			errExpect: ErrMissingTarget,
		},
		{
			name: "matcher unspecified span record field",
			proto: &policyv1alpha1.TraceFilterPolicy{
				Id:     "p3-unspecified-record",
				Action: policyv1alpha1.Action_ACTION_DROP.Enum(),
				Matches: []*policyv1alpha1.TraceMatcher{
					{
						Target:    recordFieldTarget(policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_UNSPECIFIED),
						Predicate: &policyv1alpha1.TraceMatcher_Exists{},
					},
				},
			},
			errExpect: assert.AnError,
		},
		{
			name: "matcher unspecified scope field",
			proto: &policyv1alpha1.TraceFilterPolicy{
				Id:     "p3-unspecified-scope",
				Action: policyv1alpha1.Action_ACTION_DROP.Enum(),
				Matches: []*policyv1alpha1.TraceMatcher{
					{
						Target:    scopeFieldTarget(policyv1alpha1.ScopeField_SCOPE_FIELD_UNSPECIFIED),
						Predicate: &policyv1alpha1.TraceMatcher_Exists{},
					},
				},
			},
			errExpect: assert.AnError,
		},
		{
			name: "matcher missing predicate",
			proto: &policyv1alpha1.TraceFilterPolicy{
				Id:     "p4",
				Action: policyv1alpha1.Action_ACTION_DROP.Enum(),
				Matches: []*policyv1alpha1.TraceMatcher{
					{
						Target: recordFieldTarget(policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_NAME),
					},
				},
			},
			errExpect: ErrMissingPredicate,
		},
		{
			name: "matcher invalid regex",
			proto: &policyv1alpha1.TraceFilterPolicy{
				Id:     "p5",
				Action: policyv1alpha1.Action_ACTION_DROP.Enum(),
				Matches: []*policyv1alpha1.TraceMatcher{
					{
						Target:    recordFieldTarget(policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_NAME),
						Predicate: &policyv1alpha1.TraceMatcher_Regex{Regex: "[invalid(regex"},
					},
				},
			},
			errExpect: assert.AnError,
		},
		{
			name: "matcher empty span attribute path",
			proto: &policyv1alpha1.TraceFilterPolicy{
				Id:     "p6-span-attr",
				Action: policyv1alpha1.Action_ACTION_DROP.Enum(),
				Matches: []*policyv1alpha1.TraceMatcher{
					{
						Target: &policyv1alpha1.TraceFieldSelector{
							Target: &policyv1alpha1.TraceFieldSelector_SpanAttribute{
								SpanAttribute: &policyv1alpha1.AttributePath{},
							},
						},
						Predicate: &policyv1alpha1.TraceMatcher_Exists{},
					},
				},
			},
			errExpect: assert.AnError,
		},
		{
			name: "matcher empty resource attribute path",
			proto: &policyv1alpha1.TraceFilterPolicy{
				Id:     "p6-res-attr",
				Action: policyv1alpha1.Action_ACTION_DROP.Enum(),
				Matches: []*policyv1alpha1.TraceMatcher{
					{
						Target: &policyv1alpha1.TraceFieldSelector{
							Target: &policyv1alpha1.TraceFieldSelector_ResourceAttribute{
								ResourceAttribute: &policyv1alpha1.AttributePath{},
							},
						},
						Predicate: &policyv1alpha1.TraceMatcher_Exists{},
					},
				},
			},
			errExpect: assert.AnError,
		},
		{
			name: "matcher empty scope attribute path",
			proto: &policyv1alpha1.TraceFilterPolicy{
				Id:     "p6-scope-attr",
				Action: policyv1alpha1.Action_ACTION_DROP.Enum(),
				Matches: []*policyv1alpha1.TraceMatcher{
					{
						Target: &policyv1alpha1.TraceFieldSelector{
							Target: &policyv1alpha1.TraceFieldSelector_ScopeAttribute{
								ScopeAttribute: &policyv1alpha1.AttributePath{},
							},
						},
						Predicate: &policyv1alpha1.TraceMatcher_Exists{},
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
	span, scope, res := newTestSpanBundle()

	t.Run("Action DROP matching span", func(t *testing.T) {
		pb := dropPolicy(
			"drop-healthcheck",
			recordFieldTarget(policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_NAME),
			&policyv1alpha1.TraceMatcher_Regex{Regex: "^healthcheck"},
			false,
		)

		pol, err := NewPolicyFromProto(pb)
		require.NoError(t, err)
		assert.Equal(t, "drop-healthcheck", pol.PolicyName())
		assert.Equal(t, PolicyType, pol.PolicyType())
		assert.Equal(t, googlepolicy.PolicyClassTransformation, pol.PolicyClass())
		assert.Equal(t, []googlepolicy.Signal{googlepolicy.SignalTraces}, pol.TargetSignals())
		assert.Equal(t, policyv1alpha1.Action_ACTION_DROP, pol.Action())
		assert.NotNil(t, pol.Proto())
		assert.NoError(t, pol.Validate())

		assert.True(t, matchesTrace(pol, span, scope, res))
		assert.Equal(t, googlepolicy.EvalDrop, evalTrace(pol, span, scope, res))
	})

	t.Run("Action DROP non-matching span", func(t *testing.T) {
		pb := dropPolicy(
			"drop-non-matching",
			recordFieldTarget(policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_NAME),
			equalsString("not the span name"),
			false,
		)

		pol, err := NewPolicyFromProto(pb)
		require.NoError(t, err)

		assert.False(t, matchesTrace(pol, span, scope, res))
		assert.Equal(t, googlepolicy.EvalNoMatch, evalTrace(pol, span, scope, res))
	})

	t.Run("Action KEEP matching span", func(t *testing.T) {
		pb := dropPolicy(
			"keep-errors",
			recordFieldTarget(policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_STATUS_CODE),
			equalsString("ERROR"),
			false,
		)
		pb.Action = policyv1alpha1.Action_ACTION_KEEP.Enum()

		pol, err := NewPolicyFromProto(pb)
		require.NoError(t, err)

		assert.True(t, matchesTrace(pol, span, scope, res))
		// Matching KEEP policy evaluates to EvalKeep (exemption)
		assert.Equal(t, googlepolicy.EvalKeep, evalTrace(pol, span, scope, res))
	})

	t.Run("Action KEEP non-matching span", func(t *testing.T) {
		pb := dropPolicy(
			"keep-ok-only",
			recordFieldTarget(policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_STATUS_CODE),
			equalsString("OK"),
			false,
		)
		pb.Action = policyv1alpha1.Action_ACTION_KEEP.Enum()

		pol, err := NewPolicyFromProto(pb)
		require.NoError(t, err)

		assert.False(t, matchesTrace(pol, span, scope, res))
		// KEEP is an exemption: a non-matching KEEP policy never prunes the span.
		assert.Equal(t, googlepolicy.EvalNoMatch, evalTrace(pol, span, scope, res))
	})

	t.Run("Multiple matchers AND semantics", func(t *testing.T) {
		pb := &policyv1alpha1.TraceFilterPolicy{
			Id:     "and-matchers",
			Action: policyv1alpha1.Action_ACTION_DROP.Enum(),
			Matches: []*policyv1alpha1.TraceMatcher{
				{
					Target:    recordFieldTarget(policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_NAME),
					Predicate: equalsString("healthcheck.ping"),
				},
				{
					Target:    recordFieldTarget(policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_KIND),
					Predicate: equalsString("SERVER"),
				},
			},
		}

		pol, err := NewPolicyFromProto(pb)
		require.NoError(t, err)
		assert.True(t, matchesTrace(pol, span, scope, res))

		// If one matcher does not match, the entire policy should not match.
		pb.Matches[1].Predicate = equalsString("CLIENT")
		pol2, err := NewPolicyFromProto(pb)
		require.NoError(t, err)
		assert.False(t, matchesTrace(pol2, span, scope, res))
		assert.Equal(t, googlepolicy.EvalNoMatch, evalTrace(pol2, span, scope, res))
	})

	t.Run("Negate matcher", func(t *testing.T) {
		pb := dropPolicy(
			"negate-test",
			spanAttrTarget("http.method"),
			equalsString("POST"), // does not match GET
			true,                 // negated -> true
		)

		pol, err := NewPolicyFromProto(pb)
		require.NoError(t, err)
		assert.True(t, matchesTrace(pol, span, scope, res))
		assert.Equal(t, googlepolicy.EvalDrop, evalTrace(pol, span, scope, res))

		// Un-negated, the same matcher does not match.
		pb.Matches[0].Negate = false
		pol2, err := NewPolicyFromProto(pb)
		require.NoError(t, err)
		assert.False(t, matchesTrace(pol2, span, scope, res))
	})

	t.Run("Span, resource and scope attribute selectors", func(t *testing.T) {
		pb := &policyv1alpha1.TraceFilterPolicy{
			Id:     "attr-selectors",
			Action: policyv1alpha1.Action_ACTION_DROP.Enum(),
			Matches: []*policyv1alpha1.TraceMatcher{
				{
					Target:    spanAttrTarget("http.method"),
					Predicate: equalsString("GET"),
				},
				{
					Target: &policyv1alpha1.TraceFieldSelector{
						Target: &policyv1alpha1.TraceFieldSelector_ResourceAttribute{
							ResourceAttribute: &policyv1alpha1.AttributePath{Path: []string{"cloud.zone"}},
						},
					},
					Predicate: equalsString("us-central1-a"),
				},
				{
					Target: &policyv1alpha1.TraceFieldSelector{
						Target: &policyv1alpha1.TraceFieldSelector_ScopeAttribute{
							ScopeAttribute: &policyv1alpha1.AttributePath{Path: []string{"scope.tag"}},
						},
					},
					Predicate: equalsString("backend"),
				},
			},
		}

		pol, err := NewPolicyFromProto(pb)
		require.NoError(t, err)
		assert.True(t, matchesTrace(pol, span, scope, res))
	})

	t.Run("Scope field selectors", func(t *testing.T) {
		pb := &policyv1alpha1.TraceFilterPolicy{
			Id:     "scope-fields",
			Action: policyv1alpha1.Action_ACTION_DROP.Enum(),
			Matches: []*policyv1alpha1.TraceMatcher{
				{
					Target:    scopeFieldTarget(policyv1alpha1.ScopeField_SCOPE_FIELD_NAME),
					Predicate: equalsString("my.library"),
				},
				{
					Target:    scopeFieldTarget(policyv1alpha1.ScopeField_SCOPE_FIELD_VERSION),
					Predicate: equalsString("v1.2.3"),
				},
				{
					Target:    scopeFieldTarget(policyv1alpha1.ScopeField_SCOPE_FIELD_SCHEMA_URL),
					Predicate: equalsString("https://opentelemetry.io/schemas/1.24.0"),
				},
			},
		}

		pol, err := NewPolicyFromProto(pb)
		require.NoError(t, err)
		assert.True(t, matchesTrace(pol, span, scope, res))
	})

	t.Run("Record field selectors", func(t *testing.T) {
		pb := &policyv1alpha1.TraceFilterPolicy{
			Id:     "record-fields",
			Action: policyv1alpha1.Action_ACTION_DROP.Enum(),
			Matches: []*policyv1alpha1.TraceMatcher{
				{
					Target:    recordFieldTarget(policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_NAME),
					Predicate: equalsString("healthcheck.ping"),
				},
				{
					Target:    recordFieldTarget(policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_KIND),
					Predicate: equalsString("SERVER"),
				},
				{
					Target:    recordFieldTarget(policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_STATUS_CODE),
					Predicate: equalsString("ERROR"),
				},
				{
					Target:    recordFieldTarget(policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_STATUS_MESSAGE),
					Predicate: equalsString("upstream unavailable"),
				},
				{
					Target:    recordFieldTarget(policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_TRACE_ID),
					Predicate: &policyv1alpha1.TraceMatcher_Exists{},
				},
				{
					Target:    recordFieldTarget(policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_SPAN_ID),
					Predicate: &policyv1alpha1.TraceMatcher_Exists{},
				},
				{
					Target:    recordFieldTarget(policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_PARENT_SPAN_ID),
					Predicate: &policyv1alpha1.TraceMatcher_Exists{},
				},
			},
		}

		pol, err := NewPolicyFromProto(pb)
		require.NoError(t, err)
		assert.True(t, matchesTrace(pol, span, scope, res))
	})

	t.Run("Typed span attribute matchers", func(t *testing.T) {
		pb := &policyv1alpha1.TraceFilterPolicy{
			Id:     "typed-test",
			Action: policyv1alpha1.Action_ACTION_DROP.Enum(),
			Matches: []*policyv1alpha1.TraceMatcher{
				{
					Target: spanAttrTarget("feature.enabled"),
					Predicate: &policyv1alpha1.TraceMatcher_Equals{
						Equals: &policyv1alpha1.Value{Value: &policyv1alpha1.Value_BoolValue{BoolValue: true}},
					},
				},
				{
					Target: spanAttrTarget("latency_seconds"),
					Predicate: &policyv1alpha1.TraceMatcher_Equals{
						Equals: &policyv1alpha1.Value{Value: &policyv1alpha1.Value_DoubleValue{DoubleValue: 1.25}},
					},
				},
				{
					Target: spanAttrTarget("http.status_code"),
					Predicate: &policyv1alpha1.TraceMatcher_Equals{
						Equals: &policyv1alpha1.Value{Value: &policyv1alpha1.Value_IntValue{IntValue: 200}},
					},
				},
				{
					Target: spanAttrTarget("binary.id"),
					Predicate: &policyv1alpha1.TraceMatcher_Equals{
						Equals: &policyv1alpha1.Value{Value: &policyv1alpha1.Value_BytesValue{BytesValue: []byte{0x01, 0x02, 0x03}}},
					},
				},
			},
		}

		pol, err := NewPolicyFromProto(pb)
		require.NoError(t, err)
		assert.True(t, matchesTrace(pol, span, scope, res))
	})

	t.Run("Numeric comparisons (gt, gte, lt, lte)", func(t *testing.T) {
		pb := &policyv1alpha1.TraceFilterPolicy{
			Id:     "numeric-test",
			Action: policyv1alpha1.Action_ACTION_DROP.Enum(),
			Matches: []*policyv1alpha1.TraceMatcher{
				{
					Target: spanAttrTarget("http.status_code"),
					Predicate: &policyv1alpha1.TraceMatcher_Gte{
						Gte: &policyv1alpha1.NumericValue{Value: &policyv1alpha1.NumericValue_IntValue{IntValue: 200}},
					},
				},
				{
					Target: spanAttrTarget("http.status_code"),
					Predicate: &policyv1alpha1.TraceMatcher_Lt{
						Lt: &policyv1alpha1.NumericValue{Value: &policyv1alpha1.NumericValue_IntValue{IntValue: 300}},
					},
				},
				{
					Target: spanAttrTarget("latency_seconds"),
					Predicate: &policyv1alpha1.TraceMatcher_Gt{
						Gt: &policyv1alpha1.NumericValue{Value: &policyv1alpha1.NumericValue_DoubleValue{DoubleValue: 1.0}},
					},
				},
				{
					Target: spanAttrTarget("latency_seconds"),
					Predicate: &policyv1alpha1.TraceMatcher_Lte{
						Lte: &policyv1alpha1.NumericValue{Value: &policyv1alpha1.NumericValue_DoubleValue{DoubleValue: 1.25}},
					},
				},
			},
		}

		pol, err := NewPolicyFromProto(pb)
		require.NoError(t, err)
		assert.True(t, matchesTrace(pol, span, scope, res))
	})

	t.Run("Contains predicate (list membership, string substring, bytes)", func(t *testing.T) {
		pb := &policyv1alpha1.TraceFilterPolicy{
			Id:     "contains-test",
			Action: policyv1alpha1.Action_ACTION_DROP.Enum(),
			Matches: []*policyv1alpha1.TraceMatcher{
				// Array membership: user.roles contains "admin".
				{
					Target: spanAttrTarget("user.roles"),
					Predicate: &policyv1alpha1.TraceMatcher_Contains{
						Contains: &policyv1alpha1.Value{Value: &policyv1alpha1.Value_StringValue{StringValue: "admin"}},
					},
				},
				// Substring: span name contains "check.p".
				{
					Target: recordFieldTarget(policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_NAME),
					Predicate: &policyv1alpha1.TraceMatcher_Contains{
						Contains: &policyv1alpha1.Value{Value: &policyv1alpha1.Value_StringValue{StringValue: "check.p"}},
					},
				},
				// Bytes subsequence: binary.id contains [0x02, 0x03].
				{
					Target: spanAttrTarget("binary.id"),
					Predicate: &policyv1alpha1.TraceMatcher_Contains{
						Contains: &policyv1alpha1.Value{Value: &policyv1alpha1.Value_BytesValue{BytesValue: []byte{0x02, 0x03}}},
					},
				},
			},
		}

		pol, err := NewPolicyFromProto(pb)
		require.NoError(t, err)
		assert.True(t, matchesTrace(pol, span, scope, res))

		// Non-matching list item.
		pb.Matches[0].Predicate = &policyv1alpha1.TraceMatcher_Contains{
			Contains: &policyv1alpha1.Value{Value: &policyv1alpha1.Value_StringValue{StringValue: "superadmin"}},
		}
		pol2, err := NewPolicyFromProto(pb)
		require.NoError(t, err)
		assert.False(t, matchesTrace(pol2, span, scope, res))
	})
}

// TestIDFieldsMatchHexAndBytes pins the dual representation of the native
// pcommon.TraceID / pcommon.SpanID values returned by the ID extractors: string
// predicates see the lowercase hex spelling, bytes predicates see the raw bytes.
func TestIDFieldsMatchHexAndBytes(t *testing.T) {
	span, scope, res := newTestSpanBundle()

	tests := []struct {
		name      string
		field     policyv1alpha1.SpanRecordField
		predicate any
	}{
		{
			name:      "trace id equals hex string",
			field:     policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_TRACE_ID,
			predicate: equalsString(testTraceIDHex),
		},
		{
			name:      "trace id regex on hex string",
			field:     policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_TRACE_ID,
			predicate: &policyv1alpha1.TraceMatcher_Regex{Regex: "^0102.*0f10$"},
		},
		{
			name:  "trace id contains hex substring",
			field: policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_TRACE_ID,
			predicate: &policyv1alpha1.TraceMatcher_Contains{
				Contains: &policyv1alpha1.Value{Value: &policyv1alpha1.Value_StringValue{StringValue: "090a0b0c"}},
			},
		},
		{
			name:  "trace id equals raw bytes",
			field: policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_TRACE_ID,
			predicate: &policyv1alpha1.TraceMatcher_Equals{
				Equals: &policyv1alpha1.Value{Value: &policyv1alpha1.Value_BytesValue{BytesValue: testTraceIDBytes[:]}},
			},
		},
		{
			name:  "trace id contains raw byte subsequence",
			field: policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_TRACE_ID,
			predicate: &policyv1alpha1.TraceMatcher_Contains{
				Contains: &policyv1alpha1.Value{Value: &policyv1alpha1.Value_BytesValue{BytesValue: []byte{0x03, 0x04, 0x05}}},
			},
		},
		{
			name:      "span id equals hex string",
			field:     policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_SPAN_ID,
			predicate: equalsString(testSpanIDHex),
		},
		{
			name:      "span id regex on hex string",
			field:     policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_SPAN_ID,
			predicate: &policyv1alpha1.TraceMatcher_Regex{Regex: "^0102030405060708$"},
		},
		{
			name:  "span id equals raw bytes",
			field: policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_SPAN_ID,
			predicate: &policyv1alpha1.TraceMatcher_Equals{
				Equals: &policyv1alpha1.Value{Value: &policyv1alpha1.Value_BytesValue{BytesValue: testSpanIDBytes[:]}},
			},
		},
		{
			name:  "span id contains raw byte subsequence",
			field: policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_SPAN_ID,
			predicate: &policyv1alpha1.TraceMatcher_Contains{
				Contains: &policyv1alpha1.Value{Value: &policyv1alpha1.Value_BytesValue{BytesValue: []byte{0x06, 0x07, 0x08}}},
			},
		},
		{
			name:      "parent span id equals hex string",
			field:     policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_PARENT_SPAN_ID,
			predicate: equalsString(testParentSpanIDHex),
		},
		{
			name:      "parent span id regex on hex string",
			field:     policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_PARENT_SPAN_ID,
			predicate: &policyv1alpha1.TraceMatcher_Regex{Regex: "^0a0b"},
		},
		{
			name:  "parent span id equals raw bytes",
			field: policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_PARENT_SPAN_ID,
			predicate: &policyv1alpha1.TraceMatcher_Equals{
				Equals: &policyv1alpha1.Value{Value: &policyv1alpha1.Value_BytesValue{BytesValue: testParentSpanIDBytes[:]}},
			},
		},
		{
			name:  "parent span id contains raw byte subsequence",
			field: policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_PARENT_SPAN_ID,
			predicate: &policyv1alpha1.TraceMatcher_Contains{
				Contains: &policyv1alpha1.Value{Value: &policyv1alpha1.Value_BytesValue{BytesValue: []byte{0x0f, 0x10, 0x11}}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pol, err := NewPolicyFromProto(dropPolicy("id-"+tt.name, recordFieldTarget(tt.field), tt.predicate, false))
			require.NoError(t, err)
			assert.True(t, matchesTrace(pol, span, scope, res))
		})
	}

	t.Run("wrong hex string does not match", func(t *testing.T) {
		pol, err := NewPolicyFromProto(dropPolicy(
			"wrong-hex",
			recordFieldTarget(policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_TRACE_ID),
			equalsString("ffffffffffffffffffffffffffffffff"),
			false,
		))
		require.NoError(t, err)
		assert.False(t, matchesTrace(pol, span, scope, res))
	})

	t.Run("wrong raw bytes do not match", func(t *testing.T) {
		pol, err := NewPolicyFromProto(dropPolicy(
			"wrong-bytes",
			recordFieldTarget(policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_SPAN_ID),
			&policyv1alpha1.TraceMatcher_Equals{
				Equals: &policyv1alpha1.Value{Value: &policyv1alpha1.Value_BytesValue{BytesValue: []byte{0xff, 0xff}}},
			},
			false,
		))
		require.NoError(t, err)
		assert.False(t, matchesTrace(pol, span, scope, res))
	})
}

// TestZeroIDsReportAbsent covers root spans and spans emitted without IDs: the
// extractors report exists=false, so `exists` is false and `exists` + negate is
// true (the documented way to target root spans).
func TestZeroIDsReportAbsent(t *testing.T) {
	bareSpan := ptrace.NewSpan()
	bareSpan.SetName("root")
	bareScope := ptrace.NewScopeSpans()
	bareRes := ptrace.NewResourceSpans()

	fields := []policyv1alpha1.SpanRecordField{
		policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_TRACE_ID,
		policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_SPAN_ID,
		policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_PARENT_SPAN_ID,
	}

	for _, field := range fields {
		t.Run(field.String()+" absent", func(t *testing.T) {
			pol, err := NewPolicyFromProto(dropPolicy(
				"exists-"+field.String(),
				recordFieldTarget(field),
				&policyv1alpha1.TraceMatcher_Exists{},
				false,
			))
			require.NoError(t, err)
			assert.False(t, matchesTrace(pol, bareSpan, bareScope, bareRes))
			assert.Equal(t, googlepolicy.EvalNoMatch, evalTrace(pol, bareSpan, bareScope, bareRes))
		})

		t.Run(field.String()+" absent negated", func(t *testing.T) {
			pol, err := NewPolicyFromProto(dropPolicy(
				"not-exists-"+field.String(),
				recordFieldTarget(field),
				&policyv1alpha1.TraceMatcher_Exists{},
				true,
			))
			require.NoError(t, err)
			assert.True(t, matchesTrace(pol, bareSpan, bareScope, bareRes))
			assert.Equal(t, googlepolicy.EvalDrop, evalTrace(pol, bareSpan, bareScope, bareRes))
		})
	}

	t.Run("child span parent id exists", func(t *testing.T) {
		child := ptrace.NewSpan()
		child.SetParentSpanID(pcommon.SpanID(testParentSpanIDBytes))

		pol, err := NewPolicyFromProto(dropPolicy(
			"child-has-parent",
			recordFieldTarget(policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_PARENT_SPAN_ID),
			&policyv1alpha1.TraceMatcher_Exists{},
			false,
		))
		require.NoError(t, err)
		assert.True(t, matchesTrace(pol, child, bareScope, bareRes))
	})
}

// TestRootSpanParentSpanIDEmptyString pins the dual contract that
// SPAN_RECORD_FIELD_PARENT_SPAN_ID carries for root spans, per
// proto/policy/v1alpha1/trace_filter_policy.proto:153-162:
//
//   - lines 156-157: "When evaluated with `exists`, this evaluates to false for
//     root spans (and true with `negate: true`, allowing explicit targeting or
//     exemption of root spans)."
//   - lines 158-159: "When evaluated against string predicates (`exact`,
//     `regex`), a root span matches against the empty string `\"\"`."
//
// Satisfying both at once is why the extractor returns a present-but-empty
// ("", false) for a root span rather than the (nil, false) that TRACE_ID and
// SPAN_ID use: matcher.present() keys off val != nil, so the string predicates
// still run while `exists` stays false.
//
// This restores the coverage that was lost when
// googlepolicyprocessor.TestEvaluator_TraceAllTargetsAndRootSpanParentID was
// deleted; a regression here silently breaks every "drop root spans" policy.
func TestRootSpanParentSpanIDEmptyString(t *testing.T) {
	rootSpan := ptrace.NewSpan()
	rootSpan.SetName("root")
	scope := ptrace.NewScopeSpans()
	res := ptrace.NewResourceSpans()

	childSpan := ptrace.NewSpan()
	childSpan.SetName("child")
	childSpan.SetParentSpanID(pcommon.SpanID(testParentSpanIDBytes))

	parentIDTarget := recordFieldTarget(policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_PARENT_SPAN_ID)

	t.Run("root span equals empty string matches", func(t *testing.T) {
		pol, err := NewPolicyFromProto(dropPolicy("drop-root-equals", parentIDTarget, equalsString(""), false))
		require.NoError(t, err)
		assert.True(t, matchesTrace(pol, rootSpan, scope, res))
		assert.Equal(t, googlepolicy.EvalDrop, evalTrace(pol, rootSpan, scope, res))
	})

	t.Run("root span regex anchored empty matches", func(t *testing.T) {
		pol, err := NewPolicyFromProto(dropPolicy(
			"drop-root-regex",
			parentIDTarget,
			&policyv1alpha1.TraceMatcher_Regex{Regex: "^$"},
			false,
		))
		require.NoError(t, err)
		assert.True(t, matchesTrace(pol, rootSpan, scope, res))
		assert.Equal(t, googlepolicy.EvalDrop, evalTrace(pol, rootSpan, scope, res))
	})

	t.Run("root span exists still reports absent", func(t *testing.T) {
		pol, err := NewPolicyFromProto(dropPolicy(
			"root-exists",
			parentIDTarget,
			&policyv1alpha1.TraceMatcher_Exists{},
			false,
		))
		require.NoError(t, err)
		assert.False(t, matchesTrace(pol, rootSpan, scope, res))
		assert.Equal(t, googlepolicy.EvalNoMatch, evalTrace(pol, rootSpan, scope, res))

		// ... which is what makes `exists` + negate the root-span selector.
		negated, err := NewPolicyFromProto(dropPolicy(
			"root-not-exists",
			parentIDTarget,
			&policyv1alpha1.TraceMatcher_Exists{},
			true,
		))
		require.NoError(t, err)
		assert.True(t, matchesTrace(negated, rootSpan, scope, res))
	})

	t.Run("child span does not match empty string", func(t *testing.T) {
		pol, err := NewPolicyFromProto(dropPolicy("drop-root-equals", parentIDTarget, equalsString(""), false))
		require.NoError(t, err)
		assert.False(t, matchesTrace(pol, childSpan, scope, res))
		assert.Equal(t, googlepolicy.EvalNoMatch, evalTrace(pol, childSpan, scope, res))

		regexPol, err := NewPolicyFromProto(dropPolicy(
			"drop-root-regex",
			parentIDTarget,
			&policyv1alpha1.TraceMatcher_Regex{Regex: "^$"},
			false,
		))
		require.NoError(t, err)
		assert.False(t, matchesTrace(regexPol, childSpan, scope, res))
	})

	// The classic composite policy: DROP root spans, KEEP the ones whose status
	// is OK. Verifies the two policies produce the opposing results the
	// processor's action-precedence layer relies on.
	t.Run("drop root spans with keep exemption", func(t *testing.T) {
		dropRoot, err := NewPolicyFromProto(dropPolicy("drop-root", parentIDTarget, equalsString(""), false))
		require.NoError(t, err)

		keepOkPB := dropPolicy(
			"keep-ok",
			recordFieldTarget(policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_STATUS_CODE),
			equalsString("OK"),
			false,
		)
		keepOkPB.Action = policyv1alpha1.Action_ACTION_KEEP.Enum()
		keepOk, err := NewPolicyFromProto(keepOkPB)
		require.NoError(t, err)

		// Root span, UNSET status: DROP matches, KEEP does not.
		assert.Equal(t, googlepolicy.EvalDrop, evalTrace(dropRoot, rootSpan, scope, res))
		assert.Equal(t, googlepolicy.EvalNoMatch, evalTrace(keepOk, rootSpan, scope, res))

		// Root span, OK status: both match, so the KEEP exemption applies.
		rootSpanOk := ptrace.NewSpan()
		rootSpanOk.SetName("root-ok")
		rootSpanOk.Status().SetCode(ptrace.StatusCodeOk)
		assert.Equal(t, googlepolicy.EvalDrop, evalTrace(dropRoot, rootSpanOk, scope, res))
		assert.Equal(t, googlepolicy.EvalKeep, evalTrace(keepOk, rootSpanOk, scope, res))

		// Child span: neither policy matches.
		assert.Equal(t, googlepolicy.EvalNoMatch, evalTrace(dropRoot, childSpan, scope, res))
		assert.Equal(t, googlepolicy.EvalNoMatch, evalTrace(keepOk, childSpan, scope, res))
	})
}

// TestZeroTraceAndSpanIDDoNotMatchEmptyString is the counterpart to
// TestRootSpanParentSpanIDEmptyString: SPAN_RECORD_FIELD_TRACE_ID and
// SPAN_RECORD_FIELD_SPAN_ID have NO empty-string contract in the proto (see
// trace_filter_policy.proto:141-151, which only describes the hex spelling), so
// their extractors return a genuinely absent (nil, false) and every value
// predicate — including equals "" — must report no match.
func TestZeroTraceAndSpanIDDoNotMatchEmptyString(t *testing.T) {
	bareSpan := ptrace.NewSpan()
	bareSpan.SetName("no-ids")
	scope := ptrace.NewScopeSpans()
	res := ptrace.NewResourceSpans()

	fields := []policyv1alpha1.SpanRecordField{
		policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_TRACE_ID,
		policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_SPAN_ID,
	}

	for _, field := range fields {
		t.Run(field.String()+" equals empty string", func(t *testing.T) {
			pol, err := NewPolicyFromProto(dropPolicy("zero-"+field.String(), recordFieldTarget(field), equalsString(""), false))
			require.NoError(t, err)
			assert.False(t, matchesTrace(pol, bareSpan, scope, res))
			assert.Equal(t, googlepolicy.EvalNoMatch, evalTrace(pol, bareSpan, scope, res))
		})

		t.Run(field.String()+" regex anchored empty", func(t *testing.T) {
			pol, err := NewPolicyFromProto(dropPolicy(
				"zero-regex-"+field.String(),
				recordFieldTarget(field),
				&policyv1alpha1.TraceMatcher_Regex{Regex: "^$"},
				false,
			))
			require.NoError(t, err)
			assert.False(t, matchesTrace(pol, bareSpan, scope, res))
		})

		t.Run(field.String()+" contains empty string", func(t *testing.T) {
			pol, err := NewPolicyFromProto(dropPolicy(
				"zero-contains-"+field.String(),
				recordFieldTarget(field),
				&policyv1alpha1.TraceMatcher_Contains{
					Contains: &policyv1alpha1.Value{Value: &policyv1alpha1.Value_StringValue{StringValue: ""}},
				},
				false,
			))
			require.NoError(t, err)
			assert.False(t, matchesTrace(pol, bareSpan, scope, res))
		})
	}
}

// TestEmptyContextFields covers a zero-valued TraceContext, where every
// extractor must report the target as absent rather than panicking.
func TestEmptyContextFields(t *testing.T) {
	targets := []*policyv1alpha1.TraceFieldSelector{
		recordFieldTarget(policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_NAME),
		recordFieldTarget(policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_TRACE_ID),
		recordFieldTarget(policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_SPAN_ID),
		recordFieldTarget(policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_PARENT_SPAN_ID),
		recordFieldTarget(policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_KIND),
		recordFieldTarget(policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_STATUS_CODE),
		recordFieldTarget(policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_STATUS_MESSAGE),
		spanAttrTarget("http.method"),
		{
			Target: &policyv1alpha1.TraceFieldSelector_ResourceAttribute{
				ResourceAttribute: &policyv1alpha1.AttributePath{Path: []string{"cloud.zone"}},
			},
		},
		{
			Target: &policyv1alpha1.TraceFieldSelector_ScopeAttribute{
				ScopeAttribute: &policyv1alpha1.AttributePath{Path: []string{"scope.tag"}},
			},
		},
		scopeFieldTarget(policyv1alpha1.ScopeField_SCOPE_FIELD_NAME),
		scopeFieldTarget(policyv1alpha1.ScopeField_SCOPE_FIELD_VERSION),
		scopeFieldTarget(policyv1alpha1.ScopeField_SCOPE_FIELD_SCHEMA_URL),
	}

	for i, target := range targets {
		pol, err := NewPolicyFromProto(dropPolicy("empty-ctx", target, &policyv1alpha1.TraceMatcher_Exists{}, false))
		require.NoError(t, err, "target index %d", i)
		assert.Equal(t, googlepolicy.EvalNoMatch, pol.EvaluateTrace(googlepolicy.TraceContext{}), "target index %d", i)
	}
}

func TestSpanKindString(t *testing.T) {
	tests := []struct {
		kind ptrace.SpanKind
		want string
	}{
		{ptrace.SpanKindUnspecified, "INTERNAL"},
		{ptrace.SpanKindInternal, "INTERNAL"},
		{ptrace.SpanKindServer, "SERVER"},
		{ptrace.SpanKindClient, "CLIENT"},
		{ptrace.SpanKindProducer, "PRODUCER"},
		{ptrace.SpanKindConsumer, "CONSUMER"},
	}
	for _, tt := range tests {
		t.Run(tt.want+"/"+tt.kind.String(), func(t *testing.T) {
			assert.Equal(t, tt.want, SpanKindString(tt.kind))
		})
	}
}

func TestSpanStatusString(t *testing.T) {
	tests := []struct {
		code ptrace.StatusCode
		want string
	}{
		{ptrace.StatusCodeUnset, "UNSET"},
		{ptrace.StatusCodeOk, "OK"},
		{ptrace.StatusCodeError, "ERROR"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			assert.Equal(t, tt.want, SpanStatusString(tt.code))
		})
	}
}

// TestKindAndStatusDefaults checks the OTLP defaults surfaced through the
// matcher: an unset span kind reads as INTERNAL and an unset status as UNSET.
func TestKindAndStatusDefaults(t *testing.T) {
	bare := ptrace.NewSpan()
	bareScope := ptrace.NewScopeSpans()
	bareRes := ptrace.NewResourceSpans()

	kindPol, err := NewPolicyFromProto(dropPolicy(
		"default-kind",
		recordFieldTarget(policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_KIND),
		equalsString("INTERNAL"),
		false,
	))
	require.NoError(t, err)
	assert.True(t, matchesTrace(kindPol, bare, bareScope, bareRes))

	statusPol, err := NewPolicyFromProto(dropPolicy(
		"default-status",
		recordFieldTarget(policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_STATUS_CODE),
		equalsString("UNSET"),
		false,
	))
	require.NoError(t, err)
	assert.True(t, matchesTrace(statusPol, bare, bareScope, bareRes))

	// KIND and STATUS_CODE deliberately disagree about `exists`, and only KIND
	// is exercised here.
	//
	// The general rule in trace_filter_policy.proto (SpanRecordField enum
	// prose) is that "first-class fields evaluate to true when set to a
	// non-default (non-empty / non-zero) value". An unset status code is the
	// zero value, so SPAN_RECORD_FIELD_STATUS_CODE reports exists=false even
	// though it still renders as "UNSET" for value predicates. That asymmetry
	// is pinned by TestUnsetStatusCodeReportsAbsent.
	//
	// SPAN_RECORD_FIELD_KIND is the documented exception: the proto states
	// that "if span kind is unset or SPAN_KIND_UNSPECIFIED, implementations
	// treat it as \"INTERNAL\"", so INTERNAL is a real value rather than a
	// default placeholder and the field has no "absent" state. `exists` is
	// therefore unconditionally true for KIND; see
	// TestUnsetSpanKindReportsPresent.
	existsKind, err := NewPolicyFromProto(dropPolicy(
		"exists-kind",
		recordFieldTarget(policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_KIND),
		&policyv1alpha1.TraceMatcher_Exists{},
		false,
	))
	require.NoError(t, err)
	assert.True(t, matchesTrace(existsKind, bare, bareScope, bareRes))

	// An empty name / status message reports as absent.
	existsName, err := NewPolicyFromProto(dropPolicy(
		"exists-name",
		recordFieldTarget(policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_NAME),
		&policyv1alpha1.TraceMatcher_Exists{},
		false,
	))
	require.NoError(t, err)
	assert.False(t, matchesTrace(existsName, bare, bareScope, bareRes))

	existsMsg, err := NewPolicyFromProto(dropPolicy(
		"exists-status-message",
		recordFieldTarget(policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_STATUS_MESSAGE),
		&policyv1alpha1.TraceMatcher_Exists{},
		false,
	))
	require.NoError(t, err)
	assert.False(t, matchesTrace(existsMsg, bare, bareScope, bareRes))
}

// TestEmptyScopeFieldsReportAbsent covers a scope with no name, version, or
// schema URL set.
func TestEmptyScopeFieldsReportAbsent(t *testing.T) {
	span := ptrace.NewSpan()
	scope := ptrace.NewScopeSpans()
	res := ptrace.NewResourceSpans()

	fields := []policyv1alpha1.ScopeField{
		policyv1alpha1.ScopeField_SCOPE_FIELD_NAME,
		policyv1alpha1.ScopeField_SCOPE_FIELD_VERSION,
		policyv1alpha1.ScopeField_SCOPE_FIELD_SCHEMA_URL,
	}
	for _, field := range fields {
		t.Run(field.String(), func(t *testing.T) {
			pol, err := NewPolicyFromProto(dropPolicy(
				"scope-"+field.String(),
				scopeFieldTarget(field),
				&policyv1alpha1.TraceMatcher_Exists{},
				false,
			))
			require.NoError(t, err)
			assert.False(t, matchesTrace(pol, span, scope, res))
		})
	}
}

func TestNestedAttributePaths(t *testing.T) {
	span, scope, res := newTestSpanBundle()

	tests := []struct {
		name string
		path []string
		want bool
	}{
		{name: "map descent", path: []string{"metadata", "env"}, want: true},
		{name: "slice index into map", path: []string{"items", "0", "id"}, want: true},
		{name: "slice index scalar element", path: []string{"user.roles", "1"}, want: true},
		{name: "missing top-level key", path: []string{"nope"}, want: false},
		{name: "missing nested key", path: []string{"metadata", "nope"}, want: false},
		{name: "slice index out of range", path: []string{"items", "5", "id"}, want: false},
		{name: "non-numeric index into slice", path: []string{"items", "id"}, want: false},
		{name: "negative index into slice", path: []string{"items", "-1"}, want: false},
		{name: "descend into scalar", path: []string{"http.method", "sub"}, want: false},
		{name: "descend into bytes", path: []string{"binary.id", "0"}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pol, err := NewPolicyFromProto(dropPolicy(
				"path-"+tt.name,
				spanAttrTarget(tt.path...),
				&policyv1alpha1.TraceMatcher_Exists{},
				false,
			))
			require.NoError(t, err)
			assert.Equal(t, tt.want, matchesTrace(pol, span, scope, res))
		})
	}

	t.Run("nested leaf values", func(t *testing.T) {
		pb := &policyv1alpha1.TraceFilterPolicy{
			Id:     "nested-values",
			Action: policyv1alpha1.Action_ACTION_DROP.Enum(),
			Matches: []*policyv1alpha1.TraceMatcher{
				{
					Target:    spanAttrTarget("metadata", "env"),
					Predicate: equalsString("prod"),
				},
				{
					Target:    spanAttrTarget("items", "0", "id"),
					Predicate: equalsString("item-001"),
				},
				{
					Target:    spanAttrTarget("user.roles", "1"),
					Predicate: equalsString("admin"),
				},
			},
		}
		pol, err := NewPolicyFromProto(pb)
		require.NoError(t, err)
		assert.True(t, matchesTrace(pol, span, scope, res))
	})
}

func TestDriverLoadPolicy(t *testing.T) {
	raw := map[string]any{
		"type":   "trace_filter",
		"id":     "loaded-via-driver",
		"action": "ACTION_DROP",
		"matches": []any{
			map[string]any{
				"target": map[string]any{
					"record_field": "SPAN_RECORD_FIELD_NAME",
				},
				"equals": map[string]any{
					"string_value": "drop this span",
				},
			},
		},
	}

	p, err := googlepolicy.LoadPolicy("trace_filter", raw)
	require.NoError(t, err)
	assert.Equal(t, "loaded-via-driver", p.PolicyName())
	assert.Equal(t, PolicyType, p.PolicyType())
	assert.Equal(t, googlepolicy.PolicyClassTransformation, p.PolicyClass())

	tpe, ok := p.(googlepolicy.TracePolicyEvaluator)
	require.True(t, ok)

	span, scope, res := newTestSpanBundle()
	span.SetName("drop this span")
	ctx := traceContext(span, scope, res)
	assert.Equal(t, googlepolicy.EvalDrop, tpe.EvaluateTrace(ctx))

	span.SetName("keep this span")
	assert.Equal(t, googlepolicy.EvalNoMatch, tpe.EvaluateTrace(ctx))
}

func TestDriverMetadata(t *testing.T) {
	d := &Driver{}
	assert.Equal(t, PolicyType, d.PolicyName())
}

func TestUnsetStatusCodeReportsAbsent(t *testing.T) {
	bare := ptrace.NewSpan()
	bareScope := ptrace.NewScopeSpans()
	bareRes := ptrace.NewResourceSpans()

	// (a) exists predicate reports false for StatusCodeUnset.
	existsPol, err := NewPolicyFromProto(dropPolicy(
		"exists-status",
		recordFieldTarget(policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_STATUS_CODE),
		&policyv1alpha1.TraceMatcher_Exists{},
		false,
	))
	require.NoError(t, err)
	assert.False(t, matchesTrace(existsPol, bare, bareScope, bareRes))

	// (b) equals: {string_value: "UNSET"} still matches (present-but-default).
	equalsUnsetPol, err := NewPolicyFromProto(dropPolicy(
		"equals-unset",
		recordFieldTarget(policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_STATUS_CODE),
		equalsString("UNSET"),
		false,
	))
	require.NoError(t, err)
	assert.True(t, matchesTrace(equalsUnsetPol, bare, bareScope, bareRes))

	// (c) exists + negate: true is the selector for "span has no status".
	notExistsPol, err := NewPolicyFromProto(dropPolicy(
		"not-exists-status",
		recordFieldTarget(policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_STATUS_CODE),
		&policyv1alpha1.TraceMatcher_Exists{},
		true,
	))
	require.NoError(t, err)
	assert.True(t, matchesTrace(notExistsPol, bare, bareScope, bareRes))

	// Contrasting positive case: StatusCodeOk reports exists=true.
	okSpan := ptrace.NewSpan()
	okSpan.Status().SetCode(ptrace.StatusCodeOk)
	assert.True(t, matchesTrace(existsPol, okSpan, bareScope, bareRes))
	assert.False(t, matchesTrace(notExistsPol, okSpan, bareScope, bareRes))
}

func TestUnsetSpanKindReportsPresent(t *testing.T) {
	bare := ptrace.NewSpan() // SpanKindUnspecified
	bareScope := ptrace.NewScopeSpans()
	bareRes := ptrace.NewResourceSpans()

	// Unset SpanKind is treated as "INTERNAL" and reports exists=true.
	notExistsKindPol, err := NewPolicyFromProto(dropPolicy(
		"not-exists-kind",
		recordFieldTarget(policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_KIND),
		&policyv1alpha1.TraceMatcher_Exists{},
		true,
	))
	require.NoError(t, err)
	assert.False(t, matchesTrace(notExistsKindPol, bare, bareScope, bareRes))
}

func TestDriverLoadPolicyErrors(t *testing.T) {
	tests := []struct {
		name string
		raw  map[string]any
	}{
		{
			name: "empty id",
			raw: map[string]any{
				"type":   "trace_filter",
				"id":     "",
				"action": "ACTION_DROP",
				"matches": []any{
					map[string]any{
						"target": map[string]any{"record_field": "SPAN_RECORD_FIELD_NAME"},
						"equals": map[string]any{"string_value": "x"},
					},
				},
			},
		},
		{
			name: "missing action due to typo'd top-level field",
			raw: map[string]any{
				"type":    "trace_filter",
				"id":      "typo-policy",
				"actions": "ACTION_DROP", // unknown field discarded -> action left unspecified -> validation error
			},
		},
		{
			name: "missing target due to typo'd nested field",
			raw: map[string]any{
				"type":   "trace_filter",
				"id":     "typo-nested",
				"action": "ACTION_DROP",
				"matches": []any{
					map[string]any{
						"target": map[string]any{
							"recordfield": "SPAN_RECORD_FIELD_NAME", // unknown field discarded -> target unset -> validation error
						},
						"equals": map[string]any{"string_value": "x"},
					},
				},
			},
		},
		{
			name: "invalid enum value",
			raw: map[string]any{
				"type":   "trace_filter",
				"id":     "bad-enum",
				"action": "ACTION_NOPE",
			},
		},
		{
			name: "valid proto but no matchers",
			raw: map[string]any{
				"type":   "trace_filter",
				"id":     "no-matchers",
				"action": "ACTION_DROP",
			},
		},
		{
			name: "unmarshalable config value",
			raw: map[string]any{
				"type":   "trace_filter",
				"id":     "bad-json",
				"action": make(chan int),
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

// TestDriverLoadPolicyIgnoresUnknownFields pins forward-compatibility behavior
// (DiscardUnknown: true): when the server adds new top-level or nested proto
// fields in future versions, older agents ignore the unknown fields and still
// compile and evaluate the policy cleanly.
func TestDriverLoadPolicyIgnoresUnknownFields(t *testing.T) {
	d := &Driver{}
	raw := map[string]any{
		"type":             "trace_filter",
		"id":               "future-compatible-trace-policy",
		"action":           "ACTION_DROP",
		"future_top_field": "ignored_server_metadata",
		"future_priority":  42,
		"matches": []any{
			map[string]any{
				"target":              map[string]any{"record_field": "SPAN_RECORD_FIELD_NAME"},
				"exists":              map[string]any{},
				"future_matcher_hint": true,
			},
		},
	}
	p, err := d.LoadPolicy(raw)
	require.NoError(t, err)
	assert.Equal(t, "future-compatible-trace-policy", p.PolicyName())

	tracePol, ok := p.(googlepolicy.TracePolicyEvaluator)
	require.True(t, ok)

	span, scope, res := newTestSpanBundle()
	assert.Equal(t, googlepolicy.EvalDrop, tracePol.EvaluateTrace(traceContext(span, scope, res)))
}

func TestUnsetSpanStringFieldsReportAbsent(t *testing.T) {
	span := ptrace.NewSpan()
	scope := ptrace.NewScopeSpans()
	res := ptrace.NewResourceSpans()
	ctx := traceContext(span, scope, res)

	targets := []*policyv1alpha1.TraceFieldSelector{
		recordFieldTarget(policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_NAME),
		recordFieldTarget(policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_STATUS_MESSAGE),
		scopeFieldTarget(policyv1alpha1.ScopeField_SCOPE_FIELD_NAME),
		scopeFieldTarget(policyv1alpha1.ScopeField_SCOPE_FIELD_VERSION),
		scopeFieldTarget(policyv1alpha1.ScopeField_SCOPE_FIELD_SCHEMA_URL),
	}

	for _, target := range targets {
		extract, err := compileExtractor(target)
		require.NoError(t, err)
		val, exists := extract(ctx)
		assert.False(t, exists, "target %v must report exists=false when unset", target)
		assert.Nil(t, val, "target %v must report nil value when unset", target)

		// Negative regex must not match unset string fields.
		negRegexPol, err := NewPolicyFromProto(dropPolicy("neg-regex", target, &policyv1alpha1.TraceMatcher_Regex{Regex: "^[^E]*$"}, false))
		require.NoError(t, err)
		assert.Equal(t, googlepolicy.EvalNoMatch, negRegexPol.EvaluateTrace(ctx))

		// Equals empty string must not match unset string fields.
		equalsEmptyPol, err := NewPolicyFromProto(dropPolicy("equals-empty", target, equalsString(""), false))
		require.NoError(t, err)
		assert.Equal(t, googlepolicy.EvalNoMatch, equalsEmptyPol.EvaluateTrace(ctx))
	}

	// By contrast, root span ParentSpanID intentionally returns ("", false) per spec so equals: "" matches.
	parentExtract, err := compileExtractor(recordFieldTarget(policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_PARENT_SPAN_ID))
	require.NoError(t, err)
	val, exists := parentExtract(ctx)
	assert.False(t, exists)
	assert.Equal(t, "", val)
}
