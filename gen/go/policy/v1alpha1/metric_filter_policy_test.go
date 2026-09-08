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

func TestMetricFilterPolicy_Serialization(t *testing.T) {
	policy := &policyv1alpha1.MetricFilterPolicy{
		Id:     "drop-noisy-http-metrics",
		Action: policyv1alpha1.Action_ACTION_DROP.Enum(),
		Matches: []*policyv1alpha1.MetricMatcher{
			{
				Target: &policyv1alpha1.MetricFieldSelector{
					Target: &policyv1alpha1.MetricFieldSelector_DescriptorField{
						DescriptorField: policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_NAME,
					},
				},
				Predicate: &policyv1alpha1.MetricMatcher_Regex{
					Regex: "^http\\.server\\..*",
				},
				Negate: false,
			},
			{
				Target: &policyv1alpha1.MetricFieldSelector{
					Target: &policyv1alpha1.MetricFieldSelector_DescriptorField{
						DescriptorField: policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_TYPE,
					},
				},
				Predicate: &policyv1alpha1.MetricMatcher_Equals{
					Equals: &policyv1alpha1.Value{
						Value: &policyv1alpha1.Value_StringValue{
							StringValue: "HISTOGRAM",
						},
					},
				},
			},
			{
				Target: &policyv1alpha1.MetricFieldSelector{
					Target: &policyv1alpha1.MetricFieldSelector_DatapointAttribute{
						DatapointAttribute: &policyv1alpha1.AttributePath{
							Path: []string{"http.status_code"},
						},
					},
				},
				Predicate: &policyv1alpha1.MetricMatcher_Equals{
					Equals: &policyv1alpha1.Value{
						Value: &policyv1alpha1.Value_IntValue{
							IntValue: 200,
						},
					},
				},
			},
			{
				Target: &policyv1alpha1.MetricFieldSelector{
					Target: &policyv1alpha1.MetricFieldSelector_DatapointAttribute{
						DatapointAttribute: &policyv1alpha1.AttributePath{
							Path: []string{"http.status_code"},
						},
					},
				},
				Predicate: &policyv1alpha1.MetricMatcher_Gte{
					Gte: &policyv1alpha1.NumericValue{
						Value: &policyv1alpha1.NumericValue_IntValue{
							IntValue: 400,
						},
					},
				},
			},
			{
				Target: &policyv1alpha1.MetricFieldSelector{
					Target: &policyv1alpha1.MetricFieldSelector_DatapointAttribute{
						DatapointAttribute: &policyv1alpha1.AttributePath{
							Path: []string{"pools", "active"},
						},
					},
				},
				Predicate: &policyv1alpha1.MetricMatcher_Contains{
					Contains: &policyv1alpha1.Value{
						Value: &policyv1alpha1.Value_StringValue{
							StringValue: "worker",
						},
					},
				},
			},
			{
				Target: &policyv1alpha1.MetricFieldSelector{
					Target: &policyv1alpha1.MetricFieldSelector_DatapointAttribute{
						DatapointAttribute: &policyv1alpha1.AttributePath{
							Path: []string{"http.status_code"},
						},
					},
				},
				Predicate: &policyv1alpha1.MetricMatcher_Exists{
					Exists: &emptypb.Empty{},
				},
				Negate: true,
			},
			{
				Target: &policyv1alpha1.MetricFieldSelector{
					Target: &policyv1alpha1.MetricFieldSelector_ResourceAttribute{
						ResourceAttribute: &policyv1alpha1.AttributePath{
							Path: []string{"service.name"},
						},
					},
				},
				Predicate: &policyv1alpha1.MetricMatcher_Equals{
					Equals: &policyv1alpha1.Value{
						Value: &policyv1alpha1.Value_StringValue{
							StringValue: "frontend",
						},
					},
				},
			},
			{
				Target: &policyv1alpha1.MetricFieldSelector{
					Target: &policyv1alpha1.MetricFieldSelector_ScopeAttribute{
						ScopeAttribute: &policyv1alpha1.AttributePath{
							Path: []string{"library.name"},
						},
					},
				},
				Predicate: &policyv1alpha1.MetricMatcher_Equals{
					Equals: &policyv1alpha1.Value{
						Value: &policyv1alpha1.Value_StringValue{
							StringValue: "net/http",
						},
					},
				},
			},
			{
				Target: &policyv1alpha1.MetricFieldSelector{
					Target: &policyv1alpha1.MetricFieldSelector_ScopeField{
						ScopeField: policyv1alpha1.ScopeField_SCOPE_FIELD_NAME,
					},
				},
				Predicate: &policyv1alpha1.MetricMatcher_Equals{
					Equals: &policyv1alpha1.Value{
						Value: &policyv1alpha1.Value_StringValue{
							StringValue: "go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp",
						},
					},
				},
			},
			{
				Target: &policyv1alpha1.MetricFieldSelector{
					Target: &policyv1alpha1.MetricFieldSelector_ScopeField{
						ScopeField: policyv1alpha1.ScopeField_SCOPE_FIELD_VERSION,
					},
				},
				Predicate: &policyv1alpha1.MetricMatcher_Equals{
					Equals: &policyv1alpha1.Value{
						Value: &policyv1alpha1.Value_StringValue{
							StringValue: "v0.40.0",
						},
					},
				},
			},
			{
				Target: &policyv1alpha1.MetricFieldSelector{
					Target: &policyv1alpha1.MetricFieldSelector_ScopeField{
						ScopeField: policyv1alpha1.ScopeField_SCOPE_FIELD_SCHEMA_URL,
					},
				},
				Predicate: &policyv1alpha1.MetricMatcher_Equals{
					Equals: &policyv1alpha1.Value{
						Value: &policyv1alpha1.Value_StringValue{
							StringValue: "https://opentelemetry.io/schemas/1.24.0",
						},
					},
				},
			},
			{
				Target: &policyv1alpha1.MetricFieldSelector{
					Target: &policyv1alpha1.MetricFieldSelector_DescriptorField{
						DescriptorField: policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_AGGREGATION_TEMPORALITY,
					},
				},
				Predicate: &policyv1alpha1.MetricMatcher_Equals{
					Equals: &policyv1alpha1.Value{
						Value: &policyv1alpha1.Value_StringValue{
							StringValue: "DELTA",
						},
					},
				},
			},
			{
				Target: &policyv1alpha1.MetricFieldSelector{
					Target: &policyv1alpha1.MetricFieldSelector_DescriptorField{
						DescriptorField: policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_IS_MONOTONIC,
					},
				},
				Predicate: &policyv1alpha1.MetricMatcher_Equals{
					Equals: &policyv1alpha1.Value{
						Value: &policyv1alpha1.Value_BoolValue{
							BoolValue: true,
						},
					},
				},
			},
		},
	}

	// Verify Marshal & Unmarshal round-trip.
	data, err := proto.Marshal(policy)
	require.NoError(t, err)

	unmarshaled := &policyv1alpha1.MetricFilterPolicy{}
	require.NoError(t, proto.Unmarshal(data, unmarshaled))

	assert.True(t, proto.Equal(policy, unmarshaled))
	assert.NotNil(t, unmarshaled.Action)
	assert.Equal(t, policy.GetId(), unmarshaled.GetId())
	assert.Equal(t, policy.GetAction(), unmarshaled.GetAction())
	require.Len(t, unmarshaled.GetMatches(), 13)

	// Check descriptor field name matcher.
	matcher0 := unmarshaled.GetMatches()[0]
	descFieldName := matcher0.GetTarget().GetDescriptorField()
	assert.Equal(t, policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_NAME, descFieldName)
	assert.Equal(t, "^http\\.server\\..*", matcher0.GetRegex())
	assert.False(t, matcher0.GetNegate())

	// Check descriptor field type matcher.
	matcher1 := unmarshaled.GetMatches()[1]
	descFieldType := matcher1.GetTarget().GetDescriptorField()
	assert.Equal(t, policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_TYPE, descFieldType)
	assert.Equal(t, "HISTOGRAM", matcher1.GetEquals().GetStringValue())

	// Check datapoint attribute equals matcher with int value.
	matcher2 := unmarshaled.GetMatches()[2]
	assert.Equal(t, "http.status_code", matcher2.GetTarget().GetDatapointAttribute().GetPath()[0])
	assert.Equal(t, int64(200), matcher2.GetEquals().GetIntValue())

	// Check datapoint attribute gte matcher with int value.
	matcher3 := unmarshaled.GetMatches()[3]
	assert.Equal(t, "http.status_code", matcher3.GetTarget().GetDatapointAttribute().GetPath()[0])
	assert.Equal(t, int64(400), matcher3.GetGte().GetIntValue())

	// Check contains matcher.
	matcher4 := unmarshaled.GetMatches()[4]
	assert.Equal(t, []string{"pools", "active"}, matcher4.GetTarget().GetDatapointAttribute().GetPath())
	assert.Equal(t, "worker", matcher4.GetContains().GetStringValue())

	// Check datapoint attribute exists matcher with negate.
	matcher5 := unmarshaled.GetMatches()[5]
	assert.Equal(t, "http.status_code", matcher5.GetTarget().GetDatapointAttribute().GetPath()[0])
	assert.NotNil(t, matcher5.GetExists())
	assert.True(t, matcher5.GetNegate())

	// Check scope field matcher.
	matcher8 := unmarshaled.GetMatches()[8]
	assert.Equal(t, policyv1alpha1.ScopeField_SCOPE_FIELD_NAME, matcher8.GetTarget().GetScopeField())
	assert.Equal(t, "go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp", matcher8.GetEquals().GetStringValue())

	// Check descriptor field aggregation temporality matcher.
	matcher11 := unmarshaled.GetMatches()[11]
	descFieldTemporality := matcher11.GetTarget().GetDescriptorField()
	assert.Equal(t, policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_AGGREGATION_TEMPORALITY, descFieldTemporality)
	assert.Equal(t, "DELTA", matcher11.GetEquals().GetStringValue())

	// Check descriptor field is monotonic matcher.
	matcher12 := unmarshaled.GetMatches()[12]
	descFieldMonotonic := matcher12.GetTarget().GetDescriptorField()
	assert.Equal(t, policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_IS_MONOTONIC, descFieldMonotonic)
	assert.True(t, matcher12.GetEquals().GetBoolValue())
}

func TestMetricFilterPolicy_AnyPackaging(t *testing.T) {
	policy := &policyv1alpha1.MetricFilterPolicy{
		Id:     "drop-metrics-policy",
		Action: policyv1alpha1.Action_ACTION_DROP.Enum(),
	}

	anyPolicy, err := anypb.New(policy)
	require.NoError(t, err)
	assert.Equal(t, "type.googleapis.com/google.telemetry.policy.v1alpha1.MetricFilterPolicy", anyPolicy.GetTypeUrl())

	// Wrap inside TelemetryCollector.
	collector := &xdsv1alpha1.TelemetryCollector{
		Policies: []*corev3.TypedExtensionConfig{
			{
				Name:        "metric-filter-policy",
				TypedConfig: anyPolicy,
			},
		},
	}

	// Verify round-trip serialization.
	data, err := proto.Marshal(collector)
	require.NoError(t, err)

	unmarshaled := &xdsv1alpha1.TelemetryCollector{}
	require.NoError(t, proto.Unmarshal(data, unmarshaled))

	require.Len(t, unmarshaled.GetPolicies(), 1)
	assert.Equal(t, "metric-filter-policy", unmarshaled.GetPolicies()[0].GetName())

	unpackedPolicy := &policyv1alpha1.MetricFilterPolicy{}
	require.NoError(t, unmarshaled.GetPolicies()[0].GetTypedConfig().UnmarshalTo(unpackedPolicy))
	assert.Equal(t, "drop-metrics-policy", unpackedPolicy.GetId())
	assert.NotNil(t, unpackedPolicy.Action)
	assert.Equal(t, policyv1alpha1.Action_ACTION_DROP, unpackedPolicy.GetAction())
}

func TestMetricFilterPolicy_JSONSerialization(t *testing.T) {
	policy := &policyv1alpha1.MetricFilterPolicy{
		Id:     "drop-noisy-http-metrics",
		Action: policyv1alpha1.Action_ACTION_DROP.Enum(),
		Matches: []*policyv1alpha1.MetricMatcher{
			{
				Target: &policyv1alpha1.MetricFieldSelector{
					Target: &policyv1alpha1.MetricFieldSelector_DatapointAttribute{
						DatapointAttribute: &policyv1alpha1.AttributePath{
							Path: []string{"http.status_code"},
						},
					},
				},
				Predicate: &policyv1alpha1.MetricMatcher_Equals{
					Equals: &policyv1alpha1.Value{
						Value: &policyv1alpha1.Value_IntValue{
							IntValue: 200,
						},
					},
				},
				Negate: false,
			},
			{
				Target: &policyv1alpha1.MetricFieldSelector{
					Target: &policyv1alpha1.MetricFieldSelector_ScopeField{
						ScopeField: policyv1alpha1.ScopeField_SCOPE_FIELD_SCHEMA_URL,
					},
				},
				Predicate: &policyv1alpha1.MetricMatcher_Exists{
					Exists: &emptypb.Empty{},
				},
				Negate: true,
			},
		},
	}

	// ProtoJSON marshal and unmarshal round-trip.
	jsonData, err := protojson.Marshal(policy)
	require.NoError(t, err)

	unmarshaled := &policyv1alpha1.MetricFilterPolicy{}
	require.NoError(t, protojson.Unmarshal(jsonData, unmarshaled))
	assert.True(t, proto.Equal(policy, unmarshaled))
	assert.NotNil(t, unmarshaled.Action)

	// Zero-value handling: ACTION_UNSPECIFIED explicit.
	jsonWithUnspecified := []byte(`{"id": "unspecified-policy", "action": "ACTION_UNSPECIFIED"}`)
	zeroPolicy := &policyv1alpha1.MetricFilterPolicy{}
	require.NoError(t, protojson.Unmarshal(jsonWithUnspecified, zeroPolicy))
	assert.Equal(t, "unspecified-policy", zeroPolicy.GetId())
	assert.NotNil(t, zeroPolicy.Action)
	assert.Equal(t, policyv1alpha1.Action_ACTION_UNSPECIFIED, zeroPolicy.GetAction())

	// Action omitted in JSON defaults to ACTION_UNSPECIFIED but Action pointer is nil.
	jsonOmittedAction := []byte(`{"id": "omitted-action-policy"}`)
	omittedPolicy := &policyv1alpha1.MetricFilterPolicy{}
	require.NoError(t, protojson.Unmarshal(jsonOmittedAction, omittedPolicy))
	assert.Equal(t, "omitted-action-policy", omittedPolicy.GetId())
	assert.Nil(t, omittedPolicy.Action)
	assert.Equal(t, policyv1alpha1.Action_ACTION_UNSPECIFIED, omittedPolicy.GetAction())

	// EmitUnpopulated includes ACTION_UNSPECIFIED in output.
	marshaledUnpopulated, err := protojson.MarshalOptions{EmitUnpopulated: true}.Marshal(zeroPolicy)
	require.NoError(t, err)
	assert.Contains(t, string(marshaledUnpopulated), `"action":"ACTION_UNSPECIFIED"`)
}

func TestMetricFilterPolicy_EnumValues(t *testing.T) {
	// Verify MetricDescriptorField enum mappings.
	assert.Equal(t, int32(0), int32(policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_UNSPECIFIED))
	assert.Equal(t, int32(1), int32(policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_NAME))
	assert.Equal(t, int32(2), int32(policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_DESCRIPTION))
	assert.Equal(t, int32(3), int32(policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_UNIT))
	assert.Equal(t, int32(4), int32(policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_TYPE))
	assert.Equal(t, int32(5), int32(policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_AGGREGATION_TEMPORALITY))
	assert.Equal(t, int32(6), int32(policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_IS_MONOTONIC))
}
