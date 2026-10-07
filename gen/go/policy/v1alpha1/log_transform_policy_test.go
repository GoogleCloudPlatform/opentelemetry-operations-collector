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

func TestLogTransformPolicy_Serialization(t *testing.T) {
	policy := &policyv1alpha1.LogTransformPolicy{
		Id: "label-soc-logs",
		Matches: []*policyv1alpha1.LogMatcher{
			{
				Target: &policyv1alpha1.LogFieldSelector{
					Target: &policyv1alpha1.LogFieldSelector_LogAttribute{
						LogAttribute: &policyv1alpha1.AttributePath{
							Path: []string{"winlog", "event_id"},
						},
					},
				},
				Predicate: &policyv1alpha1.LogMatcher_Equals{
					Equals: &policyv1alpha1.Value{
						Value: &policyv1alpha1.Value_IntValue{
							IntValue: 4625,
						},
					},
				},
			},
		},
		Action: &policyv1alpha1.LogTransformPolicy_Add{
			Add: &policyv1alpha1.LogAddAction{
				Target: &policyv1alpha1.LogFieldSelector{
					Target: &policyv1alpha1.LogFieldSelector_LogAttribute{
						LogAttribute: &policyv1alpha1.AttributePath{
							Path: []string{"threat_tier"},
						},
					},
				},
				Value: &policyv1alpha1.Value{
					Value: &policyv1alpha1.Value_StringValue{
						StringValue: "HOT_ACTIVE_SOC",
					},
				},
				Upsert: true,
			},
		},
	}

	// Verify Any packaging.
	anyPolicy, err := anypb.New(policy)
	require.NoError(t, err)
	assert.Equal(t, "type.googleapis.com/google.telemetry.policy.v1alpha1.LogTransformPolicy", anyPolicy.GetTypeUrl())

	// Round-trip binary serialization.
	data, err := proto.Marshal(policy)
	require.NoError(t, err)

	unmarshaled := &policyv1alpha1.LogTransformPolicy{}
	require.NoError(t, proto.Unmarshal(data, unmarshaled))

	assert.True(t, proto.Equal(policy, unmarshaled))
	assert.Equal(t, "label-soc-logs", unmarshaled.GetId())
	require.Len(t, unmarshaled.GetMatches(), 1)
	require.NotNil(t, unmarshaled.GetAdd())
	assert.True(t, unmarshaled.GetAdd().GetUpsert())
}

func TestLogTransformPolicy_JSONSerialization(t *testing.T) {
	policy := &policyv1alpha1.LogTransformPolicy{
		Id: "add-utr-routing-label",
		Matches: []*policyv1alpha1.LogMatcher{
			{
				Target: &policyv1alpha1.LogFieldSelector{
					Target: &policyv1alpha1.LogFieldSelector_ResourceAttribute{
						ResourceAttribute: &policyv1alpha1.AttributePath{
							Path: []string{"service.name"},
						},
					},
				},
				Predicate: &policyv1alpha1.LogMatcher_Equals{
					Equals: &policyv1alpha1.Value{
						Value: &policyv1alpha1.Value_StringValue{
							StringValue: "active-directory",
						},
					},
				},
			},
		},
		Action: &policyv1alpha1.LogTransformPolicy_Add{
			Add: &policyv1alpha1.LogAddAction{
				Target: &policyv1alpha1.LogFieldSelector{
					Target: &policyv1alpha1.LogFieldSelector_LogAttribute{
						LogAttribute: &policyv1alpha1.AttributePath{
							Path: []string{"threat_tier"},
						},
					},
				},
				Value: &policyv1alpha1.Value{
					Value: &policyv1alpha1.Value_StringValue{
						StringValue: "LAKEHOUSE_ARCHIVE",
					},
				},
				Upsert: true,
			},
		},
	}

	jsonData, err := protojson.Marshal(policy)
	require.NoError(t, err)

	unmarshaled := &policyv1alpha1.LogTransformPolicy{}
	require.NoError(t, protojson.Unmarshal(jsonData, unmarshaled))
	assert.True(t, proto.Equal(policy, unmarshaled))
}

func TestLogTransformPolicy_AllActionVariants(t *testing.T) {
	selector := &policyv1alpha1.LogFieldSelector{
		Target: &policyv1alpha1.LogFieldSelector_LogAttribute{
			LogAttribute: &policyv1alpha1.AttributePath{Path: []string{"field"}},
		},
	}

	variants := []*policyv1alpha1.LogTransformPolicy{
		{
			Id: "remove-action",
			Action: &policyv1alpha1.LogTransformPolicy_Remove{
				Remove: &policyv1alpha1.LogRemoveAction{Target: selector},
			},
		},
		{
			Id: "rename-action",
			Action: &policyv1alpha1.LogTransformPolicy_Rename{
				Rename: &policyv1alpha1.LogRenameAction{
					From:   selector,
					To:     &policyv1alpha1.LogFieldSelector{Target: &policyv1alpha1.LogFieldSelector_LogAttribute{LogAttribute: &policyv1alpha1.AttributePath{Path: []string{"new_field"}}}},
					Upsert: true,
				},
			},
		},
		{
			Id: "redact-action",
			Action: &policyv1alpha1.LogTransformPolicy_Redact{
				Redact: &policyv1alpha1.LogRedactAction{
					Target:      selector,
					Regex:       `\d{3}-\d{2}-\d{4}`,
					Replacement: "[REDACTED]",
				},
			},
		},
	}

	for _, p := range variants {
		t.Run(p.GetId(), func(t *testing.T) {
			data, err := proto.Marshal(p)
			require.NoError(t, err)
			out := &policyv1alpha1.LogTransformPolicy{}
			require.NoError(t, proto.Unmarshal(data, out))
			assert.True(t, proto.Equal(p, out))
		})
	}
}
