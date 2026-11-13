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
				Source: &policyv1alpha1.MetricFieldSelector{
					Target: &policyv1alpha1.MetricFieldSelector_DataPointAttributePath{
						DataPointAttributePath: &policyv1alpha1.AttributePath{
							Segments: []string{"legacy_env"},
						},
					},
				},
				Target: &policyv1alpha1.MetricFieldSelector{
					Target: &policyv1alpha1.MetricFieldSelector_DataPointAttributePath{
						DataPointAttributePath: &policyv1alpha1.AttributePath{
							Segments: []string{"deployment.environment"},
						},
					},
				},
			},
		},
		Additions: []*policyv1alpha1.MetricAddition{
			{
				Target: &policyv1alpha1.MetricFieldSelector{
					Target: &policyv1alpha1.MetricFieldSelector_DataPointAttributePath{
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
		Id: "rename-metric-env-and-add-unit",
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
				Source: &policyv1alpha1.MetricFieldSelector{
					Target: &policyv1alpha1.MetricFieldSelector_DataPointAttributePath{
						DataPointAttributePath: &policyv1alpha1.AttributePath{
							Segments: []string{"legacy_env"},
						},
					},
				},
				Target: &policyv1alpha1.MetricFieldSelector{
					Target: &policyv1alpha1.MetricFieldSelector_DataPointAttributePath{
						DataPointAttributePath: &policyv1alpha1.AttributePath{
							Segments: []string{"deployment.environment"},
						},
					},
				},
			},
		},
		Additions: []*policyv1alpha1.MetricAddition{
			{
				Target: &policyv1alpha1.MetricFieldSelector{
					Target: &policyv1alpha1.MetricFieldSelector_DescriptorField{
						DescriptorField: policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_UNIT,
					},
				},
				Source: &policyv1alpha1.MetricAddition_Value{
					Value: &policyv1alpha1.Value{
						Kind: &policyv1alpha1.Value_StringValue{
							StringValue: "ms",
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
