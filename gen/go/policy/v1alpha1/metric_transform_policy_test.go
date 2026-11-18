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
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"

	policyv1alpha1 "github.com/GoogleCloudPlatform/opentelemetry-operations-collector/gen/go/policy/v1alpha1"
)

func TestMetricTransformPolicy_Serialization(t *testing.T) {
	policy := &policyv1alpha1.MetricTransformPolicy{
		Id: "normalize-and-label-metrics",
		Matchers: []*policyv1alpha1.MetricMatcher{
			{
				Target: &policyv1alpha1.MetricFieldSelector{
					Target: &policyv1alpha1.MetricFieldSelector_DescriptorField{
						DescriptorField: policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_NAME,
					},
				},
				Predicate: &policyv1alpha1.Predicate{
					Kind: &policyv1alpha1.Predicate_Equality{
						Equality: &policyv1alpha1.Value{
							Kind: &policyv1alpha1.Value_StringValue{
								StringValue: "system.cpu.time",
							},
						},
					},
				},
			},
		},
		Renamings: []*policyv1alpha1.MetricRenaming{
			{
				Source: &policyv1alpha1.MetricAttributeSelector{
					Target: &policyv1alpha1.MetricAttributeSelector_DataPointAttributePath{
						DataPointAttributePath: &policyv1alpha1.AttributePath{
							Segments: []string{"legacy_env"},
						},
					},
				},
				Target: &policyv1alpha1.MetricAttributeSelector{
					Target: &policyv1alpha1.MetricAttributeSelector_DataPointAttributePath{
						DataPointAttributePath: &policyv1alpha1.AttributePath{
							Segments: []string{"deployment.environment"},
						},
					},
				},
			},
		},
		Additions: []*policyv1alpha1.MetricAddition{
			{
				Target: &policyv1alpha1.MetricTargetSelector{
					Target: &policyv1alpha1.MetricTargetSelector_DataPointAttributePath{
						DataPointAttributePath: &policyv1alpha1.AttributePath{
							Segments: []string{"tier"},
						},
					},
				},
				Source: &policyv1alpha1.MetricAddition_Value{
					Value: &policyv1alpha1.Value{
						Kind: &policyv1alpha1.Value_StringValue{
							StringValue: "core",
						},
					},
				},
			},
		},
	}

	// Verify Any packaging.
	anyPolicy, err := anypb.New(policy)
	require.NoError(t, err)
	assert.Equal(t, "type.googleapis.com/google.telemetry.policy.v1alpha1.MetricTransformPolicy", anyPolicy.GetTypeUrl())

	// Round-trip binary serialization.
	data, err := proto.Marshal(policy)
	require.NoError(t, err)

	unmarshaled := &policyv1alpha1.MetricTransformPolicy{}
	require.NoError(t, proto.Unmarshal(data, unmarshaled))

	assert.True(t, proto.Equal(policy, unmarshaled))
	assert.Equal(t, "normalize-and-label-metrics", unmarshaled.GetId())
	require.Len(t, unmarshaled.GetMatchers(), 1)
	require.Len(t, unmarshaled.GetRenamings(), 1)
	require.Len(t, unmarshaled.GetAdditions(), 1)
}

func TestMetricTransformPolicy_JSONSerialization(t *testing.T) {
	policy := &policyv1alpha1.MetricTransformPolicy{
		Id: "rename-metric-env-and-add-nested-tier",
		Matchers: []*policyv1alpha1.MetricMatcher{
			{
				Target: &policyv1alpha1.MetricFieldSelector{
					Target: &policyv1alpha1.MetricFieldSelector_ResourceAttributePath{
						ResourceAttributePath: &policyv1alpha1.AttributePath{
							Segments: []string{"service.name"},
						},
					},
				},
				Predicate: &policyv1alpha1.Predicate{
					Kind: &policyv1alpha1.Predicate_Equality{
						Equality: &policyv1alpha1.Value{
							Kind: &policyv1alpha1.Value_StringValue{
								StringValue: "checkout",
							},
						},
					},
				},
			},
		},
		Renamings: []*policyv1alpha1.MetricRenaming{
			{
				Source: &policyv1alpha1.MetricAttributeSelector{
					Target: &policyv1alpha1.MetricAttributeSelector_DataPointAttributePath{
						DataPointAttributePath: &policyv1alpha1.AttributePath{
							Segments: []string{"legacy_env"},
						},
					},
				},
				Target: &policyv1alpha1.MetricAttributeSelector{
					Target: &policyv1alpha1.MetricAttributeSelector_DataPointAttributePath{
						DataPointAttributePath: &policyv1alpha1.AttributePath{
							Segments: []string{"deployment.environment"},
						},
					},
				},
			},
		},
		Additions: []*policyv1alpha1.MetricAddition{
			{
				Target: &policyv1alpha1.MetricTargetSelector{
					Target: &policyv1alpha1.MetricTargetSelector_DataPointAttributePath{
						DataPointAttributePath: &policyv1alpha1.AttributePath{
							Segments: []string{"service", "tier"},
						},
					},
				},
				Source: &policyv1alpha1.MetricAddition_Value{
					Value: &policyv1alpha1.Value{
						Kind: &policyv1alpha1.Value_StringValue{
							StringValue: "gold",
						},
					},
				},
			},
		},
	}

	jsonData, err := protojson.Marshal(policy)
	require.NoError(t, err)

	unmarshaled := &policyv1alpha1.MetricTransformPolicy{}
	require.NoError(t, protojson.Unmarshal(jsonData, unmarshaled))
	assert.True(t, proto.Equal(policy, unmarshaled))
}

func TestMetricTransformPolicy_AllStageListsAndUnconditionalMatchers(t *testing.T) {
	variants := []*policyv1alpha1.MetricTransformPolicy{
		{
			// Unconditional rename-only policy (empty matchers).
			Id: "rename-only-unconditional",
			Renamings: []*policyv1alpha1.MetricRenaming{
				{
					Source: &policyv1alpha1.MetricAttributeSelector{
						Target: &policyv1alpha1.MetricAttributeSelector_DataPointAttributePath{
							DataPointAttributePath: &policyv1alpha1.AttributePath{
								Segments: []string{"legacy", "env"},
							},
						},
					},
					Target: &policyv1alpha1.MetricAttributeSelector{
						Target: &policyv1alpha1.MetricAttributeSelector_DataPointAttributePath{
							DataPointAttributePath: &policyv1alpha1.AttributePath{
								Segments: []string{"deployment.environment"},
							},
						},
					},
				},
			},
		},
		{
			// Unconditional add-only policy (empty matchers).
			Id: "add-only-unconditional",
			Additions: []*policyv1alpha1.MetricAddition{
				{
					Target: &policyv1alpha1.MetricTargetSelector{
						Target: &policyv1alpha1.MetricTargetSelector_DataPointAttributePath{
							DataPointAttributePath: &policyv1alpha1.AttributePath{
								Segments: []string{"tier"},
							},
						},
					},
					Source: &policyv1alpha1.MetricAddition_Value{
						Value: &policyv1alpha1.Value{
							Kind: &policyv1alpha1.Value_IntValue{
								IntValue: 1,
							},
						},
					},
				},
			},
		},
	}

	for _, p := range variants {
		t.Run(p.GetId(), func(t *testing.T) {
			data, err := proto.Marshal(p)
			require.NoError(t, err)
			out := &policyv1alpha1.MetricTransformPolicy{}
			require.NoError(t, proto.Unmarshal(data, out))
			assert.True(t, proto.Equal(p, out))
			assert.Empty(t, out.GetMatchers())

			jsonData, err := protojson.Marshal(p)
			require.NoError(t, err)
			jsonOut := &policyv1alpha1.MetricTransformPolicy{}
			require.NoError(t, protojson.Unmarshal(jsonData, jsonOut))
			assert.True(t, proto.Equal(p, jsonOut))
			assert.Empty(t, jsonOut.GetMatchers())
		})
	}
}

func TestMetricTransformPolicy_OneofPresenceAndParentSelectorRejection(t *testing.T) {
	// 1. Omitted oneof wrappers remain nil.
	omittedJSON := []byte(`{
		"id": "omitted-oneofs",
		"renamings": [{"source": {}, "target": {}}],
		"additions": [{"target": {}}]
	}`)
	omittedPolicy := &policyv1alpha1.MetricTransformPolicy{}
	require.NoError(t, protojson.Unmarshal(omittedJSON, omittedPolicy))
	require.Len(t, omittedPolicy.GetRenamings(), 1)
	assert.Nil(t, omittedPolicy.GetRenamings()[0].GetSource().GetTarget())
	assert.Nil(t, omittedPolicy.GetRenamings()[0].GetTarget().GetTarget())
	require.Len(t, omittedPolicy.GetAdditions(), 1)
	assert.Nil(t, omittedPolicy.GetAdditions()[0].GetTarget().GetTarget())
	assert.Nil(t, omittedPolicy.GetAdditions()[0].GetSource())

	// 2. Canonical JSON wire names and explicit zero scalar values preserve oneof presence.
	explicitZeroJSON := []byte(`{
		"id": "explicit-zero-scalars",
		"renamings": [
			{
				"source": {"dataPointAttributePath": {"segments": ["old"]}},
				"target": {"dataPointAttributePath": {"segments": ["new"]}}
			}
		],
		"additions": [
			{
				"target": {"dataPointAttributePath": {"segments": ["empty_str"]}},
				"value": {"stringValue": ""}
			},
			{
				"target": {"dataPointAttributePath": {"segments": ["zero_int"]}},
				"value": {"intValue": "0"}
			},
			{
				"target": {"dataPointAttributePath": {"segments": ["false_bool"]}},
				"value": {"boolValue": false}
			}
		]
	}`)
	zeroPolicy := &policyv1alpha1.MetricTransformPolicy{}
	require.NoError(t, protojson.Unmarshal(explicitZeroJSON, zeroPolicy))

	require.Len(t, zeroPolicy.GetRenamings(), 1)
	require.IsType(t, &policyv1alpha1.MetricAttributeSelector_DataPointAttributePath{}, zeroPolicy.GetRenamings()[0].GetSource().GetTarget())
	require.IsType(t, &policyv1alpha1.MetricAttributeSelector_DataPointAttributePath{}, zeroPolicy.GetRenamings()[0].GetTarget().GetTarget())

	require.Len(t, zeroPolicy.GetAdditions(), 3)
	for _, add := range zeroPolicy.GetAdditions() {
		require.IsType(t, &policyv1alpha1.MetricTargetSelector_DataPointAttributePath{}, add.GetTarget().GetTarget())
		require.IsType(t, &policyv1alpha1.MetricAddition_Value{}, add.GetSource())
	}
	require.IsType(t, &policyv1alpha1.Value_StringValue{}, zeroPolicy.GetAdditions()[0].GetValue().GetKind())
	assert.Equal(t, "", zeroPolicy.GetAdditions()[0].GetValue().GetStringValue())
	require.IsType(t, &policyv1alpha1.Value_IntValue{}, zeroPolicy.GetAdditions()[1].GetValue().GetKind())
	assert.Equal(t, int64(0), zeroPolicy.GetAdditions()[1].GetValue().GetIntValue())
	require.IsType(t, &policyv1alpha1.Value_BoolValue{}, zeroPolicy.GetAdditions()[2].GetValue().GetKind())
	assert.False(t, zeroPolicy.GetAdditions()[2].GetValue().GetBoolValue())

	// 3. Shared parent selectors are rejected by the schema in both MetricAttributeSelector and MetricTargetSelector.
	rejectedCases := []struct {
		name      string
		wantField string
		json      string
	}{
		{
			name:      "renaming source descriptorField",
			wantField: "descriptorField",
			json:      `{"id":"bad","renamings":[{"source":{"descriptorField":"METRIC_DESCRIPTOR_FIELD_NAME"},"target":{"dataPointAttributePath":{"segments":["a"]}}}]}`,
		},
		{
			name:      "renaming source resourceAttributePath",
			wantField: "resourceAttributePath",
			json:      `{"id":"bad","renamings":[{"source":{"resourceAttributePath":{"segments":["service.name"]}},"target":{"dataPointAttributePath":{"segments":["a"]}}}]}`,
		},
		{
			name:      "renaming source scopeAttributePath",
			wantField: "scopeAttributePath",
			json:      `{"id":"bad","renamings":[{"source":{"scopeAttributePath":{"segments":["lib"]}},"target":{"dataPointAttributePath":{"segments":["a"]}}}]}`,
		},
		{
			name:      "renaming source scopeField",
			wantField: "scopeField",
			json:      `{"id":"bad","renamings":[{"source":{"scopeField":"SCOPE_FIELD_NAME"},"target":{"dataPointAttributePath":{"segments":["a"]}}}]}`,
		},
		{
			name:      "renaming target resourceAttributePath",
			wantField: "resourceAttributePath",
			json:      `{"id":"bad","renamings":[{"source":{"dataPointAttributePath":{"segments":["a"]}},"target":{"resourceAttributePath":{"segments":["b"]}}}]}`,
		},
		{
			name:      "renaming target descriptorField",
			wantField: "descriptorField",
			json:      `{"id":"bad","renamings":[{"source":{"dataPointAttributePath":{"segments":["a"]}},"target":{"descriptorField":"METRIC_DESCRIPTOR_FIELD_UNIT"}}]}`,
		},
		{
			name:      "renaming target scopeAttributePath",
			wantField: "scopeAttributePath",
			json:      `{"id":"bad","renamings":[{"source":{"dataPointAttributePath":{"segments":["a"]}},"target":{"scopeAttributePath":{"segments":["b"]}}}]}`,
		},
		{
			name:      "renaming target scopeField",
			wantField: "scopeField",
			json:      `{"id":"bad","renamings":[{"source":{"dataPointAttributePath":{"segments":["a"]}},"target":{"scopeField":"SCOPE_FIELD_VERSION"}}]}`,
		},
		{
			name:      "addition target descriptorField",
			wantField: "descriptorField",
			json:      `{"id":"bad","additions":[{"target":{"descriptorField":"METRIC_DESCRIPTOR_FIELD_UNIT"},"value":{"stringValue":"ms"}}]}`,
		},
		{
			name:      "addition target resourceAttributePath",
			wantField: "resourceAttributePath",
			json:      `{"id":"bad","additions":[{"target":{"resourceAttributePath":{"segments":["env"]}},"value":{"stringValue":"prod"}}]}`,
		},
		{
			name:      "addition target scopeAttributePath",
			wantField: "scopeAttributePath",
			json:      `{"id":"bad","additions":[{"target":{"scopeAttributePath":{"segments":["lib"]}},"value":{"stringValue":"v1"}}]}`,
		},
		{
			name:      "addition target scopeField",
			wantField: "scopeField",
			json:      `{"id":"bad","additions":[{"target":{"scopeField":"SCOPE_FIELD_NAME"},"value":{"stringValue":"scope"}}]}`,
		},
	}

	for _, tc := range rejectedCases {
		t.Run(tc.name, func(t *testing.T) {
			out := &policyv1alpha1.MetricTransformPolicy{}
			err := protojson.Unmarshal([]byte(tc.json), out)
			require.ErrorContains(t, err, fmt.Sprintf("unknown field %q", tc.wantField), "expected protojson.Unmarshal to reject forbidden parent selector in %s", tc.name)
		})
	}
}
