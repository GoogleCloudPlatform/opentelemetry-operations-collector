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
	"math"
	"strings"

	"github.com/open-telemetry/opentelemetry-collector-contrib/pkg/sampling"
	"go.opentelemetry.io/collector/pdata/ptrace"
)

var (
	// ErrInvalidProbability is returned when a sampling percentage is outside (0.0, 100.0] or non-finite.
	ErrInvalidProbability = errors.New("sampling percent must be in (0.0, 100.0]")
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
