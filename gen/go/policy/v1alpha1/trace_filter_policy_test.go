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

	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/emptypb"

	policyv1alpha1 "github.com/GoogleCloudPlatform/opentelemetry-operations-collector/gen/go/policy/v1alpha1"
	xdsv1alpha1 "github.com/GoogleCloudPlatform/opentelemetry-operations-collector/gen/go/xds/v1alpha1"
)


func actionPtr(a policyv1alpha1.Action) *policyv1alpha1.Action {
	return &a
}

func TestTraceFilterPolicy_Serialization(t *testing.T) {
	policy := &policyv1alpha1.TraceFilterPolicy{
		Id:     "drop-healthcheck-spans",
		Action: actionPtr(policyv1alpha1.Action_ACTION_DROP),
		Matches: []*policyv1alpha1.TraceMatcher{
			{
				Target: &policyv1alpha1.TraceFieldSelector{
					Target: &policyv1alpha1.TraceFieldSelector_RecordField{
						RecordField: policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_NAME,
					},
				},
				Predicate: &policyv1alpha1.TraceMatcher_Regex{
					Regex: "^/healthz.*",
				},
			},
			{
				Target: &policyv1alpha1.TraceFieldSelector{
					Target: &policyv1alpha1.TraceFieldSelector_SpanAttribute{
						SpanAttribute: "http.route",
					},
				},
				Predicate: &policyv1alpha1.TraceMatcher_Exact{
					Exact: "/ready",
				},
			},
			{
				Target: &policyv1alpha1.TraceFieldSelector{
					Target: &policyv1alpha1.TraceFieldSelector_RecordField{
						RecordField: policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_KIND,
					},
				},
				Predicate: &policyv1alpha1.TraceMatcher_Exact{
					Exact: "SERVER",
				},
			},
			{
				Target: &policyv1alpha1.TraceFieldSelector{
					Target: &policyv1alpha1.TraceFieldSelector_RecordField{
						RecordField: policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_STATUS_CODE,
					},
				},
				Predicate: &policyv1alpha1.TraceMatcher_Exact{
					Exact: "ERROR",
				},
				Negate: true,
			},
			{
				Target: &policyv1alpha1.TraceFieldSelector{
					Target: &policyv1alpha1.TraceFieldSelector_ResourceAttribute{
						ResourceAttribute: "service.name",
					},
				},
				Predicate: &policyv1alpha1.TraceMatcher_Exact{
					Exact: "frontend",
				},
			},
			{
				Target: &policyv1alpha1.TraceFieldSelector{
					Target: &policyv1alpha1.TraceFieldSelector_ScopeAttribute{
						ScopeAttribute: "library.name",
					},
				},
				Predicate: &policyv1alpha1.TraceMatcher_Exists{
					Exists: &emptypb.Empty{},
				},
			},
			{
				Target: &policyv1alpha1.TraceFieldSelector{
					Target: &policyv1alpha1.TraceFieldSelector_ScopeField{
						ScopeField: policyv1alpha1.ScopeField_SCOPE_FIELD_NAME,
					},
				},
				Predicate: &policyv1alpha1.TraceMatcher_Exact{
					Exact: "go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp",
				},
			},
			{
				Target: &policyv1alpha1.TraceFieldSelector{
					Target: &policyv1alpha1.TraceFieldSelector_ScopeField{
						ScopeField: policyv1alpha1.ScopeField_SCOPE_FIELD_VERSION,
					},
				},
				Predicate: &policyv1alpha1.TraceMatcher_Exact{
					Exact: "v0.45.0",
				},
			},

			{
				Target: &policyv1alpha1.TraceFieldSelector{
					Target: &policyv1alpha1.TraceFieldSelector_ScopeField{
						ScopeField: policyv1alpha1.ScopeField_SCOPE_FIELD_SCHEMA_URL,
					},
				},
				Predicate: &policyv1alpha1.TraceMatcher_Exact{
					Exact: "https://opentelemetry.io/schemas/1.24.0",
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
	assert.Equal(t, policyv1alpha1.Action_ACTION_DROP, unmarshaled.GetAction())
	require.Len(t, unmarshaled.GetMatches(), 9)
}

func TestTraceFilterPolicy_AnyPackaging(t *testing.T) {
	policy := &policyv1alpha1.TraceFilterPolicy{
		Id:     "retain-error-spans",
		Action: actionPtr(policyv1alpha1.Action_ACTION_KEEP),
	}

	anyPolicy, err := anypb.New(policy)
	require.NoError(t, err)
	assert.Equal(t, "type.googleapis.com/google.telemetry.policy.v1alpha1.TraceFilterPolicy", anyPolicy.GetTypeUrl())

	collector := &xdsv1alpha1.TelemetryCollector{
		Policies: []*corev3.TypedExtensionConfig{
			{
				Name:        "default-trace-filter",
				TypedConfig: anyPolicy,
			},
		},
	}

	data, err := proto.Marshal(collector)
	require.NoError(t, err)

	unmarshaledCollector := &xdsv1alpha1.TelemetryCollector{}
	require.NoError(t, proto.Unmarshal(data, unmarshaledCollector))

	require.Len(t, unmarshaledCollector.GetPolicies(), 1)
	assert.Equal(t, "default-trace-filter", unmarshaledCollector.GetPolicies()[0].GetName())

	unpackedPolicy := &policyv1alpha1.TraceFilterPolicy{}
	require.NoError(t, unmarshaledCollector.GetPolicies()[0].GetTypedConfig().UnmarshalTo(unpackedPolicy))
	assert.Equal(t, "retain-error-spans", unpackedPolicy.GetId())
	assert.Equal(t, policyv1alpha1.Action_ACTION_KEEP, unpackedPolicy.GetAction())
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
	assert.Equal(t, policyv1alpha1.SpanRecordField(5), policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_STATUS_MESSAGE)
	assert.Equal(t, policyv1alpha1.SpanRecordField(6), policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_KIND)
	assert.Equal(t, policyv1alpha1.SpanRecordField(7), policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_STATUS_CODE)



	assert.Equal(t, policyv1alpha1.ScopeField(0), policyv1alpha1.ScopeField_SCOPE_FIELD_UNSPECIFIED)
	assert.Equal(t, policyv1alpha1.ScopeField(1), policyv1alpha1.ScopeField_SCOPE_FIELD_NAME)
	assert.Equal(t, policyv1alpha1.ScopeField(2), policyv1alpha1.ScopeField_SCOPE_FIELD_VERSION)
	assert.Equal(t, policyv1alpha1.ScopeField(3), policyv1alpha1.ScopeField_SCOPE_FIELD_SCHEMA_URL)


}

func TestTraceFilterPolicy_JSONSerialization(t *testing.T) {
	policy := &policyv1alpha1.TraceFilterPolicy{
		Id:     "drop-healthcheck-spans",
		Action: actionPtr(policyv1alpha1.Action_ACTION_DROP),
		Matches: []*policyv1alpha1.TraceMatcher{
			{
				Target: &policyv1alpha1.TraceFieldSelector{
					Target: &policyv1alpha1.TraceFieldSelector_RecordField{
						RecordField: policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_NAME,
					},
				},
				Predicate: &policyv1alpha1.TraceMatcher_Exact{
					Exact: "/healthz",
				},
				Negate: false,
			},
			{
				Target: &policyv1alpha1.TraceFieldSelector{
					Target: &policyv1alpha1.TraceFieldSelector_RecordField{
						RecordField: policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_KIND,
					},
				},
				Predicate: &policyv1alpha1.TraceMatcher_Exact{
					Exact: "SERVER",
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

	// Zero-value handling: ACTION_UNSPECIFIED explicit.
	jsonWithUnspecified := []byte(`{"id": "unspecified-policy", "action": "ACTION_UNSPECIFIED"}`)
	zeroPolicy := &policyv1alpha1.TraceFilterPolicy{}
	require.NoError(t, protojson.Unmarshal(jsonWithUnspecified, zeroPolicy))
	assert.Equal(t, "unspecified-policy", zeroPolicy.GetId())
	assert.Equal(t, policyv1alpha1.Action_ACTION_UNSPECIFIED, zeroPolicy.GetAction())

	// Action omitted in JSON defaults to ACTION_UNSPECIFIED.
	jsonOmittedAction := []byte(`{"id": "omitted-action-policy"}`)
	omittedPolicy := &policyv1alpha1.TraceFilterPolicy{}
	require.NoError(t, protojson.Unmarshal(jsonOmittedAction, omittedPolicy))
	assert.Equal(t, "omitted-action-policy", omittedPolicy.GetId())
	assert.Equal(t, policyv1alpha1.Action_ACTION_UNSPECIFIED, omittedPolicy.GetAction())

	// EmitUnpopulated includes ACTION_UNSPECIFIED in output.
	marshaledUnpopulated, err := protojson.MarshalOptions{EmitUnpopulated: true}.Marshal(zeroPolicy)
	require.NoError(t, err)
	assert.Contains(t, string(marshaledUnpopulated), `"action":"ACTION_UNSPECIFIED"`)
}
