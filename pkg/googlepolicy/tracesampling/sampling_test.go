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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"
)

func TestProbabilityToThreshold(t *testing.T) {
	t.Parallel()

	// 100% sampling -> threshold is 0
	th100, err := ProbabilityToThreshold(100.0)
	require.NoError(t, err)
	assert.Equal(t, uint64(0), th100)

	// 50% sampling -> threshold is 2^55
	th50, err := ProbabilityToThreshold(50.0)
	require.NoError(t, err)
	assert.Equal(t, uint64(1)<<55, th50)

	// Valid range boundaries
	_, err = ProbabilityToThreshold(0.000001)
	assert.NoError(t, err)
	_, err = ProbabilityToThreshold(99.999999)
	assert.NoError(t, err)

	// Invalid probabilities
	invalidProbabilities := []float64{0.0, -0.01, -50.0, 100.0001, 200.0, math.NaN(), math.Inf(1), math.Inf(-1)}
	for _, p := range invalidProbabilities {
		_, err := ProbabilityToThreshold(p)
		assert.ErrorIs(t, err, ErrInvalidProbability, "expected ErrInvalidProbability for %v", p)
	}
}

func TestEvaluateSampling_EqualizingMode(t *testing.T) {
	t.Parallel()

	targetThreshold := uint64(1) << 55 // 50% -> threshold "8"

	// Case 1: Incoming span has T_in >= targetThreshold (already sampled at least as strictly upstream).
	// Example: upstream sampled at 25% (T_in = 0.75 * 2^56 = 0xc0000000000000 -> "c").
	// With consistent randomness R >= T_in (0xe0000000000000 > 0xc0000000000000), it must be kept without modifying tracestate.
	spanStrict := ptrace.NewSpan()
	spanStrict.TraceState().FromRaw("ot=th:c;rv:e0000000000000")
	decision, updated, err := EvaluateSampling(spanStrict, targetThreshold)
	require.NoError(t, err)
	assert.Equal(t, DecisionSample, decision)
	assert.False(t, updated)
	assert.Contains(t, spanStrict.TraceState().AsRaw(), "th:c")

	// Case 1b: Incoming span claims T_in >= targetThreshold, but has INCONSISTENT randomness R < T_in.
	// Per spec, the inconsistent T_in is untrusted/cleared, and the span is evaluated against targetThreshold.
	// If R < targetThreshold, it is dropped.
	spanInconsistent := ptrace.NewSpan()
	spanInconsistent.TraceState().FromRaw("ot=th:c;rv:00000000000000")
	decision, updated, err = EvaluateSampling(spanInconsistent, targetThreshold)
	require.NoError(t, err)
	assert.Equal(t, DecisionDrop, decision)
	assert.False(t, updated)

	// Case 2: Incoming span has no T_in (or less strict T_in), and R < targetThreshold -> DROP
	spanDrop := ptrace.NewSpan()
	var traceIDDrop [16]byte
	binary.BigEndian.PutUint64(traceIDDrop[8:], (uint64(1)<<55)-1) // R < 2^55
	spanDrop.SetTraceID(pcommon.TraceID(traceIDDrop))
	decision, updated, err = EvaluateSampling(spanDrop, targetThreshold)
	require.NoError(t, err)
	assert.Equal(t, DecisionDrop, decision)
	assert.False(t, updated)

	// Case 3: Incoming span has R >= targetThreshold -> SAMPLE and update tracestate
	spanSample := ptrace.NewSpan()
	var traceIDSample [16]byte
	binary.BigEndian.PutUint64(traceIDSample[8:], (uint64(1)<<55)+1) // R >= 2^55
	spanSample.SetTraceID(pcommon.TraceID(traceIDSample))
	decision, updated, err = EvaluateSampling(spanSample, targetThreshold)
	require.NoError(t, err)
	assert.Equal(t, DecisionSample, decision)
	assert.True(t, updated)
	assert.Contains(t, spanSample.TraceState().AsRaw(), "ot=th:8")

	// Case 4: Missing randomness (empty TraceID and no ot=rv) returns ErrMissingRandomness
	spanEmpty := ptrace.NewSpan()
	decision, updated, err = EvaluateSampling(spanEmpty, targetThreshold)
	assert.ErrorIs(t, err, ErrMissingRandomness)
	assert.Equal(t, DecisionDrop, decision)
	assert.False(t, updated)

	// Case 5: Preserving other vendor entries and positioning ot leftmost
	spanVendors := ptrace.NewSpan()
	var traceIDVendors [16]byte
	binary.BigEndian.PutUint64(traceIDVendors[8:], (uint64(1)<<55)+1)
	spanVendors.SetTraceID(pcommon.TraceID(traceIDVendors))
	spanVendors.TraceState().FromRaw("congo=xyz,rojo=abc")
	decision, updated, err = EvaluateSampling(spanVendors, targetThreshold)
	require.NoError(t, err)
	assert.Equal(t, DecisionSample, decision)
	assert.True(t, updated)
	assert.Equal(t, "ot=th:8,congo=xyz,rojo=abc", spanVendors.TraceState().AsRaw())

	// Case 6: Preserving existing ot subkeys when updating th
	spanOtSubkeys := ptrace.NewSpan()
	spanOtSubkeys.TraceState().FromRaw("congo=xyz,rojo=abc,ot=p:4;rv:e0000000000000")
	decision, updated, err = EvaluateSampling(spanOtSubkeys, targetThreshold)
	require.NoError(t, err)
	assert.Equal(t, DecisionSample, decision)
	assert.True(t, updated)
	ts := spanOtSubkeys.TraceState().AsRaw()
	assert.Equal(t, "ot=rv:e0000000000000;th:8;p:4,congo=xyz,rojo=abc", ts)

	// Case 7: Malformed ot=rv falls back to TraceID randomness
	spanMalformedRv := ptrace.NewSpan()
	var traceIDFallback [16]byte
	binary.BigEndian.PutUint64(traceIDFallback[8:], (uint64(1)<<55)+1) // R >= 2^55 in TraceID
	spanMalformedRv.SetTraceID(pcommon.TraceID(traceIDFallback))
	spanMalformedRv.TraceState().FromRaw("ot=rv:short") // invalid rv length, falls back to TraceID
	decision, updated, err = EvaluateSampling(spanMalformedRv, targetThreshold)
	require.NoError(t, err)
	assert.Equal(t, DecisionSample, decision)
	assert.True(t, updated)
	assert.Contains(t, spanMalformedRv.TraceState().AsRaw(), "th:8")

	// Case 8: Malformed ot=rv with low TraceID randomness drops
	spanMalformedRvDrop := ptrace.NewSpan()
	var traceIDFallbackDrop [16]byte
	binary.BigEndian.PutUint64(traceIDFallbackDrop[8:], (uint64(1)<<55)-1) // R < 2^55 in TraceID
	spanMalformedRvDrop.SetTraceID(pcommon.TraceID(traceIDFallbackDrop))
	spanMalformedRvDrop.TraceState().FromRaw("ot=rv:short")
	decision, updated, err = EvaluateSampling(spanMalformedRvDrop, targetThreshold)
	require.NoError(t, err)
	assert.Equal(t, DecisionDrop, decision)
	assert.False(t, updated)
}
