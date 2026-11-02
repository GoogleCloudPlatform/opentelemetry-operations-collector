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
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/open-telemetry/opentelemetry-collector-contrib/pkg/sampling"
	"go.opentelemetry.io/collector/pdata/ptrace"
)

// W3C Consistent Probability Sampling Constants (OTEP 235).
const (
	// MaxAdjustedCount is 2^56, the maximum value for consistent sampling math.
	MaxAdjustedCount uint64 = sampling.MaxAdjustedCount
)

var (
	// ErrInvalidProbability is returned when a sampling percentage is outside (0.0, 100.0] or non-finite.
	ErrInvalidProbability = errors.New("sampling percent must be in (0.0, 100.0]")
	// ErrInvalidTValue is returned when a W3C T-value hex string is malformed or out of range.
	ErrInvalidTValue = errors.New("invalid W3C T-value hex string")
	// ErrMissingRandomness is returned when a span has no ot=rv in tracestate and an empty TraceID.
	ErrMissingRandomness = errors.New("missing randomness: tracestate has no ot=rv and TraceID is empty")
)

// SamplingDecision represents the result of evaluating sampling on a span.
type SamplingDecision int

const (
	DecisionDrop SamplingDecision = iota
	DecisionSample
)

// ProbabilityToThreshold converts a sampling percentage p in (0.0, 100.0] into a 56-bit
// threshold T according to W3C Consistent Probability Sampling.
func ProbabilityToThreshold(p float64) (uint64, error) {
	if math.IsNaN(p) || math.IsInf(p, 0) || p <= 0.0 || p > 100.0 {
		return 0, ErrInvalidProbability
	}
	if p == 100.0 {
		return 0, nil
	}
	ratio := p / 100.0
	th, err := sampling.ProbabilityToThreshold(ratio)
	if err != nil {
		return 0, ErrInvalidProbability
	}
	return th.Unsigned(), nil
}

// ThresholdToTValue converts a 56-bit threshold uint64 into a W3C T-value hex string.
func ThresholdToTValue(t uint64) string {
	th, err := sampling.UnsignedToThreshold(t)
	if err != nil {
		return ""
	}
	return th.TValue()
}

// TValueToThreshold parses a W3C T-value hex string into a 56-bit threshold uint64.
func TValueToThreshold(tval string) (uint64, error) {
	th, err := sampling.TValueToThreshold(tval)
	if err != nil {
		return 0, fmt.Errorf("%w: %v", ErrInvalidTValue, err)
	}
	return th.Unsigned(), nil
}

// ExtractRandomness extracts the 56-bit random value R for a span.
// Per OTEP 235:
// 1. If tracestate contains 'ot=rv:<hex>', extract R from the 14-hex-digit rv subkey.
// 2. Otherwise, fall back to the lower 56 bits of bytes 8-15 of the TraceID.
// If both ot=rv and TraceID are missing/empty, returns ErrMissingRandomness to fail open.
func ExtractRandomness(span ptrace.Span) (uint64, error) {
	w3c, err := sampling.NewW3CTraceState(span.TraceState().AsRaw())
	if err == nil {
		if rnd, ok := w3c.OTelValue().RValueRandomness(); ok {
			return rnd.Unsigned(), nil
		}
	}

	traceID := span.TraceID()
	if traceID.IsEmpty() {
		return 0, ErrMissingRandomness
	}
	rnd := sampling.TraceIDToRandomness(traceID)
	return rnd.Unsigned(), nil
}

// ExtractThreshold extracts any existing 56-bit threshold T_in from the span's tracestate.
func ExtractThreshold(span ptrace.Span) (uint64, bool) {
	w3c, err := sampling.NewW3CTraceState(span.TraceState().AsRaw())
	if err != nil {
		return 0, false
	}
	th, ok := w3c.OTelValue().TValueThreshold()
	if !ok {
		return 0, false
	}
	return th.Unsigned(), true
}

// UpdateTraceStateThreshold updates or adds 'ot=th:<tval>' to the span's tracestate.
// Per W3C Trace Context spec, modified entries must be positioned as the leftmost member.
func UpdateTraceStateThreshold(span ptrace.Span, threshold uint64) {
	th, err := sampling.UnsignedToThreshold(threshold)
	if err != nil {
		return
	}
	w3c, _ := sampling.NewW3CTraceState(span.TraceState().AsRaw())
	_ = w3c.OTelValue().UpdateTValueWithSampling(th)
	var sb strings.Builder
	if err := w3c.Serialize(&sb); err == nil {
		span.TraceState().FromRaw(sb.String())
	}
}

// EvaluateSampling evaluates probabilistic trace sampling for a span in equalizing mode.
// Returns:
//   - decision: DecisionSample (kept) or DecisionDrop (dropped)
//   - updated: true if the span's tracestate was modified
//   - error: any unexpected extraction error
func EvaluateSampling(span ptrace.Span, effectiveThreshold uint64) (SamplingDecision, bool, error) {
	effTh, err := sampling.UnsignedToThreshold(effectiveThreshold)
	if err != nil {
		return DecisionDrop, false, err
	}

	w3c, _ := sampling.NewW3CTraceState(span.TraceState().AsRaw())

	// 1. Extract randomness R
	var rnd sampling.Randomness
	if r, ok := w3c.OTelValue().RValueRandomness(); ok {
		rnd = r
	} else {
		traceID := span.TraceID()
		if traceID.IsEmpty() {
			return DecisionDrop, false, ErrMissingRandomness
		}
		rnd = sampling.TraceIDToRandomness(traceID)
	}

	// 2. Check incoming threshold T_in consistency
	if tIn, hasTIn := w3c.OTelValue().TValueThreshold(); hasTIn {
		if tIn.ShouldSample(rnd) {
			// Consistent: upstream sampled at least as strictly (T_in >= T_eff), keep unmodified.
			if !sampling.ThresholdLessThan(tIn, effTh) {
				return DecisionSample, false, nil
			}
		} else {
			// Inconsistent: incoming threshold claims span was sampled, but R < T_in.
			// Clear the invalid incoming threshold so it can be replaced.
			w3c.OTelValue().ClearTValue()
		}
	}

	// 3. Make sampling decision (R >= T_eff)
	if !effTh.ShouldSample(rnd) {
		return DecisionDrop, false, nil
	}

	// 4. Update tracestate with effectiveThreshold
	_ = w3c.OTelValue().UpdateTValueWithSampling(effTh)
	var sb strings.Builder
	if err := w3c.Serialize(&sb); err == nil {
		span.TraceState().FromRaw(sb.String())
	}
	return DecisionSample, true, nil
}
