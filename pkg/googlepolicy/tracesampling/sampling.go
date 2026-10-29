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
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"go.opentelemetry.io/collector/pdata/ptrace"
)

// W3C Consistent Probability Sampling Constants (OTEP 235).
const (
	// MaxAdjustedCount is 2^56, the maximum value for consistent sampling math.
	MaxAdjustedCount uint64 = 1 << 56

	// OTVendorKey is the OpenTelemetry vendor key in W3C tracestate.
	OTVendorKey = "ot"

	// ThresholdSubkey is the subkey for the sampling threshold in the 'ot' tracestate entry.
	ThresholdSubkey = "th"

	// RandomnessSubkey is the subkey for the random value in the 'ot' tracestate entry.
	RandomnessSubkey = "rv"
)

var (
	// ErrInvalidProbability is returned when a sampling percentage is outside (0.0, 100.0] or non-finite.
	ErrInvalidProbability = errors.New("sampling percent must be in (0.0, 100.0]")
	// ErrInvalidTValue is returned when a W3C T-value hex string is malformed or out of range.
	ErrInvalidTValue = errors.New("invalid W3C T-value hex string")
)

// SamplingDecision represents the result of evaluating sampling on a span.
type SamplingDecision int

const (
	DecisionDrop SamplingDecision = iota
	DecisionSample
)

// ProbabilityToThreshold converts a sampling percentage p in (0.0, 100.0] into a 56-bit
// threshold T according to W3C Consistent Probability Sampling:
//
//	T = 2^56 - round(p% * 2^56)
//
// A sampling percentage of 100% yields a threshold of 0 (all spans sampled).
func ProbabilityToThreshold(p float64) (uint64, error) {
	if math.IsNaN(p) || math.IsInf(p, 0) || p <= 0.0 || p > 100.0 {
		return 0, ErrInvalidProbability
	}
	if p == 100.0 {
		return 0, nil
	}
	ratio := p / 100.0
	count := uint64(math.Round(ratio * float64(MaxAdjustedCount)))
	if count >= MaxAdjustedCount {
		return 0, nil
	}
	if count == 0 {
		count = 1
	}
	return MaxAdjustedCount - count, nil
}

// ThresholdToTValue converts a 56-bit threshold uint64 into a W3C T-value hex string.
// The value is formatted as up to 14 hex digits with trailing zeros omitted.
// A threshold of 0 is formatted as "0".
func ThresholdToTValue(t uint64) string {
	if t == 0 {
		return "0"
	}
	hexStr := fmt.Sprintf("%014x", t)
	trimmed := strings.TrimRight(hexStr, "0")
	if trimmed == "" {
		return "0"
	}
	return trimmed
}

// TValueToThreshold parses a W3C T-value hex string into a 56-bit threshold uint64.
// It pads right with zeros up to 14 hex digits and parses the result.
func TValueToThreshold(tval string) (uint64, error) {
	if tval == "" || len(tval) > 14 {
		return 0, ErrInvalidTValue
	}
	// Pad right with zeros up to 14 characters.
	padded := tval + strings.Repeat("0", 14-len(tval))
	val, err := strconv.ParseUint(padded, 16, 64)
	if err != nil {
		return 0, fmt.Errorf("%w: %v", ErrInvalidTValue, err)
	}
	if val >= MaxAdjustedCount {
		return 0, ErrInvalidTValue
	}
	return val, nil
}

// ExtractRandomness extracts the 56-bit random value R for a span.
// Per OTEP 235:
// 1. If tracestate contains 'ot=rv:<hex>', extract R from the 14-hex-digit rv subkey.
// 2. Otherwise, fall back to the lower 56 bits of bytes 8-15 of the TraceID.
func ExtractRandomness(span ptrace.Span) (uint64, error) {
	tsRaw := span.TraceState().AsRaw()
	if tsRaw != "" {
		if rvHex, ok := getOTSubkey(tsRaw, RandomnessSubkey); ok {
			if len(rvHex) == 14 {
				val, err := strconv.ParseUint(rvHex, 16, 64)
				if err == nil && val < MaxAdjustedCount {
					return val, nil
				}
			}
		}
	}

	// Fallback: lower 56 bits of bytes 8-15 of TraceID
	traceID := span.TraceID()
	b8_15 := traceID[8:]
	u64 := binary.BigEndian.Uint64(b8_15)
	return u64 & 0x00ffffffffffffff, nil
}

// ExtractThreshold extracts any existing 56-bit threshold T_in from the span's tracestate 'ot=th:<tval>'.
// Returns the threshold and true if present and valid; otherwise returns 0 and false.
func ExtractThreshold(span ptrace.Span) (uint64, bool) {
	tsRaw := span.TraceState().AsRaw()
	if tsRaw == "" {
		return 0, false
	}
	thVal, ok := getOTSubkey(tsRaw, ThresholdSubkey)
	if !ok {
		return 0, false
	}
	thresh, err := TValueToThreshold(thVal)
	if err != nil {
		return 0, false
	}
	return thresh, true
}

// UpdateTraceStateThreshold updates or adds 'ot=th:<tval>' to the span's tracestate.
// Per W3C Trace Context spec, modified entries must be positioned as the leftmost member.
func UpdateTraceStateThreshold(span ptrace.Span, threshold uint64) {
	tval := ThresholdToTValue(threshold)
	tsRaw := span.TraceState().AsRaw()
	newTS := putOTSubkey(tsRaw, ThresholdSubkey, tval)
	span.TraceState().FromRaw(newTS)
}

// EvaluateSampling evaluates probabilistic trace sampling for a span in equalizing mode.
// Returns:
//   - decision: DecisionSample (kept) or DecisionDrop (dropped)
//   - updated: true if the span's tracestate was modified
//   - error: any unexpected extraction error
func EvaluateSampling(span ptrace.Span, effectiveThreshold uint64) (SamplingDecision, bool, error) {
	// Equalizing mode:
	// If incoming span has threshold T_in >= effectiveThreshold, it was already sampled
	// at least as strictly upstream. Keep it without modification.
	tIn, hasTIn := ExtractThreshold(span)
	if hasTIn && tIn >= effectiveThreshold {
		return DecisionSample, false, nil
	}

	// Compare randomness R against effectiveThreshold.
	r, err := ExtractRandomness(span)
	if err != nil {
		return DecisionDrop, false, err
	}

	// If R < effectiveThreshold, the span is dropped.
	if r < effectiveThreshold {
		return DecisionDrop, false, nil
	}

	// If R >= effectiveThreshold, the span is sampled. Update tracestate with effectiveThreshold.
	UpdateTraceStateThreshold(span, effectiveThreshold)
	return DecisionSample, true, nil
}

// Helper functions for parsing and modifying W3C tracestate and OpenTelemetry 'ot' subkeys.

func getOTSubkey(tsRaw string, subkey string) (string, bool) {
	entries := strings.Split(tsRaw, ",")
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if strings.HasPrefix(entry, OTVendorKey+"=") {
			otVal := strings.TrimPrefix(entry, OTVendorKey+"=")
			return findSubkey(otVal, subkey)
		}
	}
	return "", false
}

func findSubkey(otVal string, subkey string) (string, bool) {
	parts := strings.Split(otVal, ";")
	prefix := subkey + ":"
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if strings.HasPrefix(p, prefix) {
			return strings.TrimPrefix(p, prefix), true
		}
	}
	return "", false
}

func putOTSubkey(tsRaw string, subkey string, value string) string {
	var otherVendors []string
	var existingOTParts []string

	if tsRaw != "" {
		for _, entry := range strings.Split(tsRaw, ",") {
			entry = strings.TrimSpace(entry)
			if entry == "" {
				continue
			}
			if strings.HasPrefix(entry, OTVendorKey+"=") {
				otVal := strings.TrimPrefix(entry, OTVendorKey+"=")
				for _, p := range strings.Split(otVal, ";") {
					p = strings.TrimSpace(p)
					if p != "" && !strings.HasPrefix(p, subkey+":") {
						existingOTParts = append(existingOTParts, p)
					}
				}
			} else {
				otherVendors = append(otherVendors, entry)
			}
		}
	}

	// Build new 'ot' entry with the updated subkey
	allOTParts := append([]string{subkey + ":" + value}, existingOTParts...)
	newOTEntry := OTVendorKey + "=" + strings.Join(allOTParts, ";")

	// Per W3C spec, 'ot' must be the leftmost member.
	allEntries := append([]string{newOTEntry}, otherVendors...)
	return strings.Join(allEntries, ",")
}
