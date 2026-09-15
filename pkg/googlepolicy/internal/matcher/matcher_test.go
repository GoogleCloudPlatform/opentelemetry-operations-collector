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

package matcher

import (
	"testing"

	policyv1alpha1 "github.com/GoogleCloudPlatform/opentelemetry-operations-collector/gen/go/policy/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/pdata/pcommon"
)

func intValue(v int64) *policyv1alpha1.Value {
	return &policyv1alpha1.Value{Value: &policyv1alpha1.Value_IntValue{IntValue: v}}
}

func strValue(v string) *policyv1alpha1.Value {
	return &policyv1alpha1.Value{Value: &policyv1alpha1.Value_StringValue{StringValue: v}}
}

func bytesValue(v []byte) *policyv1alpha1.Value {
	return &policyv1alpha1.Value{Value: &policyv1alpha1.Value_BytesValue{BytesValue: v}}
}

func intNumeric(v int64) *policyv1alpha1.NumericValue {
	return &policyv1alpha1.NumericValue{Value: &policyv1alpha1.NumericValue_IntValue{IntValue: v}}
}

func doubleNumeric(v float64) *policyv1alpha1.NumericValue {
	return &policyv1alpha1.NumericValue{Value: &policyv1alpha1.NumericValue_DoubleValue{DoubleValue: v}}
}

func mustCompile(t *testing.T, predicate any) Predicate {
	t.Helper()
	p, err := CompilePredicate(predicate, false)
	require.NoError(t, err)
	return p
}

// TestCompilePredicateRejectsMalformed asserts that a policy carrying a
// predicate the engine cannot honour is rejected at load time rather than
// silently never matching at runtime.
func TestCompilePredicateRejectsMalformed(t *testing.T) {
	tests := []struct {
		name      string
		predicate any
	}{
		{"nil predicate", nil},
		{"unknown predicate type", struct{}{}},
		{"invalid log regex", &policyv1alpha1.LogMatcher_Regex{Regex: "[invalid"}},
		{"invalid metric regex", &policyv1alpha1.MetricMatcher_Regex{Regex: "[invalid"}},
		{"invalid trace regex", &policyv1alpha1.TraceMatcher_Regex{Regex: "[invalid"}},
		{"equals with nil value", &policyv1alpha1.MetricMatcher_Equals{}},
		{"equals with unset value oneof", &policyv1alpha1.MetricMatcher_Equals{Equals: &policyv1alpha1.Value{}}},
		{"contains with unset value oneof", &policyv1alpha1.TraceMatcher_Contains{Contains: &policyv1alpha1.Value{}}},
		{"gt with nil numeric", &policyv1alpha1.MetricMatcher_Gt{}},
		{"gt with unset numeric oneof", &policyv1alpha1.TraceMatcher_Gt{Gt: &policyv1alpha1.NumericValue{}}},
		{"lte with unset numeric oneof", &policyv1alpha1.LogMatcher_Lte{Lte: &policyv1alpha1.NumericValue{}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := CompilePredicate(tt.predicate, false)
			assert.Error(t, err)
		})
	}
}

// TestCompilePredicateAcceptsAllSignalFamilies asserts the three structurally
// identical protobuf oneof families compile to the same behaviour.
func TestCompilePredicateAcceptsAllSignalFamilies(t *testing.T) {
	families := []struct {
		name string
		gt   any
	}{
		{"log", &policyv1alpha1.LogMatcher_Gt{Gt: intNumeric(10)}},
		{"metric", &policyv1alpha1.MetricMatcher_Gt{Gt: intNumeric(10)}},
		{"trace", &policyv1alpha1.TraceMatcher_Gt{Gt: intNumeric(10)}},
	}

	for _, f := range families {
		t.Run(f.name, func(t *testing.T) {
			pred := mustCompile(t, f.gt)
			assert.True(t, pred(int64(11), true))
			assert.False(t, pred(int64(10), true))
			assert.False(t, pred(nil, false))
		})
	}
}

func TestPredicateExists(t *testing.T) {
	pred := mustCompile(t, &policyv1alpha1.MetricMatcher_Exists{})
	assert.True(t, pred("val", true))
	assert.False(t, pred("", false))
}

func TestPredicateNegate(t *testing.T) {
	pred, err := CompilePredicate(&policyv1alpha1.LogMatcher_Exists{}, true)
	require.NoError(t, err)
	assert.False(t, pred("val", true))
	assert.True(t, pred("", false))
}

func TestPredicateRegex(t *testing.T) {
	pred := mustCompile(t, &policyv1alpha1.MetricMatcher_Regex{Regex: "^(42|true|hello)$"})
	assert.True(t, pred("hello", true))
	assert.True(t, pred(int64(42), true))
	assert.True(t, pred(42, true))
	assert.True(t, pred(true, true))
	assert.False(t, pred(false, true))
	assert.False(t, pred(99, true))
	assert.False(t, pred(nil, false))
}

func TestPredicateRegexOnIDs(t *testing.T) {
	traceID := pcommon.TraceID([16]byte{0xaa, 0xbb})
	spanID := pcommon.SpanID([8]byte{0xcc, 0xdd})

	pred := mustCompile(t, &policyv1alpha1.TraceMatcher_Regex{Regex: "^aabb"})
	assert.True(t, pred(traceID, true))

	spanPred := mustCompile(t, &policyv1alpha1.TraceMatcher_Regex{Regex: "^ccdd"})
	assert.True(t, spanPred(spanID, true))
}

func TestPredicateEquals(t *testing.T) {
	t.Run("int widens across Go integer types", func(t *testing.T) {
		pred := mustCompile(t, &policyv1alpha1.MetricMatcher_Equals{Equals: intValue(100)})
		assert.True(t, pred(int64(100), true))
		assert.True(t, pred(100, true))
		assert.True(t, pred(int32(100), true))
		assert.True(t, pred(uint64(100), true))
		assert.True(t, pred(uint32(100), true))
		assert.False(t, pred("not-an-int", true))
	})

	t.Run("double widens across Go float types", func(t *testing.T) {
		pred := mustCompile(t, &policyv1alpha1.MetricMatcher_Equals{
			Equals: &policyv1alpha1.Value{Value: &policyv1alpha1.Value_DoubleValue{DoubleValue: 3.5}},
		})
		assert.True(t, pred(3.5, true))
		assert.True(t, pred(float32(3.5), true))
		assert.False(t, pred("str", true))
	})

	t.Run("bool", func(t *testing.T) {
		pred := mustCompile(t, &policyv1alpha1.TraceMatcher_Equals{
			Equals: &policyv1alpha1.Value{Value: &policyv1alpha1.Value_BoolValue{BoolValue: true}},
		})
		assert.True(t, pred(true, true))
		assert.False(t, pred(false, true))
	})

	t.Run("bytes", func(t *testing.T) {
		pred := mustCompile(t, &policyv1alpha1.MetricMatcher_Equals{Equals: bytesValue([]byte{1, 2, 3})})
		assert.True(t, pred([]byte{1, 2, 3}, true))
		assert.False(t, pred([]byte{4, 5}, true))
	})

	t.Run("trace and span IDs match as hex string or raw bytes", func(t *testing.T) {
		traceID := pcommon.TraceID([16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16})
		spanID := pcommon.SpanID([8]byte{1, 2, 3, 4, 5, 6, 7, 8})

		asString := mustCompile(t, &policyv1alpha1.LogMatcher_Equals{Equals: strValue(traceID.String())})
		assert.True(t, asString(traceID, true))

		asBytes := mustCompile(t, &policyv1alpha1.LogMatcher_Equals{Equals: bytesValue(traceID[:])})
		assert.True(t, asBytes(traceID, true))

		spanAsString := mustCompile(t, &policyv1alpha1.LogMatcher_Equals{Equals: strValue(spanID.String())})
		assert.True(t, spanAsString(spanID, true))

		spanAsBytes := mustCompile(t, &policyv1alpha1.LogMatcher_Equals{Equals: bytesValue(spanID[:])})
		assert.True(t, spanAsBytes(spanID, true))
	})
}

func TestPredicateNumericComparisons(t *testing.T) {
	tests := []struct {
		name      string
		predicate any
		val       any
		want      bool
	}{
		{"gt int true", &policyv1alpha1.MetricMatcher_Gt{Gt: intNumeric(10)}, int64(11), true},
		{"gt int equal is false", &policyv1alpha1.MetricMatcher_Gt{Gt: intNumeric(10)}, int64(10), false},
		{"gt double", &policyv1alpha1.MetricMatcher_Gt{Gt: doubleNumeric(5.5)}, 6.0, true},
		{"gte int32", &policyv1alpha1.MetricMatcher_Gte{Gte: intNumeric(10)}, int32(10), true},
		{"gte uint64", &policyv1alpha1.MetricMatcher_Gte{Gte: intNumeric(10)}, uint64(10), true},
		{"gte uint32", &policyv1alpha1.TraceMatcher_Gte{Gte: intNumeric(10)}, uint32(10), true},
		{"lt float32", &policyv1alpha1.MetricMatcher_Lt{Lt: doubleNumeric(5.0)}, float32(4.0), true},
		{"lt int", &policyv1alpha1.MetricMatcher_Lt{Lt: intNumeric(5)}, int64(4), true},
		{"lte int equal", &policyv1alpha1.MetricMatcher_Lte{Lte: intNumeric(5)}, int64(5), true},
		{"lte int above", &policyv1alpha1.TraceMatcher_Lte{Lte: intNumeric(5)}, int64(6), false},
		{"double value against int target", &policyv1alpha1.MetricMatcher_Gt{Gt: intNumeric(5)}, 5.5, true},
		{"non-numeric value never matches", &policyv1alpha1.MetricMatcher_Gt{Gt: intNumeric(5)}, "ten", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pred := mustCompile(t, tt.predicate)
			assert.Equal(t, tt.want, pred(tt.val, true))
		})
	}
}

func TestPredicateContains(t *testing.T) {
	t.Run("string substring and slice membership", func(t *testing.T) {
		pred := mustCompile(t, &policyv1alpha1.MetricMatcher_Contains{Contains: strValue("sub")})
		assert.True(t, pred("my_substring", true))
		assert.True(t, pred([]any{"a", "sub", "b"}, true))
		assert.False(t, pred([]any{"a", "b"}, true))
		assert.False(t, pred(123, true))
	})

	t.Run("bytes subslice", func(t *testing.T) {
		pred := mustCompile(t, &policyv1alpha1.MetricMatcher_Contains{Contains: bytesValue([]byte{2, 3})})
		assert.True(t, pred([]byte{1, 2, 3, 4}, true))
		assert.False(t, pred([]byte{1, 4}, true))
	})

	t.Run("slice of ints", func(t *testing.T) {
		pred := mustCompile(t, &policyv1alpha1.TraceMatcher_Contains{Contains: intValue(42)})
		assert.True(t, pred([]any{int64(10), int64(42)}, true))
		assert.False(t, pred([]any{int64(10)}, true))
	})
}

func TestCompilePathRejectsEmpty(t *testing.T) {
	_, err := CompilePath(nil, "log")
	assert.ErrorContains(t, err, "log attribute path cannot be empty")

	_, err = CompilePath(&policyv1alpha1.AttributePath{}, "datapoint")
	assert.ErrorContains(t, err, "datapoint attribute path cannot be empty")
}

func TestEvalPath(t *testing.T) {
	m := pcommon.NewMap()
	m.PutStr("str", "val")
	m.PutInt("int", 123)
	m.PutDouble("double", 4.56)
	m.PutBool("bool", true)
	m.PutEmptyBytes("bytes").FromRaw([]byte{9, 8})
	m.PutEmptyMap("nested").PutStr("inner", "found")
	sl := m.PutEmptySlice("items")
	sl.AppendEmpty().SetStr("zero")
	sl.AppendEmpty().SetInt(10)
	sl.AppendEmpty().SetEmptyMap().PutStr("deep", "value")

	tests := []struct {
		name string
		path []string
		want any
		ok   bool
	}{
		{"missing top-level key", []string{"missing"}, nil, false},
		{"string", []string{"str"}, "val", true},
		{"int", []string{"int"}, int64(123), true},
		{"double", []string{"double"}, 4.56, true},
		{"bool", []string{"bool"}, true, true},
		{"bytes", []string{"bytes"}, []byte{9, 8}, true},
		{"nested map", []string{"nested", "inner"}, "found", true},
		{"missing nested key", []string{"nested", "missing"}, nil, false},
		{"slice index 0", []string{"items", "0"}, "zero", true},
		{"slice index 1", []string{"items", "1"}, int64(10), true},
		{"map inside slice", []string{"items", "2", "deep"}, "value", true},
		{"slice index out of range", []string{"items", "99"}, nil, false},
		{"negative slice index", []string{"items", "-1"}, nil, false},
		{"non-integer slice index", []string{"items", "not_an_int"}, nil, false},
		{"descend into scalar", []string{"str", "deeper"}, nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			steps, err := CompilePath(&policyv1alpha1.AttributePath{Path: tt.path}, "test")
			require.NoError(t, err)
			got, ok := EvalPath(m, steps)
			assert.Equal(t, tt.ok, ok)
			if tt.ok {
				assert.Equal(t, tt.want, got)
			}
		})
	}

	t.Run("whole slice", func(t *testing.T) {
		steps, err := CompilePath(&policyv1alpha1.AttributePath{Path: []string{"items"}}, "test")
		require.NoError(t, err)
		got, ok := EvalPath(m, steps)
		require.True(t, ok)
		assert.IsType(t, []any{}, got)
	})

	t.Run("zero map", func(t *testing.T) {
		steps, err := CompilePath(&policyv1alpha1.AttributePath{Path: []string{"str"}}, "test")
		require.NoError(t, err)
		_, ok := EvalPath(pcommon.Map{}, steps)
		assert.False(t, ok)
	})

	t.Run("no steps", func(t *testing.T) {
		_, ok := EvalPath(m, nil)
		assert.False(t, ok)
	})
}

func TestValueToAny(t *testing.T) {
	mapVal := pcommon.NewValueMap()
	mapVal.Map().PutStr("k", "v")
	// Maps have no plain Go analogue the predicates can compare, so they fall
	// back to their string rendering.
	assert.NotEmpty(t, ValueToAny(mapVal))

	sliceVal := pcommon.NewValueSlice()
	sliceVal.Slice().AppendEmpty().SetStr("a")
	sliceVal.Slice().AppendEmpty().SetInt(2)
	assert.Equal(t, []any{"a", int64(2)}, ValueToAny(sliceVal))

	assert.Equal(t, "", ValueToAny(pcommon.NewValueEmpty()))
}
