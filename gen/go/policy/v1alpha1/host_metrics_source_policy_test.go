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

func TestHostMetricsSourcePolicy_Serialization(t *testing.T) {
	interval := int32(30)
	policy := &policyv1alpha1.HostMetricsSourcePolicy{
		Id:                        "host-metrics-30s",
		CollectionIntervalSeconds: &interval,
	}

	// Verify Any packaging.
	anyPolicy, err := anypb.New(policy)
	require.NoError(t, err)
	assert.Equal(t, "type.googleapis.com/google.telemetry.policy.v1alpha1.HostMetricsSourcePolicy", anyPolicy.GetTypeUrl())

	unpacked := &policyv1alpha1.HostMetricsSourcePolicy{}
	require.NoError(t, anyPolicy.UnmarshalTo(unpacked))
	assert.True(t, proto.Equal(policy, unpacked))

	// Round-trip wire serialization.
	data, err := proto.Marshal(policy)
	require.NoError(t, err)

	unmarshaled := &policyv1alpha1.HostMetricsSourcePolicy{}
	require.NoError(t, proto.Unmarshal(data, unmarshaled))

	assert.True(t, proto.Equal(policy, unmarshaled))
	assert.Equal(t, "host-metrics-30s", unmarshaled.GetId())
	require.NotNil(t, unmarshaled.CollectionIntervalSeconds)
	assert.Equal(t, int32(30), unmarshaled.GetCollectionIntervalSeconds())
}

func TestHostMetricsSourcePolicy_OptionalIntervalPresence(t *testing.T) {
	// Unset collection_interval_seconds preserves nil pointer presence.
	unsetPolicy := &policyv1alpha1.HostMetricsSourcePolicy{
		Id: "host-metrics-default-interval",
	}

	data, err := proto.Marshal(unsetPolicy)
	require.NoError(t, err)

	unmarshaledUnset := &policyv1alpha1.HostMetricsSourcePolicy{}
	require.NoError(t, proto.Unmarshal(data, unmarshaledUnset))
	assert.True(t, proto.Equal(unsetPolicy, unmarshaledUnset))
	assert.Equal(t, "host-metrics-default-interval", unmarshaledUnset.GetId())
	assert.Nil(t, unmarshaledUnset.CollectionIntervalSeconds)
	assert.Equal(t, int32(0), unmarshaledUnset.GetCollectionIntervalSeconds())

	// Explicit zero collection_interval_seconds preserves non-nil pointer presence
	// so validators can distinguish an unset interval (defaults to 60s) from an
	// invalid explicit 0s interval.
	zero := int32(0)
	zeroPolicy := &policyv1alpha1.HostMetricsSourcePolicy{
		Id:                        "host-metrics-zero-interval",
		CollectionIntervalSeconds: &zero,
	}

	zeroData, err := proto.Marshal(zeroPolicy)
	require.NoError(t, err)

	unmarshaledZero := &policyv1alpha1.HostMetricsSourcePolicy{}
	require.NoError(t, proto.Unmarshal(zeroData, unmarshaledZero))
	assert.True(t, proto.Equal(zeroPolicy, unmarshaledZero))
	require.NotNil(t, unmarshaledZero.CollectionIntervalSeconds)
	assert.Equal(t, int32(0), unmarshaledZero.GetCollectionIntervalSeconds())
}

func TestHostMetricsSourcePolicy_JSONSerialization(t *testing.T) {
	interval := int32(60)
	policy := &policyv1alpha1.HostMetricsSourcePolicy{
		Id:                        "host-metrics-60s",
		CollectionIntervalSeconds: &interval,
	}

	// ProtoJSON marshal and unmarshal round-trip.
	jsonData, err := protojson.Marshal(policy)
	require.NoError(t, err)

	unmarshaled := &policyv1alpha1.HostMetricsSourcePolicy{}
	require.NoError(t, protojson.Unmarshal(jsonData, unmarshaled))
	assert.True(t, proto.Equal(policy, unmarshaled))
	assert.Equal(t, "host-metrics-60s", unmarshaled.GetId())
	require.NotNil(t, unmarshaled.CollectionIntervalSeconds)
	assert.Equal(t, int32(60), unmarshaled.GetCollectionIntervalSeconds())

	// Explicit zero in JSON preserves non-nil pointer presence.
	jsonWithZero := []byte(`{"id": "zero-interval-policy", "collectionIntervalSeconds": 0}`)
	zeroPolicy := &policyv1alpha1.HostMetricsSourcePolicy{}
	require.NoError(t, protojson.Unmarshal(jsonWithZero, zeroPolicy))
	assert.Equal(t, "zero-interval-policy", zeroPolicy.GetId())
	require.NotNil(t, zeroPolicy.CollectionIntervalSeconds)
	assert.Equal(t, int32(0), zeroPolicy.GetCollectionIntervalSeconds())

	// Snake_case field name in JSON is also accepted by protojson.
	jsonSnakeCase := []byte(`{"id": "snake-case-policy", "collection_interval_seconds": 15}`)
	snakePolicy := &policyv1alpha1.HostMetricsSourcePolicy{}
	require.NoError(t, protojson.Unmarshal(jsonSnakeCase, snakePolicy))
	assert.Equal(t, "snake-case-policy", snakePolicy.GetId())
	require.NotNil(t, snakePolicy.CollectionIntervalSeconds)
	assert.Equal(t, int32(15), snakePolicy.GetCollectionIntervalSeconds())

	// Omitted collection_interval_seconds in JSON leaves pointer nil.
	jsonOmittedInterval := []byte(`{"id": "omitted-interval-policy"}`)
	omittedPolicy := &policyv1alpha1.HostMetricsSourcePolicy{}
	require.NoError(t, protojson.Unmarshal(jsonOmittedInterval, omittedPolicy))
	assert.Equal(t, "omitted-interval-policy", omittedPolicy.GetId())
	assert.Nil(t, omittedPolicy.CollectionIntervalSeconds)
	assert.Equal(t, int32(0), omittedPolicy.GetCollectionIntervalSeconds())

	// EmitUnpopulated omits unset optional scalar or emits explicit 0 when set.
	marshaledUnpopulated, err := protojson.MarshalOptions{EmitUnpopulated: true}.Marshal(zeroPolicy)
	require.NoError(t, err)
	assert.Contains(t, string(marshaledUnpopulated), `"collectionIntervalSeconds":0`)
}
