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

package policyv1_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"

	policyv1 "github.com/GoogleCloudPlatform/opentelemetry-operations-collector/gen/go/policy/v1"
)

func TestLogTransformPolicy_Serialization(t *testing.T) {
	policy := &policyv1.LogTransformPolicy{
		Id: "label-soc-logs",
		Matchers: []*policyv1.LogMatcher{
			{
				Target: &policyv1.LogFieldSelector{
					Target: &policyv1.LogFieldSelector_BodyFieldPath{
						BodyFieldPath: &policyv1.AttributePath{
							Segments: []string{"event_id"},
						},
					},
				},
				Predicate: &policyv1.Predicate{
					Kind: &policyv1.Predicate_Equality{
						Equality: &policyv1.Value{
							Kind: &policyv1.Value_IntValue{
								IntValue: 4625,
							},
						},
					},
				},
			},
		},
		Renamings: []*policyv1.LogRenaming{
			{
				Source: &policyv1.LogAttributeSelector{
					Target: &policyv1.LogAttributeSelector_LogAttributePath{
						LogAttributePath: &policyv1.AttributePath{
							Segments: []string{"legacy_user"},
						},
					},
				},
				Target: &policyv1.LogAttributeSelector{
					Target: &policyv1.LogAttributeSelector_LogAttributePath{
						LogAttributePath: &policyv1.AttributePath{
							Segments: []string{"user.name"},
						},
					},
				},
				UpsertEnabled: proto.Bool(false),
			},
		},
		Additions: []*policyv1.LogAddition{
			{
				Target: &policyv1.LogTargetSelector{
					Target: &policyv1.LogTargetSelector_LogAttributePath{
						LogAttributePath: &policyv1.AttributePath{
							Segments: []string{"threat_tier"},
						},
					},
				},
				Source: &policyv1.LogAddition_Value{
					Value: &policyv1.Value{
						Kind: &policyv1.Value_StringValue{
							StringValue: "HOT_ACTIVE_SOC",
						},
					},
				},
				UpsertEnabled: proto.Bool(true),
			},
			{
				Target: &policyv1.LogTargetSelector{
					Target: &policyv1.LogTargetSelector_LogAttributePath{
						LogAttributePath: &policyv1.AttributePath{
							Segments: []string{"soc_owner"},
						},
					},
				},
				Source: &policyv1.LogAddition_Value{
					Value: &policyv1.Value{
						Kind: &policyv1.Value_StringValue{
							StringValue: "secops",
						},
					},
				},
				UpsertEnabled: proto.Bool(false),
			},
		},
		Removals: []*policyv1.LogRemoval{
			{
				Target: &policyv1.LogAttributeSelector{
					Target: &policyv1.LogAttributeSelector_LogAttributePath{
						LogAttributePath: &policyv1.AttributePath{
							Segments: []string{"debug_scratch"},
						},
					},
				},
			},
		},
		Redactions: []*policyv1.LogRedaction{
			{
				Target: &policyv1.LogTargetSelector{
					Target: &policyv1.LogTargetSelector_LogAttributePath{
						LogAttributePath: &policyv1.AttributePath{
							Segments: []string{"user.name"},
						},
					},
				},
				Pattern: &policyv1.LogRedaction_Regex{
					Regex: `@.+`,
				},
				Mask: &policyv1.LogRedaction_Replacement{
					Replacement: "@[REDACTED]",
				},
			},
		},
	}

	// Verify Any packaging.
	anyPolicy, err := anypb.New(policy)
	require.NoError(t, err)
	assert.Equal(t, "type.googleapis.com/google.telemetry.policy.v1.LogTransformPolicy", anyPolicy.GetTypeUrl())

	// Round-trip binary serialization.
	data, err := proto.Marshal(policy)
	require.NoError(t, err)

	unmarshaled := &policyv1.LogTransformPolicy{}
	require.NoError(t, proto.Unmarshal(data, unmarshaled))

	assert.True(t, proto.Equal(policy, unmarshaled))
	assert.Equal(t, "label-soc-logs", unmarshaled.GetId())
	require.Len(t, unmarshaled.GetMatchers(), 1)
	require.Len(t, unmarshaled.GetRenamings(), 1)
	require.Len(t, unmarshaled.GetAdditions(), 2)
	require.Len(t, unmarshaled.GetRemovals(), 1)
	require.Len(t, unmarshaled.GetRedactions(), 1)
	require.NotNil(t, unmarshaled.GetAdditions()[0].UpsertEnabled)
	assert.True(t, unmarshaled.GetAdditions()[0].GetUpsertEnabled())
	require.NotNil(t, unmarshaled.GetAdditions()[1].UpsertEnabled)
	assert.False(t, unmarshaled.GetAdditions()[1].GetUpsertEnabled())
}

func TestLogTransformPolicy_JSONSerialization(t *testing.T) {
	policy := &policyv1.LogTransformPolicy{
		Id: "add-utr-routing-label",
		Matchers: []*policyv1.LogMatcher{
			{
				Target: &policyv1.LogFieldSelector{
					Target: &policyv1.LogFieldSelector_ResourceAttributePath{
						ResourceAttributePath: &policyv1.AttributePath{
							Segments: []string{"service.name"},
						},
					},
				},
				Predicate: &policyv1.Predicate{
					Kind: &policyv1.Predicate_Equality{
						Equality: &policyv1.Value{
							Kind: &policyv1.Value_StringValue{
								StringValue: "active-directory",
							},
						},
					},
				},
			},
			{
				Target: &policyv1.LogFieldSelector{
					Target: &policyv1.LogFieldSelector_BodyFieldPath{
						BodyFieldPath: &policyv1.AttributePath{
							Segments: []string{"event_data", "TargetUserName"},
						},
					},
				},
				Predicate: &policyv1.Predicate{
					Kind: &policyv1.Predicate_Glob{
						Glob: "*$",
					},
				},
			},
		},
		Additions: []*policyv1.LogAddition{
			{
				Target: &policyv1.LogTargetSelector{
					Target: &policyv1.LogTargetSelector_LogAttributePath{
						LogAttributePath: &policyv1.AttributePath{
							Segments: []string{"threat_tier"},
						},
					},
				},
				Source: &policyv1.LogAddition_Value{
					Value: &policyv1.Value{
						Kind: &policyv1.Value_StringValue{
							StringValue: "LAKEHOUSE_ARCHIVE",
						},
					},
				},
				UpsertEnabled: proto.Bool(true),
			},
		},
	}

	jsonData, err := protojson.Marshal(policy)
	require.NoError(t, err)

	unmarshaled := &policyv1.LogTransformPolicy{}
	require.NoError(t, protojson.Unmarshal(jsonData, unmarshaled))
	assert.True(t, proto.Equal(policy, unmarshaled))
}

func TestLogTransformPolicy_AllStageListsAndUnconditionalMatchers(t *testing.T) {
	variants := []*policyv1.LogTransformPolicy{
		{
			// Unconditional removal operation (empty matchers).
			Id: "remove-local-scriptpath",
			Removals: []*policyv1.LogRemoval{
				{
					Target: &policyv1.LogAttributeSelector{
						Target: &policyv1.LogAttributeSelector_LogAttributePath{
							LogAttributePath: &policyv1.AttributePath{Segments: []string{"LOCAL_SCRIPTPATH_V1"}},
						},
					},
				},
			},
		},
		{
			// Renaming operation from body_field_path to log_attribute_path.
			Id: "rename-legacy-user-id",
			Renamings: []*policyv1.LogRenaming{
				{
					Source: &policyv1.LogAttributeSelector{
						Target: &policyv1.LogAttributeSelector_BodyFieldPath{
							BodyFieldPath: &policyv1.AttributePath{Segments: []string{"event_data", "userId"}},
						},
					},
					Target: &policyv1.LogAttributeSelector{
						Target: &policyv1.LogAttributeSelector_LogAttributePath{
							LogAttributePath: &policyv1.AttributePath{Segments: []string{"user.id"}},
						},
					},
					UpsertEnabled: proto.Bool(false),
				},
			},
		},
		{
			// Unconditional redaction operation (empty matchers).
			Id: "redact-ssn",
			Redactions: []*policyv1.LogRedaction{
				{
					Target: &policyv1.LogTargetSelector{
						Target: &policyv1.LogTargetSelector_RecordField{
							RecordField: policyv1.LogRecordField_LOG_RECORD_FIELD_BODY,
						},
					},
					Pattern: &policyv1.LogRedaction_Regex{
						Regex: `\b\d{3}-\d{2}-\d{4}\b`,
					},
					Mask: &policyv1.LogRedaction_Replacement{
						Replacement: "[REDACTED_SSN]",
					},
				},
			},
		},
	}

	for _, p := range variants {
		t.Run(p.GetId(), func(t *testing.T) {
			data, err := proto.Marshal(p)
			require.NoError(t, err)
			out := &policyv1.LogTransformPolicy{}
			require.NoError(t, proto.Unmarshal(data, out))
			assert.True(t, proto.Equal(p, out))

			jsonData, err := protojson.Marshal(p)
			require.NoError(t, err)
			jsonOut := &policyv1.LogTransformPolicy{}
			require.NoError(t, protojson.Unmarshal(jsonData, jsonOut))
			assert.True(t, proto.Equal(p, jsonOut))
		})
	}
}

func TestLogTransformPolicy_OptionalAndOneofFieldsPresence(t *testing.T) {
	// 1. Omitted pattern/mask oneof fields remain nil while getters return default zero values.
	omittedJSON := []byte(`{
		"id": "redact-entire-attribute",
		"redactions": [
			{
				"target": {
					"logAttributePath": {"segments": ["secret_token"]}
				}
			}
		]
	}`)
	omittedPolicy := &policyv1.LogTransformPolicy{}
	require.NoError(t, protojson.Unmarshal(omittedJSON, omittedPolicy))
	require.Len(t, omittedPolicy.GetRedactions(), 1)
	assert.Nil(t, omittedPolicy.GetRedactions()[0].GetPattern())
	assert.Equal(t, "", omittedPolicy.GetRedactions()[0].GetRegex())
	assert.Nil(t, omittedPolicy.GetRedactions()[0].GetMask())
	assert.Equal(t, "", omittedPolicy.GetRedactions()[0].GetReplacement())

	// 2. Explicit zero values (upsertEnabled: false, regex: "", replacement: "") preserve presence.
	explicitZeroJSON := []byte(`{
		"id": "add-and-redact-explicit-zero",
		"additions": [
			{
				"target": {"logAttributePath": {"segments": ["tier"]}},
				"value": {"stringValue": "default"},
				"upsertEnabled": false
			}
		],
		"redactions": [
			{
				"target": {"logAttributePath": {"segments": ["secret"]}},
				"regex": "",
				"replacement": ""
			}
		]
	}`)
	explicitZeroPolicy := &policyv1.LogTransformPolicy{}
	require.NoError(t, protojson.Unmarshal(explicitZeroJSON, explicitZeroPolicy))
	require.Len(t, explicitZeroPolicy.GetAdditions(), 1)
	require.NotNil(t, explicitZeroPolicy.GetAdditions()[0].UpsertEnabled)
	assert.False(t, explicitZeroPolicy.GetAdditions()[0].GetUpsertEnabled())
	require.Len(t, explicitZeroPolicy.GetRedactions(), 1)
	require.IsType(t, &policyv1.LogRedaction_Regex{}, explicitZeroPolicy.GetRedactions()[0].GetPattern())
	require.IsType(t, &policyv1.LogRedaction_Replacement{}, explicitZeroPolicy.GetRedactions()[0].GetMask())
}

func TestLogTransformPolicy_RecordFieldAndNestedPathTargets(t *testing.T) {
	policies := []*policyv1.LogTransformPolicy{
		{
			Id: "add-record-field-body-fallback",
			Additions: []*policyv1.LogAddition{
				{
					Target: &policyv1.LogTargetSelector{
						Target: &policyv1.LogTargetSelector_RecordField{
							RecordField: policyv1.LogRecordField_LOG_RECORD_FIELD_BODY,
						},
					},
					Source: &policyv1.LogAddition_Value{
						Value: &policyv1.Value{
							Kind: &policyv1.Value_StringValue{StringValue: "default-body"},
						},
					},
					UpsertEnabled: proto.Bool(false),
				},
			},
		},
		{
			Id: "add-record-field-severity-text",
			Additions: []*policyv1.LogAddition{
				{
					Target: &policyv1.LogTargetSelector{
						Target: &policyv1.LogTargetSelector_RecordField{
							RecordField: policyv1.LogRecordField_LOG_RECORD_FIELD_SEVERITY_TEXT,
						},
					},
					Source: &policyv1.LogAddition_Value{
						Value: &policyv1.Value{
							Kind: &policyv1.Value_StringValue{StringValue: "WARN"},
						},
					},
					UpsertEnabled: proto.Bool(true),
				},
			},
		},
		{
			Id: "add-record-field-severity-number",
			Additions: []*policyv1.LogAddition{
				{
					Target: &policyv1.LogTargetSelector{
						Target: &policyv1.LogTargetSelector_RecordField{
							RecordField: policyv1.LogRecordField_LOG_RECORD_FIELD_SEVERITY_NUMBER,
						},
					},
					Source: &policyv1.LogAddition_Value{
						Value: &policyv1.Value{
							Kind: &policyv1.Value_IntValue{IntValue: 13},
						},
					},
					UpsertEnabled: proto.Bool(true),
				},
			},
		},
		{
			Id: "add-nested-body-field-upsert",
			Additions: []*policyv1.LogAddition{
				{
					Target: &policyv1.LogTargetSelector{
						Target: &policyv1.LogTargetSelector_BodyFieldPath{
							BodyFieldPath: &policyv1.AttributePath{
								Segments: []string{"event_data", "security", "classification"},
							},
						},
					},
					Source: &policyv1.LogAddition_Value{
						Value: &policyv1.Value{
							Kind: &policyv1.Value_StringValue{StringValue: "restricted"},
						},
					},
					UpsertEnabled: proto.Bool(true),
				},
			},
		},
		{
			Id: "redact-record-field-severity-text",
			Redactions: []*policyv1.LogRedaction{
				{
					Target: &policyv1.LogTargetSelector{
						Target: &policyv1.LogTargetSelector_RecordField{
							RecordField: policyv1.LogRecordField_LOG_RECORD_FIELD_SEVERITY_TEXT,
						},
					},
					Mask: &policyv1.LogRedaction_Replacement{
						Replacement: "REDACTED",
					},
				},
			},
		},
	}

	for _, p := range policies {
		t.Run(p.GetId(), func(t *testing.T) {
			data, err := proto.Marshal(p)
			require.NoError(t, err)
			out := &policyv1.LogTransformPolicy{}
			require.NoError(t, proto.Unmarshal(data, out))
			assert.True(t, proto.Equal(p, out))

			jsonData, err := protojson.Marshal(p)
			require.NoError(t, err)
			jsonOut := &policyv1.LogTransformPolicy{}
			require.NoError(t, protojson.Unmarshal(jsonData, jsonOut))
			assert.True(t, proto.Equal(p, jsonOut))
		})
	}
}
