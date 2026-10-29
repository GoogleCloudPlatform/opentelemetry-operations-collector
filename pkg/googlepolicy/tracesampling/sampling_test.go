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

func TestThresholdToTValue_And_Back(t *testing.T) {
	t.Parallel()

	tests := []struct {
		threshold uint64
		tval      string
	}{
		{threshold: 0, tval: "0"},
		{threshold: 1 << 55, tval: "8"}, // 0x80000000000000 -> "8"
		{threshold: (1 << 55) | (1 << 51), tval: "88"},
		{threshold: 0x1234567890abcd, tval: "1234567890abcd"},
	}

	for _, tc := range tests {
		encoded := ThresholdToTValue(tc.threshold)
		assert.Equal(t, tc.tval, encoded)

		decoded, err := TValueToThreshold(tc.tval)
		require.NoError(t, err)
		assert.Equal(t, tc.threshold, decoded)
	}

	// Invalid T-values
	_, err := TValueToThreshold("")
	assert.ErrorIs(t, err, ErrInvalidTValue)

	_, err = TValueToThreshold("1234567890abcdef") // > 14 hex chars
	assert.ErrorIs(t, err, ErrInvalidTValue)

	_, err = TValueToThreshold("not-hex")
	assert.ErrorIs(t, err, ErrInvalidTValue)
}

func TestExtractRandomness(t *testing.T) {
	t.Parallel()

	span := ptrace.NewSpan()
	var traceIDBytes [16]byte
	// Set bytes 8-15 to 0x001234567890abcd
	binary.BigEndian.PutUint64(traceIDBytes[8:], 0x001234567890abcd)
	span.SetTraceID(pcommon.TraceID(traceIDBytes))

	// 1. Without tracestate: fallback to lower 56 bits of bytes 8-15 of TraceID
	r, err := ExtractRandomness(span)
	require.NoError(t, err)
	assert.Equal(t, uint64(0x001234567890abcd), r)

	// 2. With tracestate containing ot=rv:<14-hex>
	span.TraceState().FromRaw("ot=rv:00fedcba987654")
	r, err = ExtractRandomness(span)
	require.NoError(t, err)
	assert.Equal(t, uint64(0x00fedcba987654), r)

	// 3. With multiple vendors in tracestate
	span.TraceState().FromRaw("congo=t61rcWkgMzE,ot=p:4;rv:00112233445566,rojo=something")
	r, err = ExtractRandomness(span)
	require.NoError(t, err)
	assert.Equal(t, uint64(0x00112233445566), r)

	// 4. Invalid rv hex length falls back to TraceID
	span.TraceState().FromRaw("ot=rv:short")
	r, err = ExtractRandomness(span)
	require.NoError(t, err)
	assert.Equal(t, uint64(0x001234567890abcd), r)
}

func TestExtractThreshold(t *testing.T) {
	t.Parallel()

	span := ptrace.NewSpan()

	// 1. Empty tracestate
	_, ok := ExtractThreshold(span)
	assert.False(t, ok)

	// 2. No ot entry
	span.TraceState().FromRaw("vendor=xyz")
	_, ok = ExtractThreshold(span)
	assert.False(t, ok)

	// 3. ot entry without th subkey
	span.TraceState().FromRaw("ot=rv:00112233445566")
	_, ok = ExtractThreshold(span)
	assert.False(t, ok)

	// 4. Valid th subkey (50% sampling -> "8" -> 2^55)
	span.TraceState().FromRaw("ot=th:8;rv:00112233445566")
	th, ok := ExtractThreshold(span)
	assert.True(t, ok)
	assert.Equal(t, uint64(1)<<55, th)

	// 5. Valid th subkey among multiple vendors
	span.TraceState().FromRaw("congo=val,ot=p:1;th:8;rv:123,rojo=val2")
	th, ok = ExtractThreshold(span)
	assert.True(t, ok)
	assert.Equal(t, uint64(1)<<55, th)
}

func TestUpdateTraceStateThreshold(t *testing.T) {
	t.Parallel()

	span := ptrace.NewSpan()

	// 1. Empty tracestate -> creates ot=th:<tval>
	UpdateTraceStateThreshold(span, 1<<55)
	assert.Equal(t, "ot=th:8", span.TraceState().AsRaw())

	// 2. Existing ot subkeys are preserved, th is updated
	span.TraceState().FromRaw("ot=rv:00112233445566;p:4")
	UpdateTraceStateThreshold(span, 1<<55)
	ts := span.TraceState().AsRaw()
	assert.Contains(t, ts, "th:8")
	assert.Contains(t, ts, "rv:00112233445566")
	assert.Contains(t, ts, "p:4")

	// 3. Existing other vendors are preserved and ot is moved to leftmost
	span.TraceState().FromRaw("congo=xyz,rojo=abc,ot=th:4;rv:123")
	UpdateTraceStateThreshold(span, 1<<55)
	ts = span.TraceState().AsRaw()
	assert.True(t, ts == "ot=th:8;rv:123,congo=xyz,rojo=abc" || ts == "ot=th:8;rv:123,rojo=abc,congo=xyz")
}

func TestEvaluateSampling_EqualizingMode(t *testing.T) {
	t.Parallel()

	targetThreshold := uint64(1) << 55 // 50%

	// Case 1: Incoming span has T_in >= targetThreshold (already sampled at least as strictly upstream)
	// Example: upstream sampled at 25% (T_in = 0.75 * 2^56 > 0.5 * 2^56).
	// In equalizing mode, it must be kept without modifying tracestate.
	spanStrict := ptrace.NewSpan()
	tInStrict := uint64(3) << 54 // 75% threshold
	spanStrict.TraceState().FromRaw("ot=th:" + ThresholdToTValue(tInStrict) + ";rv:00000000000000")
	decision, updated, err := EvaluateSampling(spanStrict, targetThreshold)
	require.NoError(t, err)
	assert.Equal(t, DecisionSample, decision)
	assert.False(t, updated)
	assert.Contains(t, spanStrict.TraceState().AsRaw(), ThresholdToTValue(tInStrict))

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
}
