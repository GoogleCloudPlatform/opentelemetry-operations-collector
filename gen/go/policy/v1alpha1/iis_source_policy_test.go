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
	xdsv1alpha1 "github.com/GoogleCloudPlatform/opentelemetry-operations-collector/gen/go/xds/v1alpha1"
)

func TestIISSourcePolicy_Serialization(t *testing.T) {
	testCases := []struct {
		name     string
		policy   *policyv1alpha1.IISSourcePolicy
		validate func(t *testing.T, p *policyv1alpha1.IISSourcePolicy)
	}{
		{
			name: "with_collection_interval",
			policy: &policyv1alpha1.IISSourcePolicy{
				Id:                        "enable-iis-metrics",
				CollectionIntervalSeconds: proto.Int32(30),
			},
			validate: func(t *testing.T, p *policyv1alpha1.IISSourcePolicy) {
				assert.Equal(t, "enable-iis-metrics", p.GetId())
				require.NotNil(t, p.CollectionIntervalSeconds)
				assert.Equal(t, int32(30), p.GetCollectionIntervalSeconds())
			},
		},
		{
			name: "default_unset_collection_interval",
			policy: &policyv1alpha1.IISSourcePolicy{
				Id: "enable-iis-default-interval",
			},
			validate: func(t *testing.T, p *policyv1alpha1.IISSourcePolicy) {
				assert.Equal(t, "enable-iis-default-interval", p.GetId())
				assert.Nil(t, p.CollectionIntervalSeconds)
				assert.Equal(t, int32(0), p.GetCollectionIntervalSeconds())
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			data, err := proto.Marshal(tc.policy)
			require.NoError(t, err)

			unmarshaled := &policyv1alpha1.IISSourcePolicy{}
			require.NoError(t, proto.Unmarshal(data, unmarshaled))

			assert.True(t, proto.Equal(tc.policy, unmarshaled))
			tc.validate(t, unmarshaled)
		})
	}
}

func TestIISSourcePolicy_AnyPackaging(t *testing.T) {
	policy := &policyv1alpha1.IISSourcePolicy{
		Id:                        "iis-source-policy",
		CollectionIntervalSeconds: proto.Int32(60),
	}

	anyPolicy, err := anypb.New(policy)
	require.NoError(t, err)
	assert.Equal(t, "type.googleapis.com/google.telemetry.policy.v1alpha1.IISSourcePolicy", anyPolicy.GetTypeUrl())

	collector := &xdsv1alpha1.TelemetryCollector{
		Policies: []*anypb.Any{anyPolicy},
	}

	data, err := proto.Marshal(collector)
	require.NoError(t, err)

	unmarshaled := &xdsv1alpha1.TelemetryCollector{}
	require.NoError(t, proto.Unmarshal(data, unmarshaled))

	require.Len(t, unmarshaled.GetPolicies(), 1)
	assert.Equal(t, "type.googleapis.com/google.telemetry.policy.v1alpha1.IISSourcePolicy", unmarshaled.GetPolicies()[0].GetTypeUrl())

	unpackedPolicy := &policyv1alpha1.IISSourcePolicy{}
	require.NoError(t, unmarshaled.GetPolicies()[0].UnmarshalTo(unpackedPolicy))
	assert.Equal(t, "iis-source-policy", unpackedPolicy.GetId())
	require.NotNil(t, unpackedPolicy.CollectionIntervalSeconds)
	assert.Equal(t, int32(60), unpackedPolicy.GetCollectionIntervalSeconds())
}

func TestIISSourcePolicy_JSONSerialization(t *testing.T) {
	policy := &policyv1alpha1.IISSourcePolicy{
		Id:                        "enable-iis-metrics",
		CollectionIntervalSeconds: proto.Int32(30),
	}

	// ProtoJSON marshal and unmarshal round-trip.
	jsonData, err := protojson.Marshal(policy)
	require.NoError(t, err)

	unmarshaled := &policyv1alpha1.IISSourcePolicy{}
	require.NoError(t, protojson.Unmarshal(jsonData, unmarshaled))
	assert.True(t, proto.Equal(policy, unmarshaled))
	require.NotNil(t, unmarshaled.CollectionIntervalSeconds)
	assert.Equal(t, int32(30), unmarshaled.GetCollectionIntervalSeconds())

	// Omitted collection_interval_seconds preserves nil pointer (distinguishing unset from explicit 0).
	jsonOmittedInterval := []byte(`{"id": "omitted-interval-policy"}`)
	omittedPolicy := &policyv1alpha1.IISSourcePolicy{}
	require.NoError(t, protojson.Unmarshal(jsonOmittedInterval, omittedPolicy))
	assert.Equal(t, "omitted-interval-policy", omittedPolicy.GetId())
	assert.Nil(t, omittedPolicy.CollectionIntervalSeconds)
	assert.Equal(t, int32(0), omittedPolicy.GetCollectionIntervalSeconds())

	// Explicit zero collection_interval_seconds preserves non-nil pointer so validators can reject < 10.
	jsonZeroInterval := []byte(`{"id": "zero-interval-policy", "collectionIntervalSeconds": 0}`)
	zeroPolicy := &policyv1alpha1.IISSourcePolicy{}
	require.NoError(t, protojson.Unmarshal(jsonZeroInterval, zeroPolicy))
	assert.Equal(t, "zero-interval-policy", zeroPolicy.GetId())
	require.NotNil(t, zeroPolicy.CollectionIntervalSeconds)
	assert.Equal(t, int32(0), zeroPolicy.GetCollectionIntervalSeconds())

	// EmitUnpopulated includes explicit 0 when set.
	marshaledUnpopulated, err := protojson.MarshalOptions{EmitUnpopulated: true}.Marshal(zeroPolicy)
	require.NoError(t, err)
	assert.Contains(t, string(marshaledUnpopulated), `"collectionIntervalSeconds":0`)
}
