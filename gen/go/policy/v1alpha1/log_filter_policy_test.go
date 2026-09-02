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
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/emptypb"

	policyv1alpha1 "github.com/GoogleCloudPlatform/opentelemetry-operations-collector/gen/go/policy/v1alpha1"
)

func TestLogFilterPolicy_Serialization(t *testing.T) {
	policy := &policyv1alpha1.LogFilterPolicy{
		Id:     "drop-noisy-healthchecks",
		Action: policyv1alpha1.Action_ACTION_DROP,
		Matches: []*policyv1alpha1.LogMatcher{
			{
				Target: &policyv1alpha1.LogFieldSelector{
					Target: &policyv1alpha1.LogFieldSelector_RecordField{
						RecordField: policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_BODY,
					},
				},
				Predicate: &policyv1alpha1.LogMatcher_Regex{
					Regex: "^healthcheck.*",
				},
				Negate: false,
			},
			{
				Target: &policyv1alpha1.LogFieldSelector{
					Target: &policyv1alpha1.LogFieldSelector_LogAttribute{
						LogAttribute: "http.route",
					},
				},
				Predicate: &policyv1alpha1.LogMatcher_Exact{
					Exact: "/healthz",
				},
			},
			{
				Target: &policyv1alpha1.LogFieldSelector{
					Target: &policyv1alpha1.LogFieldSelector_ResourceAttribute{
						ResourceAttribute: "service.name",
					},
				},
				Predicate: &policyv1alpha1.LogMatcher_Exists{
					Exists: &emptypb.Empty{},
				},
				Negate: true,
			},

			{
				Target: &policyv1alpha1.LogFieldSelector{
					Target: &policyv1alpha1.LogFieldSelector_ScopeAttribute{
						ScopeAttribute: "library.name",
					},
				},
				Predicate: &policyv1alpha1.LogMatcher_Exact{
					Exact: "my-library",
				},
			},
			{
				Target: &policyv1alpha1.LogFieldSelector{
					Target: &policyv1alpha1.LogFieldSelector_ScopeField{
						ScopeField: policyv1alpha1.ScopeField_SCOPE_FIELD_NAME,
					},
				},
				Predicate: &policyv1alpha1.LogMatcher_Exact{
					Exact: "my-scope",
				},
			},
			{
				Target: &policyv1alpha1.LogFieldSelector{
					Target: &policyv1alpha1.LogFieldSelector_ScopeField{
						ScopeField: policyv1alpha1.ScopeField_SCOPE_FIELD_VERSION,
					},
				},
				Predicate: &policyv1alpha1.LogMatcher_Exact{
					Exact: "v1.2.3",
				},
			},
			{
				Target: &policyv1alpha1.LogFieldSelector{
					Target: &policyv1alpha1.LogFieldSelector_ResourceField{
						ResourceField: policyv1alpha1.ResourceField_RESOURCE_FIELD_SCHEMA_URL,
					},
				},
				Predicate: &policyv1alpha1.LogMatcher_Exact{
					Exact: "https://opentelemetry.io/schemas/1.24.0",
				},
			},
			{
				Target: &policyv1alpha1.LogFieldSelector{
					Target: &policyv1alpha1.LogFieldSelector_ScopeField{
						ScopeField: policyv1alpha1.ScopeField_SCOPE_FIELD_SCHEMA_URL,
					},
				},
				Predicate: &policyv1alpha1.LogMatcher_Exact{
					Exact: "https://opentelemetry.io/schemas/1.24.0",
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
	assert.Equal(t, policyv1alpha1.Action_ACTION_DROP, unmarshaled.GetAction())
	require.Len(t, unmarshaled.GetMatches(), 8)
}
