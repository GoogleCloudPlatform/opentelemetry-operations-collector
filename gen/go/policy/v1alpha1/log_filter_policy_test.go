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
)

func TestLogFilterPolicy_Serialization(t *testing.T) {
	policy := &policyv1alpha1.LogFilterPolicy{
		Id:     "drop-noisy-healthchecks",
		Action: policyv1alpha1.Action_ACTION_DROP.Enum(),
		Matchers: []*policyv1alpha1.LogMatcher{

			{
				Target: &policyv1alpha1.LogFieldSelector{
					Target: &policyv1alpha1.LogFieldSelector_RecordField{
						RecordField: policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_BODY,
					},
				},
				Predicate: &policyv1alpha1.LogMatcher_Glob{
					Glob: "healthcheck*",
				},
				Negated: false,
			},
			{
				Target: &policyv1alpha1.LogFieldSelector{
					Target: &policyv1alpha1.LogFieldSelector_LogAttributePath{
						LogAttributePath: &policyv1alpha1.AttributePath{
							Segments: []string{"http.route"},
						},
					},
				},
				Predicate: &policyv1alpha1.LogMatcher_Equality{
					Equality: &policyv1alpha1.Value{
						Kind: &policyv1alpha1.Value_StringValue{
							StringValue: "/healthz",
						},
					},
				},
			},
			{
				Target: &policyv1alpha1.LogFieldSelector{
					Target: &policyv1alpha1.LogFieldSelector_LogAttributePath{
						LogAttributePath: &policyv1alpha1.AttributePath{
							Segments: []string{"http.status_code"},
						},
					},
				},
				Predicate: &policyv1alpha1.LogMatcher_Equality{
					Equality: &policyv1alpha1.Value{
						Kind: &policyv1alpha1.Value_IntValue{
							IntValue: 200,
						},
					},
				},
			},
			{
				Target: &policyv1alpha1.LogFieldSelector{
					Target: &policyv1alpha1.LogFieldSelector_LogAttributePath{
						LogAttributePath: &policyv1alpha1.AttributePath{
							Segments: []string{"feature_flag.enabled"},
						},
					},
				},
				Predicate: &policyv1alpha1.LogMatcher_Equality{
					Equality: &policyv1alpha1.Value{
						Kind: &policyv1alpha1.Value_BoolValue{
							BoolValue: true,
						},
					},
				},
			},
			{
				Target: &policyv1alpha1.LogFieldSelector{
					Target: &policyv1alpha1.LogFieldSelector_LogAttributePath{
						LogAttributePath: &policyv1alpha1.AttributePath{
							Segments: []string{"sampling.threshold"},
						},
					},
				},
				Predicate: &policyv1alpha1.LogMatcher_Equality{
					Equality: &policyv1alpha1.Value{
						Kind: &policyv1alpha1.Value_DoubleValue{
							DoubleValue: 0.05,
						},
					},
				},
			},
			{
				Target: &policyv1alpha1.LogFieldSelector{
					Target: &policyv1alpha1.LogFieldSelector_LogAttributePath{
						LogAttributePath: &policyv1alpha1.AttributePath{
							Segments: []string{"binary.signature"},
						},
					},
				},
				Predicate: &policyv1alpha1.LogMatcher_Equality{
					Equality: &policyv1alpha1.Value{
						Kind: &policyv1alpha1.Value_BytesValue{
							BytesValue: []byte{0xde, 0xad, 0xbe, 0xef},
						},
					},
				},
			},
			{
				Target: &policyv1alpha1.LogFieldSelector{
					Target: &policyv1alpha1.LogFieldSelector_LogAttributePath{
						LogAttributePath: &policyv1alpha1.AttributePath{
							Segments: []string{"user", "roles"},
						},
					},
				},
				Predicate: &policyv1alpha1.LogMatcher_Containment{
					Containment: &policyv1alpha1.Value{
						Kind: &policyv1alpha1.Value_StringValue{
							StringValue: "admin",
						},
					},
				},
			},
			{
				Target: &policyv1alpha1.LogFieldSelector{
					Target: &policyv1alpha1.LogFieldSelector_RecordField{
						RecordField: policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SEVERITY_NUMBER,
					},
				},
				Predicate: &policyv1alpha1.LogMatcher_InclusiveMinimum{
					InclusiveMinimum: &policyv1alpha1.NumericValue{
						Kind: &policyv1alpha1.NumericValue_IntValue{
							IntValue: 17, // ERROR
						},
					},
				},
			},
			{
				Target: &policyv1alpha1.LogFieldSelector{
					Target: &policyv1alpha1.LogFieldSelector_LogAttributePath{
						LogAttributePath: &policyv1alpha1.AttributePath{
							Segments: []string{"latency_seconds"},
						},
					},
				},
				Predicate: &policyv1alpha1.LogMatcher_ExclusiveMinimum{
					ExclusiveMinimum: &policyv1alpha1.NumericValue{
						Kind: &policyv1alpha1.NumericValue_DoubleValue{
							DoubleValue: 2.5,
						},
					},
				},
			},
			{
				Target: &policyv1alpha1.LogFieldSelector{
					Target: &policyv1alpha1.LogFieldSelector_ResourceAttributePath{
						ResourceAttributePath: &policyv1alpha1.AttributePath{
							Segments: []string{"service.name"},
						},
					},
				},
				Predicate: &policyv1alpha1.LogMatcher_Existence{
					Existence: &emptypb.Empty{},
				},
				Negated: true,
			},
			{
				Target: &policyv1alpha1.LogFieldSelector{
					Target: &policyv1alpha1.LogFieldSelector_ScopeAttributePath{
						ScopeAttributePath: &policyv1alpha1.AttributePath{
							Segments: []string{"library.name"},
						},
					},
				},
				Predicate: &policyv1alpha1.LogMatcher_Equality{
					Equality: &policyv1alpha1.Value{
						Kind: &policyv1alpha1.Value_StringValue{
							StringValue: "my-library",
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
				Predicate: &policyv1alpha1.LogMatcher_Equality{
					Equality: &policyv1alpha1.Value{
						Kind: &policyv1alpha1.Value_StringValue{
							StringValue: "my-scope",
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
				Predicate: &policyv1alpha1.LogMatcher_Equality{
					Equality: &policyv1alpha1.Value{
						Kind: &policyv1alpha1.Value_StringValue{
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
				Predicate: &policyv1alpha1.LogMatcher_Equality{
					Equality: &policyv1alpha1.Value{
						Kind: &policyv1alpha1.Value_StringValue{
							StringValue: "https://opentelemetry.io/schemas/1.24.0",
						},
					},
				},
			},
			{
				Target: &policyv1alpha1.LogFieldSelector{
					Target: &policyv1alpha1.LogFieldSelector_BodyFieldPath{
						BodyFieldPath: &policyv1alpha1.AttributePath{
							Segments: []string{"event_data", "TargetUserName"},
						},
					},
				},
				Predicate: &policyv1alpha1.LogMatcher_Equality{
					Equality: &policyv1alpha1.Value{
						Kind: &policyv1alpha1.Value_StringValue{
							StringValue: "SYSTEM",
						},
					},
				},
			},
		},
	}

	// Verify Any packaging.
	anyPolicy, err := anypb.New(policy)
	require.NoError(t, err)
	assert.Equal(t, "type.googleapis.com/google.telemetry.policy.v1alpha1.LogFilterPolicy", anyPolicy.GetTypeUrl())

	// Round-trip serialization.
	data, err := proto.Marshal(policy)
	require.NoError(t, err)

	unmarshaled := &policyv1alpha1.LogFilterPolicy{}
	require.NoError(t, proto.Unmarshal(data, unmarshaled))

	assert.True(t, proto.Equal(policy, unmarshaled))
	assert.Equal(t, "drop-noisy-healthchecks", unmarshaled.GetId())
	assert.NotNil(t, unmarshaled.Action)
	assert.Equal(t, policyv1alpha1.Action_ACTION_DROP, unmarshaled.GetAction())
	require.Len(t, unmarshaled.GetMatchers(), 15)
	assert.Equal(t, []string{"event_data", "TargetUserName"}, unmarshaled.GetMatchers()[14].GetTarget().GetBodyFieldPath().GetSegments())
}

func TestLogFilterPolicy_JSONSerialization(t *testing.T) {
	policy := &policyv1alpha1.LogFilterPolicy{
		Id:     "drop-noisy-healthchecks",
		Action: policyv1alpha1.Action_ACTION_DROP.Enum(),
		Matchers: []*policyv1alpha1.LogMatcher{
			{
				Target: &policyv1alpha1.LogFieldSelector{
					Target: &policyv1alpha1.LogFieldSelector_LogAttributePath{
						LogAttributePath: &policyv1alpha1.AttributePath{
							Segments: []string{"http.route"},
						},
					},
				},
				Predicate: &policyv1alpha1.LogMatcher_Equality{
					Equality: &policyv1alpha1.Value{
						Kind: &policyv1alpha1.Value_StringValue{
							StringValue: "/healthz",
						},
					},
				},
				Negated: false,
			},
			{
				Target: &policyv1alpha1.LogFieldSelector{
					Target: &policyv1alpha1.LogFieldSelector_LogAttributePath{
						LogAttributePath: &policyv1alpha1.AttributePath{
							Segments: []string{"http.status_code"},
						},
					},
				},
				Predicate: &policyv1alpha1.LogMatcher_InclusiveMinimum{
					InclusiveMinimum: &policyv1alpha1.NumericValue{
						Kind: &policyv1alpha1.NumericValue_IntValue{
							IntValue: 400,
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
				Predicate: &policyv1alpha1.LogMatcher_Existence{
					Existence: &emptypb.Empty{},
				},
				Negated: true,
			},
			{
				Target: &policyv1alpha1.LogFieldSelector{
					Target: &policyv1alpha1.LogFieldSelector_BodyFieldPath{
						BodyFieldPath: &policyv1alpha1.AttributePath{
							Segments: []string{"_SYSTEMD_UNIT"},
						},
					},
				},
				Predicate: &policyv1alpha1.LogMatcher_Equality{
					Equality: &policyv1alpha1.Value{
						Kind: &policyv1alpha1.Value_StringValue{
							StringValue: "sshd.service",
						},
					},
				},
			},
		},
	}

	// ProtoJSON marshal and unmarshal round-trip.
	jsonData, err := protojson.Marshal(policy)
	require.NoError(t, err)
	assert.Contains(t, string(jsonData), `"bodyFieldPath":{"segments":["_SYSTEMD_UNIT"]}`)

	unmarshaled := &policyv1alpha1.LogFilterPolicy{}
	require.NoError(t, protojson.Unmarshal(jsonData, unmarshaled))
	assert.True(t, proto.Equal(policy, unmarshaled))
	assert.NotNil(t, unmarshaled.Action)

	// Zero-value handling: ACTION_UNSPECIFIED explicit.
	jsonWithUnspecified := []byte(`{"id": "unspecified-policy", "action": "ACTION_UNSPECIFIED"}`)
	zeroPolicy := &policyv1alpha1.LogFilterPolicy{}
	require.NoError(t, protojson.Unmarshal(jsonWithUnspecified, zeroPolicy))
	assert.Equal(t, "unspecified-policy", zeroPolicy.GetId())
	assert.NotNil(t, zeroPolicy.Action)
	assert.Equal(t, policyv1alpha1.Action_ACTION_UNSPECIFIED, zeroPolicy.GetAction())

	// Action omitted in JSON defaults to ACTION_UNSPECIFIED but Action pointer is nil.
	jsonOmittedAction := []byte(`{"id": "omitted-action-policy"}`)
	omittedPolicy := &policyv1alpha1.LogFilterPolicy{}
	require.NoError(t, protojson.Unmarshal(jsonOmittedAction, omittedPolicy))
	assert.Equal(t, "omitted-action-policy", omittedPolicy.GetId())
	assert.Nil(t, omittedPolicy.Action)
	assert.Equal(t, policyv1alpha1.Action_ACTION_UNSPECIFIED, omittedPolicy.GetAction())

	// EmitUnpopulated includes ACTION_UNSPECIFIED in output.
	marshaledUnpopulated, err := protojson.MarshalOptions{EmitUnpopulated: true}.Marshal(zeroPolicy)
	require.NoError(t, err)
	assert.Contains(t, string(marshaledUnpopulated), `"action":"ACTION_UNSPECIFIED"`)
}

func TestLogFilterPolicy_EnumValues(t *testing.T) {
	assert.Equal(t, policyv1alpha1.LogRecordField(0), policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_UNSPECIFIED)
	assert.Equal(t, policyv1alpha1.LogRecordField(1), policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_BODY)
	assert.Equal(t, policyv1alpha1.LogRecordField(2), policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SEVERITY_TEXT)
	assert.Equal(t, policyv1alpha1.LogRecordField(3), policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SEVERITY_NUMBER)
	assert.Equal(t, policyv1alpha1.LogRecordField(4), policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_TRACE_ID)
	assert.Equal(t, policyv1alpha1.LogRecordField(5), policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SPAN_ID)
}
