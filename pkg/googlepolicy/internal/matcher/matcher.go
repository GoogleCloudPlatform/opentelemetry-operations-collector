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

// Package matcher provides the signal-agnostic matching engine shared by the
// log, metric, and trace filter policy packages.
//
// Policy evaluation is split in two: a per-signal target extractor pulls a
// value out of the telemetry item, and a signal-agnostic Predicate tests that
// value. Everything in this package belongs to the second half, so that the
// three filter packages agree on what "equals", "regex", "contains", and the
// numeric comparisons mean.
//
// All compilation happens once at policy load time, so the hot path never
// compiles a regex, parses a path segment, or type-switches on a protobuf
// oneof. Extracted values are carried as plain Go values in an `any` rather
// than as pcommon.Value: boxing still costs an allocation for most types, but
// it avoids pdata's heavier per-call AnyValue allocation.
package matcher

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	policyv1alpha1 "github.com/GoogleCloudPlatform/opentelemetry-operations-collector/gen/go/policy/v1alpha1"
	"go.opentelemetry.io/collector/pdata/pcommon"
)

// ErrMissingPredicate is returned when a matcher omits its predicate or sets it
// to a nil value.
var ErrMissingPredicate = errors.New("matcher must specify a valid predicate")

// Predicate tests an extracted target value. The exists flag reports whether
// the extractor found the target at all, which lets predicates distinguish a
// missing field from a zero-valued one.
type Predicate func(val any, exists bool) bool

// kind enumerates the predicate operations supported by the policy protos.
type kind int

const (
	kindExists kind = iota
	kindEquals
	kindRegex
	kindGt
	kindGte
	kindLt
	kindLte
	kindContains
)

// spec is the signal-agnostic form of a matcher predicate, produced by
// normalize from any of the three per-signal protobuf oneof families.
type spec struct {
	k     kind
	val   *policyv1alpha1.Value
	num   *policyv1alpha1.NumericValue
	regex string
}

// CompilePredicate compiles a matcher predicate oneof wrapper (any of the
// LogMatcher_*, MetricMatcher_*, or TraceMatcher_* variants) into a Predicate.
// The negate flag inverts the compiled result.
func CompilePredicate(predicate any, negate bool) (Predicate, error) {
	s, err := normalize(predicate)
	if err != nil {
		return nil, err
	}

	var pred Predicate
	switch s.k {
	case kindExists:
		pred = func(_ any, exists bool) bool { return exists }

	case kindEquals:
		if s.val.GetValue() == nil {
			return nil, ErrMissingPredicate
		}
		expected := s.val
		pred = func(val any, exists bool) bool {
			return present(val, exists) && valueEquals(val, expected)
		}

	case kindRegex:
		re, err := regexp.Compile(s.regex)
		if err != nil {
			return nil, fmt.Errorf("invalid regex %q: %w", s.regex, err)
		}
		pred = func(val any, exists bool) bool {
			return present(val, exists) && valueMatchesRegex(val, re)
		}

	case kindGt:
		pred, err = numericPredicate(s.num, func(c int) bool { return c > 0 })

	case kindGte:
		pred, err = numericPredicate(s.num, func(c int) bool { return c >= 0 })

	case kindLt:
		pred, err = numericPredicate(s.num, func(c int) bool { return c < 0 })

	case kindLte:
		pred, err = numericPredicate(s.num, func(c int) bool { return c <= 0 })

	case kindContains:
		if s.val.GetValue() == nil {
			return nil, ErrMissingPredicate
		}
		expected := s.val
		pred = func(val any, exists bool) bool {
			return present(val, exists) && valueContains(val, expected)
		}
	}
	if err != nil {
		return nil, err
	}

	if negate {
		inner := pred
		return func(val any, exists bool) bool { return !inner(val, exists) }, nil
	}
	return pred, nil
}

// normalize collapses the three per-signal predicate oneof families into a
// single spec. The families are structurally identical but generate distinct
// Go types, so this switch is the one place that has to know about all three.
func normalize(predicate any) (spec, error) {
	switch p := predicate.(type) {
	case *policyv1alpha1.LogMatcher_Exists, *policyv1alpha1.MetricMatcher_Exists, *policyv1alpha1.TraceMatcher_Exists:
		return spec{k: kindExists}, nil

	case *policyv1alpha1.LogMatcher_Equals:
		return spec{k: kindEquals, val: p.Equals}, nil
	case *policyv1alpha1.MetricMatcher_Equals:
		return spec{k: kindEquals, val: p.Equals}, nil
	case *policyv1alpha1.TraceMatcher_Equals:
		return spec{k: kindEquals, val: p.Equals}, nil

	case *policyv1alpha1.LogMatcher_Regex:
		return spec{k: kindRegex, regex: p.Regex}, nil
	case *policyv1alpha1.MetricMatcher_Regex:
		return spec{k: kindRegex, regex: p.Regex}, nil
	case *policyv1alpha1.TraceMatcher_Regex:
		return spec{k: kindRegex, regex: p.Regex}, nil

	case *policyv1alpha1.LogMatcher_Gt:
		return spec{k: kindGt, num: p.Gt}, nil
	case *policyv1alpha1.MetricMatcher_Gt:
		return spec{k: kindGt, num: p.Gt}, nil
	case *policyv1alpha1.TraceMatcher_Gt:
		return spec{k: kindGt, num: p.Gt}, nil

	case *policyv1alpha1.LogMatcher_Gte:
		return spec{k: kindGte, num: p.Gte}, nil
	case *policyv1alpha1.MetricMatcher_Gte:
		return spec{k: kindGte, num: p.Gte}, nil
	case *policyv1alpha1.TraceMatcher_Gte:
		return spec{k: kindGte, num: p.Gte}, nil

	case *policyv1alpha1.LogMatcher_Lt:
		return spec{k: kindLt, num: p.Lt}, nil
	case *policyv1alpha1.MetricMatcher_Lt:
		return spec{k: kindLt, num: p.Lt}, nil
	case *policyv1alpha1.TraceMatcher_Lt:
		return spec{k: kindLt, num: p.Lt}, nil

	case *policyv1alpha1.LogMatcher_Lte:
		return spec{k: kindLte, num: p.Lte}, nil
	case *policyv1alpha1.MetricMatcher_Lte:
		return spec{k: kindLte, num: p.Lte}, nil
	case *policyv1alpha1.TraceMatcher_Lte:
		return spec{k: kindLte, num: p.Lte}, nil

	case *policyv1alpha1.LogMatcher_Contains:
		return spec{k: kindContains, val: p.Contains}, nil
	case *policyv1alpha1.MetricMatcher_Contains:
		return spec{k: kindContains, val: p.Contains}, nil
	case *policyv1alpha1.TraceMatcher_Contains:
		return spec{k: kindContains, val: p.Contains}, nil

	default:
		return spec{}, fmt.Errorf("%w: got %T", ErrMissingPredicate, predicate)
	}
}

func numericPredicate(expected *policyv1alpha1.NumericValue, accept func(cmp int) bool) (Predicate, error) {
	if expected.GetValue() == nil {
		return nil, ErrMissingPredicate
	}
	return func(val any, exists bool) bool {
		if !present(val, exists) {
			return false
		}
		cmp, ok := compareNumeric(val, expected)
		return ok && accept(cmp)
	}, nil
}

// present reports whether the extractor produced something worth testing.
// Extractors report exists=false for absent and empty-string fields but may
// still return a non-nil value, which the value-comparing predicates accept.
func present(val any, exists bool) bool {
	return exists || val != nil
}

// PathStep is one pre-parsed segment of an AttributePath. Segments that parse
// as non-negative integers double as slice indices, resolved at compile time so
// the hot path never calls strconv.
type PathStep struct {
	Key     string
	Index   int
	IsIndex bool
}

// CompilePath pre-parses an AttributePath into PathSteps. The kind label names
// the attribute source (for example "log" or "datapoint") in error messages.
func CompilePath(attrPath *policyv1alpha1.AttributePath, kind string) ([]PathStep, error) {
	if attrPath == nil || len(attrPath.GetPath()) == 0 {
		return nil, fmt.Errorf("%s attribute path cannot be empty", kind)
	}
	segments := attrPath.GetPath()
	steps := make([]PathStep, len(segments))
	for i, seg := range segments {
		if idx, err := strconv.Atoi(seg); err == nil && idx >= 0 {
			steps[i] = PathStep{Key: seg, Index: idx, IsIndex: true}
			continue
		}
		steps[i] = PathStep{Key: seg}
	}
	return steps, nil
}

// EvalPath walks the compiled path through an attribute map, descending into
// nested maps and slices, and returns the leaf value converted to a plain Go
// value.
func EvalPath(attrs pcommon.Map, steps []PathStep) (any, bool) {
	if len(steps) == 0 || attrs == (pcommon.Map{}) {
		return nil, false
	}
	val, ok := attrs.Get(steps[0].Key)
	if !ok {
		return nil, false
	}
	for _, step := range steps[1:] {
		switch val.Type() {
		case pcommon.ValueTypeMap:
			child, found := val.Map().Get(step.Key)
			if !found {
				return nil, false
			}
			val = child
		case pcommon.ValueTypeSlice:
			if !step.IsIndex || step.Index >= val.Slice().Len() {
				return nil, false
			}
			val = val.Slice().At(step.Index)
		default:
			return nil, false
		}
	}
	return ValueToAny(val), true
}

// ValueToAny converts a pcommon.Value into the plain Go value that the
// predicates operate on. Returning plain values rather than pcommon.Value keeps
// the hot path free of pdata allocations.
func ValueToAny(v pcommon.Value) any {
	switch v.Type() {
	case pcommon.ValueTypeEmpty:
		return nil
	case pcommon.ValueTypeStr:
		return v.Str()
	case pcommon.ValueTypeInt:
		return v.Int()
	case pcommon.ValueTypeDouble:
		return v.Double()
	case pcommon.ValueTypeBool:
		return v.Bool()
	case pcommon.ValueTypeBytes:
		return v.Bytes().AsRaw()
	case pcommon.ValueTypeSlice:
		s := v.Slice()
		res := make([]any, s.Len())
		for i := 0; i < s.Len(); i++ {
			res[i] = ValueToAny(s.At(i))
		}
		return res
	default:
		return v.AsString()
	}
}

// Equals reports whether the extracted value equals the policy's expected
// Value, comparing within the type selected by the expected value's oneof.
func valueEquals(val any, expected *policyv1alpha1.Value) bool {
	switch exp := expected.GetValue().(type) {
	case *policyv1alpha1.Value_StringValue:
		s, ok := asString(val)
		return ok && s == exp.StringValue
	case *policyv1alpha1.Value_IntValue:
		if n, ok := toInt64(val); ok {
			return n == exp.IntValue
		}
		if f, ok := toFloat64(val); ok {
			return f == float64(exp.IntValue)
		}
		return false
	case *policyv1alpha1.Value_DoubleValue:
		f, ok := toFloat64(val)
		return ok && f == exp.DoubleValue
	case *policyv1alpha1.Value_BoolValue:
		b, ok := val.(bool)
		return ok && b == exp.BoolValue
	case *policyv1alpha1.Value_BytesValue:
		b, ok := asBytes(val)
		return ok && bytes.Equal(b, exp.BytesValue)
	default:
		return false
	}
}

// Regex reports whether the extracted value matches re. Non-string values are
// rendered with their canonical string form first.
func valueMatchesRegex(val any, re *regexp.Regexp) bool {
	if s, ok := asString(val); ok {
		return re.MatchString(s)
	}
	switch v := val.(type) {
	case int64:
		return re.MatchString(strconv.FormatInt(v, 10))
	case int:
		return re.MatchString(strconv.Itoa(v))
	case bool:
		return re.MatchString(strconv.FormatBool(v))
	default:
		return false
	}
}

// Contains reports containment: membership for slice values, substring for
// strings, and subslice for bytes.
func valueContains(val any, expected *policyv1alpha1.Value) bool {
	if elems, ok := val.([]any); ok {
		for _, elem := range elems {
			if valueEquals(elem, expected) {
				return true
			}
		}
		return false
	}

	switch exp := expected.GetValue().(type) {
	case *policyv1alpha1.Value_StringValue:
		s, ok := asString(val)
		return ok && strings.Contains(s, exp.StringValue)
	case *policyv1alpha1.Value_BytesValue:
		b, ok := asBytes(val)
		return ok && bytes.Contains(b, exp.BytesValue)
	default:
		return false
	}
}

// CompareNumeric compares the extracted value against the policy's expected
// NumericValue, returning -1, 0, or 1. The second result is false when the
// extracted value is not numeric or the expected value is unset.
func compareNumeric(val any, expected *policyv1alpha1.NumericValue) (int, bool) {
	intVal, isInt := toInt64(val)
	floatVal, isFloat := toFloat64(val)
	if !isInt && !isFloat {
		return 0, false
	}

	switch exp := expected.GetValue().(type) {
	case *policyv1alpha1.NumericValue_IntValue:
		if isInt {
			return compare(intVal, exp.IntValue), true
		}
		return compare(floatVal, float64(exp.IntValue)), true
	case *policyv1alpha1.NumericValue_DoubleValue:
		if math.IsNaN(exp.DoubleValue) {
			return 0, false
		}
		return compare(floatVal, exp.DoubleValue), true
	default:
		return 0, false
	}
}

func compare[T int64 | float64](a, b T) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

// asString renders the string-like extracted values. TraceID and SpanID are
// carried as their native pdata types to avoid hot-path allocations, so they
// are rendered to their lowercase hex form only when a string is needed.
func asString(val any) (string, bool) {
	switch v := val.(type) {
	case string:
		return v, true
	case pcommon.TraceID:
		return v.String(), true
	case pcommon.SpanID:
		return v.String(), true
	default:
		return "", false
	}
}

// asBytes exposes the byte-like extracted values, including the raw bytes
// behind a TraceID or SpanID.
func asBytes(val any) ([]byte, bool) {
	switch v := val.(type) {
	case []byte:
		return v, true
	case pcommon.TraceID:
		return v[:], true
	case pcommon.SpanID:
		return v[:], true
	default:
		return nil, false
	}
}

func toFloat64(val any) (float64, bool) {
	switch v := val.(type) {
	case float64:
		if math.IsNaN(v) {
			return 0, false
		}
		return v, true
	case float32:
		f := float64(v)
		if math.IsNaN(f) {
			return 0, false
		}
		return f, true
	case int64:
		return float64(v), true
	case int:
		return float64(v), true
	case int32:
		return float64(v), true
	case uint64:
		return float64(v), true
	case uint32:
		return float64(v), true
	default:
		return 0, false
	}
}

func toInt64(val any) (int64, bool) {
	switch v := val.(type) {
	case int64:
		return v, true
	case int:
		return int64(v), true
	case int32:
		return int64(v), true
	case uint64:
		return int64(v), true
	case uint32:
		return int64(v), true
	default:
		return 0, false
	}
}
