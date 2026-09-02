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

package xdsv1alpha1_test

import (
	"testing"

	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"

	policyv1alpha1 "github.com/GoogleCloudPlatform/opentelemetry-operations-collector/gen/go/policy/v1alpha1"
	xdsv1alpha1 "github.com/GoogleCloudPlatform/opentelemetry-operations-collector/gen/go/xds/v1alpha1"
)

func TestTelemetryCollector_AnyPackaging(t *testing.T) {
	// Create pure policy (Tier 1).
	policy := &policyv1alpha1.LogFilterPolicy{}

	anyPolicy, err := anypb.New(policy)
	require.NoError(t, err)
	assert.Equal(t, "type.googleapis.com/google.telemetry.policy.v1alpha1.LogFilterPolicy", anyPolicy.GetTypeUrl())

	// Wrap inside TelemetryCollector (Tier 2).
	collector := &xdsv1alpha1.TelemetryCollector{
		Name: "test-collector-fleet",
		Policies: []*corev3.TypedExtensionConfig{
			{
				Name:        "default-log-filter",
				TypedConfig: anyPolicy,
			},
		},
	}

	// Verify serialization round-trip.
	data, err := proto.Marshal(collector)
	require.NoError(t, err)

	unmarshaled := &xdsv1alpha1.TelemetryCollector{}
	require.NoError(t, proto.Unmarshal(data, unmarshaled))

	assert.Equal(t, collector.GetName(), unmarshaled.GetName())
	require.Len(t, unmarshaled.GetPolicies(), 1)
	assert.Equal(t, "default-log-filter", unmarshaled.GetPolicies()[0].GetName())

	unpackedPolicy := &policyv1alpha1.LogFilterPolicy{}
	require.NoError(t, unmarshaled.GetPolicies()[0].GetTypedConfig().UnmarshalTo(unpackedPolicy))
}
