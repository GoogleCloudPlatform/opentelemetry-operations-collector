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

package tracesampling

import (
	"encoding/binary"
	"math"
	"testing"

	policyv1alpha1 "github.com/GoogleCloudPlatform/opentelemetry-operations-collector/gen/go/policy/v1alpha1"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"
)

func equalsString(s string) *policyv1alpha1.Predicate {
	return &policyv1alpha1.Predicate{Kind: &policyv1alpha1.Predicate_Equality{
		Equality: &policyv1alpha1.Value{Kind: &policyv1alpha1.Value_StringValue{StringValue: s}},
	}}
}

func equalsInt(v int64) *policyv1alpha1.Predicate {
	return &policyv1alpha1.Predicate{Kind: &policyv1alpha1.Predicate_Equality{
		Equality: &policyv1alpha1.Value{Kind: &policyv1alpha1.Value_IntValue{IntValue: v}},
	}}
}

func equalsBool(b bool) *policyv1alpha1.Predicate {
	return &policyv1alpha1.Predicate{Kind: &policyv1alpha1.Predicate_Equality{
		Equality: &policyv1alpha1.Value{Kind: &policyv1alpha1.Value_BoolValue{BoolValue: b}},
	}}
}

func TestNewPolicyFromProto_Validation(t *testing.T) {
	t.Parallel()

	// 1. Nil proto
	_, err := NewPolicyFromProto(nil)
	assert.ErrorIs(t, err, ErrNilProto)

	// 2. Empty ID
	_, err = NewPolicyFromProto(&policyv1alpha1.TraceSamplingPolicy{
		Id:              "",
		SamplingPercent: 50.0,
	})
	assert.ErrorIs(t, err, ErrMissingID)

	// 3. Invalid sampling percentages
	invalidPercents := []float64{0.0, -0.01, -50.0, 100.0001, 200.0, math.NaN(), math.Inf(1), math.Inf(-1)}
	for _, p := range invalidPercents {
		_, err = NewPolicyFromProto(&policyv1alpha1.TraceSamplingPolicy{
			Id:              "test-p",
			SamplingPercent: p,
		})
		assert.ErrorIs(t, err, ErrInvalidProbability, "expected ErrInvalidProbability for %v", p)
	}

	// 4. Valid policy
	pol, err := NewPolicyFromProto(&policyv1alpha1.TraceSamplingPolicy{
		Id:              "sample-50",
		SamplingPercent: 50.0,
	})
	require.NoError(t, err)
	assert.Equal(t, "sample-50", pol.PolicyName())
	assert.Equal(t, PolicyType, pol.PolicyType())
	assert.Equal(t, googlepolicy.PolicyClassTransformation, pol.PolicyClass())
	assert.Equal(t, []googlepolicy.Signal{googlepolicy.SignalTraces}, pol.TargetSignals())
	assert.Equal(t, 50.0, pol.SamplingPercent())
	assert.Equal(t, uint64(1)<<55, pol.Threshold())
	assert.NoError(t, pol.Validate())
}

func TestMatchesTrace(t *testing.T) {
	t.Parallel()

	// 1. Unconditional policy (empty matchers -> matches all spans)
	unconditional, err := NewPolicyFromProto(&policyv1alpha1.TraceSamplingPolicy{
		Id:              "sample-all",
		SamplingPercent: 10.0,
	})
	require.NoError(t, err)

	span := ptrace.NewSpan()
	ctx := googlepolicy.TraceContext{Span: span}
	assert.True(t, unconditional.MatchesTrace(ctx))

	// 2. Conditional policy targeting http.status_code == 500
	conditional, err := NewPolicyFromProto(&policyv1alpha1.TraceSamplingPolicy{
		Id:              "sample-500",
		SamplingPercent: 100.0,
		Matchers: []*policyv1alpha1.TraceMatcher{
			{
				Target: &policyv1alpha1.TraceFieldSelector{
					Target: &policyv1alpha1.TraceFieldSelector_SpanAttributePath{
						SpanAttributePath: &policyv1alpha1.AttributePath{
							Segments: []string{"http.status_code"},
						},
					},
				},
				Predicate: &policyv1alpha1.Predicate{
					Kind: &policyv1alpha1.Predicate_Equality{
						Equality: &policyv1alpha1.Value{
							Kind: &policyv1alpha1.Value_IntValue{
								IntValue: 500,
							},
						},
					},
				},
			},
		},
	})
	require.NoError(t, err)

	// Non-matching span
	span200 := ptrace.NewSpan()
	span200.Attributes().PutInt("http.status_code", 200)
	ctx200 := googlepolicy.TraceContext{Span: span200}
	assert.False(t, conditional.MatchesTrace(ctx200))

	// Matching span
	span500 := ptrace.NewSpan()
	span500.Attributes().PutInt("http.status_code", 500)
	ctx500 := googlepolicy.TraceContext{Span: span500}
	assert.True(t, conditional.MatchesTrace(ctx500))

	// 3. Multi-matcher policy (AND logic)
	multi, err := NewPolicyFromProto(&policyv1alpha1.TraceSamplingPolicy{
		Id:              "sample-cart-errors",
		SamplingPercent: 20.0,
		Matchers: []*policyv1alpha1.TraceMatcher{
			{
				Target: &policyv1alpha1.TraceFieldSelector{
					Target: &policyv1alpha1.TraceFieldSelector_RecordField{
						RecordField: policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_NAME,
					},
				},
				Predicate: &policyv1alpha1.Predicate{
					Kind: &policyv1alpha1.Predicate_Equality{
						Equality: &policyv1alpha1.Value{
							Kind: &policyv1alpha1.Value_StringValue{
								StringValue: "Checkout",
							},
						},
					},
				},
			},
			{
				Target: &policyv1alpha1.TraceFieldSelector{
					Target: &policyv1alpha1.TraceFieldSelector_SpanAttributePath{
						SpanAttributePath: &policyv1alpha1.AttributePath{
							Segments: []string{"error"},
						},
					},
				},
				Predicate: &policyv1alpha1.Predicate{
					Kind: &policyv1alpha1.Predicate_Equality{
						Equality: &policyv1alpha1.Value{
							Kind: &policyv1alpha1.Value_BoolValue{
								BoolValue: true,
							},
						},
					},
				},
			},
		},
	})
	require.NoError(t, err)

	// Only matches when BOTH name == "Checkout" AND error == true
	spanOnlyName := ptrace.NewSpan()
	spanOnlyName.SetName("Checkout")
	assert.False(t, multi.MatchesTrace(googlepolicy.TraceContext{Span: spanOnlyName}))

	spanBoth := ptrace.NewSpan()
	spanBoth.SetName("Checkout")
	spanBoth.Attributes().PutBool("error", true)
	assert.True(t, multi.MatchesTrace(googlepolicy.TraceContext{Span: spanBoth}))
}

func TestDriver_LoadPolicy(t *testing.T) {
	t.Parallel()

	// 1. Valid map configuration loaded through registry
	rawConfig := map[string]any{
		"type":             PolicyType,
		"id":               "test-driver-policy",
		"sampling_percent": 25.0,
	}
	p, err := googlepolicy.LoadPolicy(PolicyType, rawConfig)
	require.NoError(t, err)
	assert.Equal(t, "test-driver-policy", p.PolicyName())
	assert.Equal(t, PolicyType, p.PolicyType())
	assert.Equal(t, googlepolicy.PolicyClassTransformation, p.PolicyClass())

	// 2. Map with unknown fields (forward compatibility)
	rawWithUnknown := map[string]any{
		"type":             PolicyType,
		"id":               "test-future-policy",
		"sampling_percent": 10.0,
		"future_field":     "unknown_val",
	}
	p, err = googlepolicy.LoadPolicy(PolicyType, rawWithUnknown)
	require.NoError(t, err)
	assert.Equal(t, "test-future-policy", p.PolicyName())

	// 3. Invalid map configuration (missing ID)
	rawInvalid := map[string]any{
		"type":             PolicyType,
		"id":               "",
		"sampling_percent": 50.0,
	}
	_, err = googlepolicy.LoadPolicy(PolicyType, rawInvalid)
	assert.Error(t, err)

	// 4. LoadPolicyProto from proto
	pb := &policyv1alpha1.TraceSamplingPolicy{
		Id:              "from-proto",
		SamplingPercent: 30.0,
	}
	p, err = driver.LoadPolicyProto(pb)
	require.NoError(t, err)
	assert.Equal(t, "from-proto", p.PolicyName())

	// 5. LoadPolicyProto with foreign proto returns error
	foreignProto := &policyv1alpha1.LogFilterPolicy{Id: "bad"}
	_, err = driver.LoadPolicyProto(foreignProto)
	assert.ErrorIs(t, err, googlepolicy.ErrPolicyProtoMismatch)
}

func TestStatisticalDistribution(t *testing.T) {
	t.Parallel()

	// Verify that EvaluateSampling produces the expected proportion of sampled spans
	// over 10,000 synthetic spans.
	const totalSpans = 10000
	const targetPercent = 50.0
	threshold, err := ProbabilityToThreshold(targetPercent)
	require.NoError(t, err)

	sampledCount := 0
	state := uint64(123456789)
	for i := 0; i < totalSpans; i++ {
		state = state*6364136223846793005 + 1442695040888963407
		span := ptrace.NewSpan()
		var traceIDBytes [16]byte
		binary.BigEndian.PutUint64(traceIDBytes[0:], uint64(i))
		binary.BigEndian.PutUint64(traceIDBytes[8:], state)
		span.SetTraceID(pcommon.TraceID(traceIDBytes))

		decision, _, err := EvaluateSampling(span, threshold)
		require.NoError(t, err)
		if decision == DecisionSample {
			sampledCount++
		}
	}

	actualPercent := float64(sampledCount) / float64(totalSpans) * 100.0
	// 50% target with 10k samples has standard error = sqrt(0.5*0.5/10000) = 0.5%.
	// 3 sigma = 1.5%. So actualPercent should be within [47.5, 52.5].
	assert.InDelta(t, targetPercent, actualPercent, 2.5, "actual sampling rate %v deviated from target %v", actualPercent, targetPercent)
}
