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
	"google.golang.org/protobuf/types/known/emptypb"

	policyv1alpha1 "github.com/GoogleCloudPlatform/opentelemetry-operations-collector/gen/go/policy/v1alpha1"
	xdsv1alpha1 "github.com/GoogleCloudPlatform/opentelemetry-operations-collector/gen/go/xds/v1alpha1"
)

func TestTraceSamplingPolicy_Serialization(t *testing.T) {
	policy := &policyv1alpha1.TraceSamplingPolicy{
		Id:              "sample-traces-10pct",
		SamplingPercent: 10.0,
	}

	data, err := proto.Marshal(policy)
	require.NoError(t, err)

	unmarshaled := &policyv1alpha1.TraceSamplingPolicy{}
	require.NoError(t, proto.Unmarshal(data, unmarshaled))

	assert.True(t, proto.Equal(policy, unmarshaled))
	assert.Equal(t, "sample-traces-10pct", unmarshaled.GetId())
	assert.InDelta(t, 10.0, unmarshaled.GetSamplingPercent(), 1e-9)
	assert.Empty(t, unmarshaled.GetMatchers())
}

func TestTraceSamplingPolicy_WithMatchers(t *testing.T) {
	policy := &policyv1alpha1.TraceSamplingPolicy{
		Id:              "sample-checkout-db-spans-5pct",
		SamplingPercent: 5.0,
		Matchers: []*policyv1alpha1.TraceMatcher{
			{
				Target: &policyv1alpha1.TraceFieldSelector{
					Target: &policyv1alpha1.TraceFieldSelector_ResourceAttributePath{
						ResourceAttributePath: &policyv1alpha1.AttributePath{
							Segments: []string{"service.name"},
						},
					},
				},
				Predicate: &policyv1alpha1.TraceMatcher_Equality{
					Equality: &policyv1alpha1.Value{
						Kind: &policyv1alpha1.Value_StringValue{
							StringValue: "checkout",
						},
					},
				},
			},
			{
				Target: &policyv1alpha1.TraceFieldSelector{
					Target: &policyv1alpha1.TraceFieldSelector_RecordField{
						RecordField: policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_KIND,
					},
				},
				Predicate: &policyv1alpha1.TraceMatcher_Equality{
					Equality: &policyv1alpha1.Value{
						Kind: &policyv1alpha1.Value_StringValue{
							StringValue: "CLIENT",
						},
					},
				},
			},
			{
				Target: &policyv1alpha1.TraceFieldSelector{
					Target: &policyv1alpha1.TraceFieldSelector_SpanAttributePath{
						SpanAttributePath: &policyv1alpha1.AttributePath{
							Segments: []string{"db.system.name"},
						},
					},
				},
				Predicate: &policyv1alpha1.TraceMatcher_Existence{
					Existence: &emptypb.Empty{},
				},
			},
			{
				Target: &policyv1alpha1.TraceFieldSelector{
					Target: &policyv1alpha1.TraceFieldSelector_ScopeField{
						ScopeField: policyv1alpha1.ScopeField_SCOPE_FIELD_NAME,
					},
				},
				Predicate: &policyv1alpha1.TraceMatcher_Glob{
					Glob: "go.opentelemetry.io/contrib/instrumentation/*",
				},
			},
		},
	}

	data, err := proto.Marshal(policy)
	require.NoError(t, err)

	unmarshaled := &policyv1alpha1.TraceSamplingPolicy{}
	require.NoError(t, proto.Unmarshal(data, unmarshaled))

	assert.True(t, proto.Equal(policy, unmarshaled))
	assert.Equal(t, "sample-checkout-db-spans-5pct", unmarshaled.GetId())
	assert.InDelta(t, 5.0, unmarshaled.GetSamplingPercent(), 1e-9)
	require.Len(t, unmarshaled.GetMatchers(), 4)

	jsonData, err := protojson.Marshal(policy)
	require.NoError(t, err)
	assert.Contains(t, string(jsonData), `"matchers":`)

	unmarshaledJSON := &policyv1alpha1.TraceSamplingPolicy{}
	require.NoError(t, protojson.Unmarshal(jsonData, unmarshaledJSON))
	assert.True(t, proto.Equal(policy, unmarshaledJSON))
}

func TestTraceSamplingPolicy_AnyPackaging(t *testing.T) {
	policy := &policyv1alpha1.TraceSamplingPolicy{
		Id:              "sample-traces-25pct",
		SamplingPercent: 25.5,
	}

	anyPolicy, err := anypb.New(policy)
	require.NoError(t, err)
	assert.Equal(t, "type.googleapis.com/google.telemetry.policy.v1alpha1.TraceSamplingPolicy", anyPolicy.GetTypeUrl())

	collector := &xdsv1alpha1.TelemetryCollector{
		Policies: []*anypb.Any{anyPolicy},
	}

	data, err := proto.Marshal(collector)
	require.NoError(t, err)

	unmarshaledCollector := &xdsv1alpha1.TelemetryCollector{}
	require.NoError(t, proto.Unmarshal(data, unmarshaledCollector))

	require.Len(t, unmarshaledCollector.GetPolicies(), 1)
	assert.Equal(t, "type.googleapis.com/google.telemetry.policy.v1alpha1.TraceSamplingPolicy", unmarshaledCollector.GetPolicies()[0].GetTypeUrl())

	unpackedPolicy := &policyv1alpha1.TraceSamplingPolicy{}
	require.NoError(t, unmarshaledCollector.GetPolicies()[0].UnmarshalTo(unpackedPolicy))
	assert.True(t, proto.Equal(policy, unpackedPolicy))
	assert.Equal(t, "sample-traces-25pct", unpackedPolicy.GetId())
	assert.InDelta(t, 25.5, unpackedPolicy.GetSamplingPercent(), 1e-9)
}

func TestTraceSamplingPolicy_JSONSerialization(t *testing.T) {
	policy := &policyv1alpha1.TraceSamplingPolicy{
		Id:              "sample-traces-fractional",
		SamplingPercent: 0.05,
	}

	// ProtoJSON marshal and unmarshal round-trip.
	jsonData, err := protojson.Marshal(policy)
	require.NoError(t, err)
	assert.Contains(t, string(jsonData), `"samplingPercent":0.05`)

	unmarshaled := &policyv1alpha1.TraceSamplingPolicy{}
	require.NoError(t, protojson.Unmarshal(jsonData, unmarshaled))
	assert.True(t, proto.Equal(policy, unmarshaled))
	assert.Equal(t, "sample-traces-fractional", unmarshaled.GetId())
	assert.InDelta(t, 0.05, unmarshaled.GetSamplingPercent(), 1e-9)

	// Snake_case field name in JSON is also accepted by protojson.
	jsonSnakeCase := []byte(`{"id": "sample-traces-100pct", "sampling_percent": 100.0}`)
	snakePolicy := &policyv1alpha1.TraceSamplingPolicy{}
	require.NoError(t, protojson.Unmarshal(jsonSnakeCase, snakePolicy))
	assert.Equal(t, "sample-traces-100pct", snakePolicy.GetId())
	assert.InDelta(t, 100.0, snakePolicy.GetSamplingPercent(), 1e-9)

	// Empty / default message round-trip.
	emptyJSON := []byte(`{"id": "omitted-percentage-policy"}`)
	emptyPolicy := &policyv1alpha1.TraceSamplingPolicy{}
	require.NoError(t, protojson.Unmarshal(emptyJSON, emptyPolicy))
	assert.Equal(t, "omitted-percentage-policy", emptyPolicy.GetId())
	assert.Equal(t, 0.0, emptyPolicy.GetSamplingPercent())

	// EmitUnpopulated includes zero-valued samplingPercent in output.
	marshaledUnpopulated, err := protojson.MarshalOptions{EmitUnpopulated: true}.Marshal(emptyPolicy)
	require.NoError(t, err)
	assert.Contains(t, string(marshaledUnpopulated), `"samplingPercent":0`)
}
