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
					Target: &policyv1alpha1.LogFieldSelector_BodyField{
						BodyField: &policyv1alpha1.AttributePath{
							Path: []string{"event_id"},
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
				Target: &policyv1alpha1.LogTargetSelector{
					Target: &policyv1alpha1.LogTargetSelector_LogAttribute{
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
				Upsert: proto.Bool(true),
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
	require.NotNil(t, unmarshaled.GetAdd().Upsert)
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
			{
				Target: &policyv1alpha1.LogFieldSelector{
					Target: &policyv1alpha1.LogFieldSelector_BodyField{
						BodyField: &policyv1alpha1.AttributePath{
							Path: []string{"event_data", "TargetUserName"},
						},
					},
				},
				Predicate: &policyv1alpha1.LogMatcher_Glob{
					Glob: "*$",
				},
			},
		},
		Action: &policyv1alpha1.LogTransformPolicy_Add{
			Add: &policyv1alpha1.LogAddAction{
				Target: &policyv1alpha1.LogTargetSelector{
					Target: &policyv1alpha1.LogTargetSelector_LogAttribute{
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
				Upsert: proto.Bool(true),
			},
		},
	}

	jsonData, err := protojson.Marshal(policy)
	require.NoError(t, err)

	unmarshaled := &policyv1alpha1.LogTransformPolicy{}
	require.NoError(t, protojson.Unmarshal(jsonData, unmarshaled))
	assert.True(t, proto.Equal(policy, unmarshaled))
}

func TestLogTransformPolicy_AllActionVariantsAndUnconditionalMatches(t *testing.T) {
	variants := []*policyv1alpha1.LogTransformPolicy{
		{
			// ST2 Step 4: Unconditional remove action (empty matches).
			Id: "remove-local-scriptpath",
			Action: &policyv1alpha1.LogTransformPolicy_Remove{
				Remove: &policyv1alpha1.LogRemoveAction{
					Target: &policyv1alpha1.LogAttributeSelector{
						Target: &policyv1alpha1.LogAttributeSelector_LogAttribute{
							LogAttribute: &policyv1alpha1.AttributePath{Path: []string{"LOCAL_SCRIPTPATH_V1"}},
						},
					},
				},
			},
		},
		{
			// Rename action from body_field to log_attribute.
			Id: "rename-legacy-user-id",
			Action: &policyv1alpha1.LogTransformPolicy_Rename{
				Rename: &policyv1alpha1.LogRenameAction{
					Source: &policyv1alpha1.LogAttributeSelector{
						Target: &policyv1alpha1.LogAttributeSelector_BodyField{
							BodyField: &policyv1alpha1.AttributePath{Path: []string{"event_data", "userId"}},
						},
					},
					Target: &policyv1alpha1.LogAttributeSelector{
						Target: &policyv1alpha1.LogAttributeSelector_LogAttribute{
							LogAttribute: &policyv1alpha1.AttributePath{Path: []string{"user.id"}},
						},
					},
					Upsert: proto.Bool(false),
				},
			},
		},
		{
			// ST1: Unconditional redact action (empty matches).
			Id: "redact-ssn",
			Action: &policyv1alpha1.LogTransformPolicy_Redact{
				Redact: &policyv1alpha1.LogRedactAction{
					Target: &policyv1alpha1.LogTargetSelector{
						Target: &policyv1alpha1.LogTargetSelector_RecordField{
							RecordField: policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_BODY,
						},
					},
					Regex:       proto.String(`\b\d{3}-\d{2}-\d{4}\b`),
					Replacement: proto.String("[REDACTED_SSN]"),
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

			jsonData, err := protojson.Marshal(p)
			require.NoError(t, err)
			jsonOut := &policyv1alpha1.LogTransformPolicy{}
			require.NoError(t, protojson.Unmarshal(jsonData, jsonOut))
			assert.True(t, proto.Equal(p, jsonOut))
		})
	}
}

func TestLogTransformPolicy_OptionalFieldsPresence(t *testing.T) {
	// 1. Omitted optional fields remain nil while getters return default zero values.
	omittedJSON := []byte(`{
		"id": "redact-entire-attribute",
		"redact": {
			"target": {
				"logAttribute": {"path": ["secret_token"]}
			}
		}
	}`)
	omittedPolicy := &policyv1alpha1.LogTransformPolicy{}
	require.NoError(t, protojson.Unmarshal(omittedJSON, omittedPolicy))
	require.NotNil(t, omittedPolicy.GetRedact())
	assert.Nil(t, omittedPolicy.GetRedact().Regex)
	assert.Equal(t, "", omittedPolicy.GetRedact().GetRegex())
	assert.Nil(t, omittedPolicy.GetRedact().Replacement)
	assert.Equal(t, "", omittedPolicy.GetRedact().GetReplacement())

	// 2. Explicit zero values (upsert: false, replacement: "") preserve presence.
	explicitZeroJSON := []byte(`{
		"id": "add-no-upsert",
		"add": {
			"target": {"logAttribute": {"path": ["tier"]}},
			"value": {"stringValue": "default"},
			"upsert": false
		}
	}`)
	explicitZeroPolicy := &policyv1alpha1.LogTransformPolicy{}
	require.NoError(t, protojson.Unmarshal(explicitZeroJSON, explicitZeroPolicy))
	require.NotNil(t, explicitZeroPolicy.GetAdd())
	require.NotNil(t, explicitZeroPolicy.GetAdd().Upsert)
	assert.False(t, explicitZeroPolicy.GetAdd().GetUpsert())
}

func TestLogTransformPolicy_RecordFieldAndNestedPathTargets(t *testing.T) {
	policies := []*policyv1alpha1.LogTransformPolicy{
		{
			Id: "add-record-field-body-fallback",
			Action: &policyv1alpha1.LogTransformPolicy_Add{
				Add: &policyv1alpha1.LogAddAction{
					Target: &policyv1alpha1.LogTargetSelector{
						Target: &policyv1alpha1.LogTargetSelector_RecordField{
							RecordField: policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_BODY,
						},
					},
					Value: &policyv1alpha1.Value{
						Value: &policyv1alpha1.Value_StringValue{StringValue: "default-body"},
					},
					Upsert: proto.Bool(false),
				},
			},
		},
		{
			Id: "add-record-field-severity-text",
			Action: &policyv1alpha1.LogTransformPolicy_Add{
				Add: &policyv1alpha1.LogAddAction{
					Target: &policyv1alpha1.LogTargetSelector{
						Target: &policyv1alpha1.LogTargetSelector_RecordField{
							RecordField: policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SEVERITY_TEXT,
						},
					},
					Value: &policyv1alpha1.Value{
						Value: &policyv1alpha1.Value_StringValue{StringValue: "WARN"},
					},
					Upsert: proto.Bool(true),
				},
			},
		},
		{
			Id: "add-record-field-severity-number",
			Action: &policyv1alpha1.LogTransformPolicy_Add{
				Add: &policyv1alpha1.LogAddAction{
					Target: &policyv1alpha1.LogTargetSelector{
						Target: &policyv1alpha1.LogTargetSelector_RecordField{
							RecordField: policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SEVERITY_NUMBER,
						},
					},
					Value: &policyv1alpha1.Value{
						Value: &policyv1alpha1.Value_IntValue{IntValue: 13},
					},
					Upsert: proto.Bool(true),
				},
			},
		},
		{
			Id: "add-nested-body-field-upsert",
			Action: &policyv1alpha1.LogTransformPolicy_Add{
				Add: &policyv1alpha1.LogAddAction{
					Target: &policyv1alpha1.LogTargetSelector{
						Target: &policyv1alpha1.LogTargetSelector_BodyField{
							BodyField: &policyv1alpha1.AttributePath{
								Path: []string{"event_data", "security", "classification"},
							},
						},
					},
					Value: &policyv1alpha1.Value{
						Value: &policyv1alpha1.Value_StringValue{StringValue: "restricted"},
					},
					Upsert: proto.Bool(true),
				},
			},
		},
		{
			Id: "redact-record-field-severity-text",
			Action: &policyv1alpha1.LogTransformPolicy_Redact{
				Redact: &policyv1alpha1.LogRedactAction{
					Target: &policyv1alpha1.LogTargetSelector{
						Target: &policyv1alpha1.LogTargetSelector_RecordField{
							RecordField: policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SEVERITY_TEXT,
						},
					},
					Replacement: proto.String("REDACTED"),
				},
			},
		},
	}

	for _, p := range policies {
		t.Run(p.GetId(), func(t *testing.T) {
			data, err := proto.Marshal(p)
			require.NoError(t, err)
			out := &policyv1alpha1.LogTransformPolicy{}
			require.NoError(t, proto.Unmarshal(data, out))
			assert.True(t, proto.Equal(p, out))

			jsonData, err := protojson.Marshal(p)
			require.NoError(t, err)
			jsonOut := &policyv1alpha1.LogTransformPolicy{}
			require.NoError(t, protojson.Unmarshal(jsonData, jsonOut))
			assert.True(t, proto.Equal(p, jsonOut))
		})
	}
}
