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

package policyv1alpha1_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/emptypb"

	policyv1alpha1 "github.com/GoogleCloudPlatform/opentelemetry-operations-collector/gen/go/policy/v1alpha1"
	xdsv1alpha1 "github.com/GoogleCloudPlatform/opentelemetry-operations-collector/gen/go/xds/v1alpha1"
)


func TestTraceFilterPolicy_Serialization(t *testing.T) {
	policy := &policyv1alpha1.TraceFilterPolicy{
		Id: "drop-healthcheck-spans",
		Matchers: []*policyv1alpha1.TraceMatcher{
			{
				Target: &policyv1alpha1.TraceFieldSelector{
					Target: &policyv1alpha1.TraceFieldSelector_RecordField{
						RecordField: policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_NAME,
					},
				},
				Predicate: &policyv1alpha1.TraceMatcher_Glob{
					Glob: "/healthz*",
				},
			},
			{
				Target: &policyv1alpha1.TraceFieldSelector{
					Target: &policyv1alpha1.TraceFieldSelector_SpanAttributePath{
						SpanAttributePath: &policyv1alpha1.AttributePath{
							Segments: []string{"http.route"},
						},
					},
				},
				Predicate: &policyv1alpha1.TraceMatcher_Equality{
					Equality: &policyv1alpha1.Value{
						Kind: &policyv1alpha1.Value_StringValue{
							StringValue: "/ready",
						},
					},
				},
			},
			{
				Target: &policyv1alpha1.TraceFieldSelector{
					Target: &policyv1alpha1.TraceFieldSelector_SpanAttributePath{
						SpanAttributePath: &policyv1alpha1.AttributePath{
							Segments: []string{"http.status_code"},
						},
					},
				},
				Predicate: &policyv1alpha1.TraceMatcher_Equality{
					Equality: &policyv1alpha1.Value{
						Kind: &policyv1alpha1.Value_IntValue{
							IntValue: 200,
						},
					},
				},
			},
			{
				Target: &policyv1alpha1.TraceFieldSelector{
					Target: &policyv1alpha1.TraceFieldSelector_SpanAttributePath{
						SpanAttributePath: &policyv1alpha1.AttributePath{
							Segments: []string{"http.status_code"},
						},
					},
				},
				Predicate: &policyv1alpha1.TraceMatcher_InclusiveMinimum{
					InclusiveMinimum: &policyv1alpha1.NumericValue{
						Kind: &policyv1alpha1.NumericValue_IntValue{
							IntValue: 400,
						},
					},
				},
			},
			{
				Target: &policyv1alpha1.TraceFieldSelector{
					Target: &policyv1alpha1.TraceFieldSelector_SpanAttributePath{
						SpanAttributePath: &policyv1alpha1.AttributePath{
							Segments: []string{"deployment", "tags"},
						},
					},
				},
				Predicate: &policyv1alpha1.TraceMatcher_Containment{
					Containment: &policyv1alpha1.Value{
						Kind: &policyv1alpha1.Value_StringValue{
							StringValue: "canary",
						},
					},
				},
			},
			{
				Target: &policyv1alpha1.TraceFieldSelector{
					Target: &policyv1alpha1.TraceFieldSelector_RecordField{
						RecordField: policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_KIND,
					},
				},
				Predicate: &policyv1alpha1.TraceMatcher_Equality{
					Equality: &policyv1alpha1.Value{
						Kind: &policyv1alpha1.Value_StringValue{
							StringValue: "SERVER",
						},
					},
				},
			},
			{
				Target: &policyv1alpha1.TraceFieldSelector{
					Target: &policyv1alpha1.TraceFieldSelector_ResourceAttributePath{
						ResourceAttributePath: &policyv1alpha1.AttributePath{
							Segments: []string{"service.name"},
						},
					},
				},
				Predicate: &policyv1alpha1.TraceMatcher_Equality{
					Equality: &policyv1alpha1.Value{
						Kind: &policyv1alpha1.Value_StringValue{
							StringValue: "frontend",
						},
					},
				},
			},
			{
				Target: &policyv1alpha1.TraceFieldSelector{
					Target: &policyv1alpha1.TraceFieldSelector_ScopeAttributePath{
						ScopeAttributePath: &policyv1alpha1.AttributePath{
							Segments: []string{"library.name"},
						},
					},
				},
				Predicate: &policyv1alpha1.TraceMatcher_Existence{
					Existence: &emptypb.Empty{},
				},
			},
			{
				Target: &policyv1alpha1.TraceFieldSelector{
					Target: &policyv1alpha1.TraceFieldSelector_ScopeField{
						ScopeField: policyv1alpha1.ScopeField_SCOPE_FIELD_NAME,
					},
				},
				Predicate: &policyv1alpha1.TraceMatcher_Equality{
					Equality: &policyv1alpha1.Value{
						Kind: &policyv1alpha1.Value_StringValue{
							StringValue: "go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp",
						},
					},
				},
			},
			{
				Target: &policyv1alpha1.TraceFieldSelector{
					Target: &policyv1alpha1.TraceFieldSelector_ScopeField{
						ScopeField: policyv1alpha1.ScopeField_SCOPE_FIELD_VERSION,
					},
				},
				Predicate: &policyv1alpha1.TraceMatcher_Equality{
					Equality: &policyv1alpha1.Value{
						Kind: &policyv1alpha1.Value_StringValue{
							StringValue: "v0.45.0",
						},
					},
				},
			},
			{
				Target: &policyv1alpha1.TraceFieldSelector{
					Target: &policyv1alpha1.TraceFieldSelector_ScopeField{
						ScopeField: policyv1alpha1.ScopeField_SCOPE_FIELD_SCHEMA_URL,
					},
				},
				Predicate: &policyv1alpha1.TraceMatcher_Equality{
					Equality: &policyv1alpha1.Value{
						Kind: &policyv1alpha1.Value_StringValue{
							StringValue: "https://opentelemetry.io/schemas/1.24.0",
						},
					},
				},
			},
		},
	}

	data, err := proto.Marshal(policy)
	require.NoError(t, err)

	unmarshaled := &policyv1alpha1.TraceFilterPolicy{}
	require.NoError(t, proto.Unmarshal(data, unmarshaled))

	assert.True(t, proto.Equal(policy, unmarshaled))
	assert.Equal(t, "drop-healthcheck-spans", unmarshaled.GetId())
	require.Len(t, unmarshaled.GetMatchers(), 11)
}

func TestTraceFilterPolicy_AnyPackaging(t *testing.T) {
	policy := &policyv1alpha1.TraceFilterPolicy{
		Id: "drop-healthcheck-spans",
	}

	anyPolicy, err := anypb.New(policy)
	require.NoError(t, err)
	assert.Equal(t, "type.googleapis.com/google.telemetry.policy.v1alpha1.TraceFilterPolicy", anyPolicy.GetTypeUrl())

	collector := &xdsv1alpha1.TelemetryCollector{
		Policies: []*anypb.Any{anyPolicy},
	}

	data, err := proto.Marshal(collector)
	require.NoError(t, err)

	unmarshaledCollector := &xdsv1alpha1.TelemetryCollector{}
	require.NoError(t, proto.Unmarshal(data, unmarshaledCollector))

	require.Len(t, unmarshaledCollector.GetPolicies(), 1)
	assert.Equal(t, "type.googleapis.com/google.telemetry.policy.v1alpha1.TraceFilterPolicy", unmarshaledCollector.GetPolicies()[0].GetTypeUrl())

	unpackedPolicy := &policyv1alpha1.TraceFilterPolicy{}
	require.NoError(t, unmarshaledCollector.GetPolicies()[0].UnmarshalTo(unpackedPolicy))
	assert.Equal(t, "drop-healthcheck-spans", unpackedPolicy.GetId())
}

func TestTraceFilterPolicy_EnumValues(t *testing.T) {
	assert.Equal(t, policyv1alpha1.Action(0), policyv1alpha1.Action_ACTION_UNSPECIFIED)
	assert.Equal(t, policyv1alpha1.Action(1), policyv1alpha1.Action_ACTION_KEEP)
	assert.Equal(t, policyv1alpha1.Action(2), policyv1alpha1.Action_ACTION_DROP)

	assert.Equal(t, policyv1alpha1.SpanRecordField(0), policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_UNSPECIFIED)
	assert.Equal(t, policyv1alpha1.SpanRecordField(1), policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_NAME)
	assert.Equal(t, policyv1alpha1.SpanRecordField(2), policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_TRACE_ID)
	assert.Equal(t, policyv1alpha1.SpanRecordField(3), policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_SPAN_ID)
	assert.Equal(t, policyv1alpha1.SpanRecordField(4), policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_PARENT_SPAN_ID)
	assert.Equal(t, policyv1alpha1.SpanRecordField(5), policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_KIND)

	assert.Equal(t, policyv1alpha1.ScopeField(0), policyv1alpha1.ScopeField_SCOPE_FIELD_UNSPECIFIED)
	assert.Equal(t, policyv1alpha1.ScopeField(1), policyv1alpha1.ScopeField_SCOPE_FIELD_NAME)
	assert.Equal(t, policyv1alpha1.ScopeField(2), policyv1alpha1.ScopeField_SCOPE_FIELD_VERSION)
	assert.Equal(t, policyv1alpha1.ScopeField(3), policyv1alpha1.ScopeField_SCOPE_FIELD_SCHEMA_URL)
}

func TestTraceFilterPolicy_JSONSerialization(t *testing.T) {
	policy := &policyv1alpha1.TraceFilterPolicy{
		Id: "drop-healthcheck-spans",
		Matchers: []*policyv1alpha1.TraceMatcher{
			{
				Target: &policyv1alpha1.TraceFieldSelector{
					Target: &policyv1alpha1.TraceFieldSelector_RecordField{
						RecordField: policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_NAME,
					},
				},
				Predicate: &policyv1alpha1.TraceMatcher_Equality{
					Equality: &policyv1alpha1.Value{
						Kind: &policyv1alpha1.Value_StringValue{
							StringValue: "/healthz",
						},
					},
				},
				Negated: false,
			},
			{
				Target: &policyv1alpha1.TraceFieldSelector{
					Target: &policyv1alpha1.TraceFieldSelector_RecordField{
						RecordField: policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_KIND,
					},
				},
				Predicate: &policyv1alpha1.TraceMatcher_Equality{
					Equality: &policyv1alpha1.Value{
						Kind: &policyv1alpha1.Value_StringValue{
							StringValue: "SERVER",
						},
					},
				},
			},
		},
	}

	// ProtoJSON marshal and unmarshal round-trip.
	jsonData, err := protojson.Marshal(policy)
	require.NoError(t, err)

	unmarshaled := &policyv1alpha1.TraceFilterPolicy{}
	require.NoError(t, protojson.Unmarshal(jsonData, unmarshaled))
	assert.True(t, proto.Equal(policy, unmarshaled))
}

func TestTraceFilterPolicy_ParentSpanIDAndKindMatchers(t *testing.T) {
	// Policy targeting root spans (absent parent_span_id: existence with negated: true or equality: "")
	rootSpanPolicy := &policyv1alpha1.TraceFilterPolicy{
		Id: "target-root-spans",
		Matchers: []*policyv1alpha1.TraceMatcher{
			{
				Target: &policyv1alpha1.TraceFieldSelector{
					Target: &policyv1alpha1.TraceFieldSelector_RecordField{
						RecordField: policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_PARENT_SPAN_ID,
					},
				},
				Predicate: &policyv1alpha1.TraceMatcher_Existence{
					Existence: &emptypb.Empty{},
				},
				Negated: true,
			},
			{
				Target: &policyv1alpha1.TraceFieldSelector{
					Target: &policyv1alpha1.TraceFieldSelector_RecordField{
						RecordField: policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_KIND,
					},
				},
				Predicate: &policyv1alpha1.TraceMatcher_Equality{
					Equality: &policyv1alpha1.Value{
						Kind: &policyv1alpha1.Value_StringValue{
							StringValue: "INTERNAL",
						},
					},
				},
			},
		},
	}

	data, err := proto.Marshal(rootSpanPolicy)
	require.NoError(t, err)

	unmarshaled := &policyv1alpha1.TraceFilterPolicy{}
	require.NoError(t, proto.Unmarshal(data, unmarshaled))
	assert.True(t, proto.Equal(rootSpanPolicy, unmarshaled))

	// Policy targeting child spans with explicit 16-char hex parent_span_id.
	childSpanPolicy := &policyv1alpha1.TraceFilterPolicy{
		Id: "target-child-spans",
		Matchers: []*policyv1alpha1.TraceMatcher{
			{
				Target: &policyv1alpha1.TraceFieldSelector{
					Target: &policyv1alpha1.TraceFieldSelector_RecordField{
						RecordField: policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_PARENT_SPAN_ID,
					},
				},
				Predicate: &policyv1alpha1.TraceMatcher_Equality{
					Equality: &policyv1alpha1.Value{
						Kind: &policyv1alpha1.Value_StringValue{
							StringValue: "0123456789abcdef",
						},
					},
				},
			},
		},
	}

	data, err = proto.Marshal(childSpanPolicy)
	require.NoError(t, err)
	unmarshaledChild := &policyv1alpha1.TraceFilterPolicy{}
	require.NoError(t, proto.Unmarshal(data, unmarshaledChild))
	assert.True(t, proto.Equal(childSpanPolicy, unmarshaledChild))
}
