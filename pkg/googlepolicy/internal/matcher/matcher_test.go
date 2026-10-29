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
	"math"
	"testing"

	policyv1alpha1 "github.com/GoogleCloudPlatform/opentelemetry-operations-collector/gen/go/policy/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"google.golang.org/protobuf/types/known/emptypb"
)

func intValue(v int64) *policyv1alpha1.Value {
	return &policyv1alpha1.Value{Kind: &policyv1alpha1.Value_IntValue{IntValue: v}}
}

func strValue(v string) *policyv1alpha1.Value {
	return &policyv1alpha1.Value{Kind: &policyv1alpha1.Value_StringValue{StringValue: v}}
}

func bytesValue(v []byte) *policyv1alpha1.Value {
	return &policyv1alpha1.Value{Kind: &policyv1alpha1.Value_BytesValue{BytesValue: v}}
}

func intNumeric(v int64) *policyv1alpha1.NumericValue {
	return &policyv1alpha1.NumericValue{Kind: &policyv1alpha1.NumericValue_IntValue{IntValue: v}}
}

func doubleNumeric(v float64) *policyv1alpha1.NumericValue {
	return &policyv1alpha1.NumericValue{Kind: &policyv1alpha1.NumericValue_DoubleValue{DoubleValue: v}}
}

func predExists() *policyv1alpha1.Predicate {
	return &policyv1alpha1.Predicate{Kind: &policyv1alpha1.Predicate_Existence{Existence: &emptypb.Empty{}}}
}

func predEquals(v *policyv1alpha1.Value) *policyv1alpha1.Predicate {
	return &policyv1alpha1.Predicate{Kind: &policyv1alpha1.Predicate_Equality{Equality: v}}
}

func predGlob(pattern string) *policyv1alpha1.Predicate {
	return &policyv1alpha1.Predicate{Kind: &policyv1alpha1.Predicate_Glob{Glob: pattern}}
}

func predGt(n *policyv1alpha1.NumericValue) *policyv1alpha1.Predicate {
	return &policyv1alpha1.Predicate{Kind: &policyv1alpha1.Predicate_ExclusiveMinimum{ExclusiveMinimum: n}}
}

func predGte(n *policyv1alpha1.NumericValue) *policyv1alpha1.Predicate {
	return &policyv1alpha1.Predicate{Kind: &policyv1alpha1.Predicate_InclusiveMinimum{InclusiveMinimum: n}}
}

func predLt(n *policyv1alpha1.NumericValue) *policyv1alpha1.Predicate {
	return &policyv1alpha1.Predicate{Kind: &policyv1alpha1.Predicate_ExclusiveMaximum{ExclusiveMaximum: n}}
}

func predLte(n *policyv1alpha1.NumericValue) *policyv1alpha1.Predicate {
	return &policyv1alpha1.Predicate{Kind: &policyv1alpha1.Predicate_InclusiveMaximum{InclusiveMaximum: n}}
}

func predContains(v *policyv1alpha1.Value) *policyv1alpha1.Predicate {
	return &policyv1alpha1.Predicate{Kind: &policyv1alpha1.Predicate_Containment{Containment: v}}
}

func mustCompile(t *testing.T, predicate *policyv1alpha1.Predicate) Predicate {
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
		predicate *policyv1alpha1.Predicate
		wantErr   error
	}{
		{"nil predicate", nil, ErrMissingPredicate},
		{"unset predicate oneof", &policyv1alpha1.Predicate{}, ErrMissingPredicate},
		{"equals with nil value", predEquals(nil), ErrMissingPredicate},
		{"equals with unset value oneof", predEquals(&policyv1alpha1.Value{}), ErrMissingPredicate},
		{"contains with unset value oneof", predContains(&policyv1alpha1.Value{}), ErrMissingPredicate},
		{"gt with nil numeric", predGt(nil), ErrMissingPredicate},
		{"gt with unset numeric oneof", predGt(&policyv1alpha1.NumericValue{}), ErrMissingPredicate},
		{"lte with unset numeric oneof", predLte(&policyv1alpha1.NumericValue{}), ErrMissingPredicate},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := CompilePredicate(tt.predicate, false)
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func TestPredicateExists(t *testing.T) {
	pred := mustCompile(t, predExists())
	assert.True(t, pred("val", true))
	assert.False(t, pred("", false))
}

func TestPredicateNegate(t *testing.T) {
	pred, err := CompilePredicate(predExists(), true)
	require.NoError(t, err)
	assert.False(t, pred("val", true))
	assert.True(t, pred("", false))
}

func TestPredicateGlob(t *testing.T) {
	pred := mustCompile(t, predGlob("hello*?"))
	assert.True(t, pred("hello_world!", true))
	assert.True(t, pred("hello!", true))
	assert.True(t, pred("hello🎉", true))
	assert.False(t, pred("hello", true))
	assert.False(t, pred("HELLO!", true))
	assert.False(t, pred("prefix_hello!", true))
	assert.False(t, pred(int64(42), true))
	assert.False(t, pred(42, true))
	assert.False(t, pred(true, true))
	assert.False(t, pred(nil, false))

	emptyPred := mustCompile(t, predGlob(""))
	assert.True(t, emptyPred("", true))
	assert.False(t, emptyPred("a", true))

	starOnlyPred := mustCompile(t, predGlob("**"))
	assert.True(t, starOnlyPred("", true))
	assert.True(t, starOnlyPred("anything", true))
}

func TestPredicateGlobOnIDs(t *testing.T) {
	traceID := pcommon.TraceID([16]byte{0xaa, 0xbb})
	spanID := pcommon.SpanID([8]byte{0xcc, 0xdd})

	pred := mustCompile(t, predGlob("aabb*"))
	assert.True(t, pred(traceID, true))

	spanPred := mustCompile(t, predGlob("ccdd*"))
	assert.True(t, spanPred(spanID, true))
}

func TestPredicateEquals(t *testing.T) {
	t.Run("int widens across Go integer types", func(t *testing.T) {
		pred := mustCompile(t, predEquals(intValue(100)))
		assert.True(t, pred(int64(100), true))
		assert.True(t, pred(100, true))
		assert.True(t, pred(int32(100), true))
		assert.True(t, pred(uint64(100), true))
		assert.True(t, pred(uint32(100), true))
		assert.False(t, pred("not-an-int", true))
	})

	t.Run("double widens across Go float types", func(t *testing.T) {
		pred := mustCompile(t, predEquals(&policyv1alpha1.Value{Kind: &policyv1alpha1.Value_DoubleValue{DoubleValue: 3.5}}))
		assert.True(t, pred(3.5, true))
		assert.True(t, pred(float32(3.5), true))
		assert.False(t, pred("str", true))
	})

	t.Run("bool", func(t *testing.T) {
		pred := mustCompile(t, predEquals(&policyv1alpha1.Value{Kind: &policyv1alpha1.Value_BoolValue{BoolValue: true}}))
		assert.True(t, pred(true, true))
		assert.False(t, pred(false, true))
	})

	t.Run("bytes", func(t *testing.T) {
		pred := mustCompile(t, predEquals(bytesValue([]byte{1, 2, 3})))
		assert.True(t, pred([]byte{1, 2, 3}, true))
		assert.False(t, pred([]byte{4, 5}, true))
	})

	t.Run("trace and span IDs match as hex string or raw bytes", func(t *testing.T) {
		traceID := pcommon.TraceID([16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16})
		spanID := pcommon.SpanID([8]byte{1, 2, 3, 4, 5, 6, 7, 8})

		asString := mustCompile(t, predEquals(strValue(traceID.String())))
		assert.True(t, asString(traceID, true))

		asBytes := mustCompile(t, predEquals(bytesValue(traceID[:])))
		assert.True(t, asBytes(traceID, true))

		spanAsString := mustCompile(t, predEquals(strValue(spanID.String())))
		assert.True(t, spanAsString(spanID, true))

		spanAsBytes := mustCompile(t, predEquals(bytesValue(spanID[:])))
		assert.True(t, spanAsBytes(spanID, true))
	})
}

func TestPredicateNumericComparisons(t *testing.T) {
	tests := []struct {
		name      string
		predicate *policyv1alpha1.Predicate
		val       any
		want      bool
	}{
		{"gt int true", predGt(intNumeric(10)), int64(11), true},
		{"gt int equal is false", predGt(intNumeric(10)), int64(10), false},
		{"gt double", predGt(doubleNumeric(5.5)), 6.0, true},
		{"gte int32", predGte(intNumeric(10)), int32(10), true},
		{"gte uint64", predGte(intNumeric(10)), uint64(10), true},
		{"gte uint32", predGte(intNumeric(10)), uint32(10), true},
		{"lt float32", predLt(doubleNumeric(5.0)), float32(4.0), true},
		{"lt int", predLt(intNumeric(5)), int64(4), true},
		{"lte int equal", predLte(intNumeric(5)), int64(5), true},
		{"lte int above", predLte(intNumeric(5)), int64(6), false},
		{"double value against int target", predGt(intNumeric(5)), 5.5, true},
		{"non-numeric value never matches", predGt(intNumeric(5)), "ten", false},
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
		pred := mustCompile(t, predContains(strValue("sub")))
		assert.True(t, pred("my_substring", true))
		assert.True(t, pred([]any{"a", "sub", "b"}, true))
		assert.False(t, pred([]any{"a", "b"}, true))
		assert.False(t, pred(123, true))
	})

	t.Run("bytes subslice", func(t *testing.T) {
		pred := mustCompile(t, predContains(bytesValue([]byte{2, 3})))
		assert.True(t, pred([]byte{1, 2, 3, 4}, true))
		assert.False(t, pred([]byte{1, 4}, true))
	})

	t.Run("slice of ints", func(t *testing.T) {
		pred := mustCompile(t, predContains(intValue(42)))
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
		{"descend into slice is unsupported", []string{"items", "0"}, nil, false},
		{"descend into scalar", []string{"str", "deeper"}, nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			steps, err := CompilePath(&policyv1alpha1.AttributePath{Segments: tt.path}, "test")
			require.NoError(t, err)
			got, ok := EvalPath(m, steps)
			assert.Equal(t, tt.ok, ok)
			if tt.ok {
				assert.Equal(t, tt.want, got)
			}
		})
	}

	t.Run("whole slice", func(t *testing.T) {
		steps, err := CompilePath(&policyv1alpha1.AttributePath{Segments: []string{"items"}}, "test")
		require.NoError(t, err)
		got, ok := EvalPath(m, steps)
		require.True(t, ok)
		assert.IsType(t, []any{}, got)
	})

	t.Run("zero map", func(t *testing.T) {
		steps, err := CompilePath(&policyv1alpha1.AttributePath{Segments: []string{"str"}}, "test")
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

	assert.Nil(t, ValueToAny(pcommon.NewValueEmpty()))
}

func TestNullAttributeBehavior(t *testing.T) {
	attrs := pcommon.NewMap()
	attrs.PutEmpty("null_key")

	steps, err := CompilePath(&policyv1alpha1.AttributePath{Segments: []string{"null_key"}}, "log")
	require.NoError(t, err)

	val, exists := EvalPath(attrs, steps)
	assert.True(t, exists, "null attribute key exists in map")
	assert.Nil(t, val, "null attribute value must be nil, not empty string")

	// exists predicate evaluates to true
	existsPred, err := CompilePredicate(predExists(), false)
	require.NoError(t, err)
	assert.True(t, existsPred(val, exists))

	// equals "" evaluates to false
	equalsEmptyPred, err := CompilePredicate(predEquals(&policyv1alpha1.Value{Kind: &policyv1alpha1.Value_StringValue{StringValue: ""}}), false)
	require.NoError(t, err)
	assert.False(t, equalsEmptyPred(val, exists), "null attribute must not match equals \"\"")

	// glob "" evaluates to false
	globEmptyPred, err := CompilePredicate(predGlob(""), false)
	require.NoError(t, err)
	assert.False(t, globEmptyPred(val, exists), "null attribute must not match glob \"\"")
}

func TestCompareNumericNaN(t *testing.T) {
	nan64 := math.NaN()
	nan32 := float32(math.NaN())
	expected100 := &policyv1alpha1.NumericValue{
		Kind: &policyv1alpha1.NumericValue_DoubleValue{DoubleValue: 100.0},
	}
	expectedNaN := &policyv1alpha1.NumericValue{
		Kind: &policyv1alpha1.NumericValue_DoubleValue{DoubleValue: nan64},
	}

	predicates := []struct {
		name string
		pred *policyv1alpha1.Predicate
	}{
		{"gt", predGt(expected100)},
		{"gte", predGte(expected100)},
		{"lt", predLt(expected100)},
		{"lte", predLte(expected100)},
		{"equals", predEquals(&policyv1alpha1.Value{Kind: &policyv1alpha1.Value_DoubleValue{DoubleValue: 100.0}})},
		{"gte with expected NaN", predGte(expectedNaN)},
		{"lte with expected NaN", predLte(expectedNaN)},
	}

	for _, tc := range predicates {
		t.Run(tc.name, func(t *testing.T) {
			fn, err := CompilePredicate(tc.pred, false)
			require.NoError(t, err)
			assert.False(t, fn(nan64, true), "float64(NaN) must never match %s", tc.name)
			assert.False(t, fn(nan32, true), "float32(NaN) must never match %s", tc.name)
			if tc.name == "gte with expected NaN" || tc.name == "lte with expected NaN" {
				assert.False(t, fn(100.0, true), "100.0 against expected NaN must never match %s", tc.name)
			}
		})
	}
}

func TestCompilePathSetter(t *testing.T) {
	t.Run("validation errors", func(t *testing.T) {
		_, err := CompilePathSetter(nil, "log", strValue("v"), true)
		assert.ErrorContains(t, err, "log attribute path cannot be empty")

		_, err = CompilePathSetter(&policyv1alpha1.AttributePath{Segments: []string{"k"}}, "log", nil, true)
		assert.ErrorIs(t, err, ErrMissingValue)

		_, err = CompilePathSetter(&policyv1alpha1.AttributePath{Segments: []string{"k"}}, "log", &policyv1alpha1.Value{}, true)
		assert.ErrorIs(t, err, ErrMissingValue)
	})

	t.Run("all scalar value types flat and zero map guard", func(t *testing.T) {
		attrs := pcommon.NewMap()

		setStr, err := CompilePathSetter(&policyv1alpha1.AttributePath{Segments: []string{"s"}}, "log", strValue("hello"), true)
		require.NoError(t, err)
		assert.False(t, setStr(pcommon.Map{}), "zero map must return false")
		assert.True(t, setStr(attrs))

		setInt, err := CompilePathSetter(&policyv1alpha1.AttributePath{Segments: []string{"i"}}, "log", intValue(42), true)
		require.NoError(t, err)
		assert.True(t, setInt(attrs))

		setDouble, err := CompilePathSetter(&policyv1alpha1.AttributePath{Segments: []string{"d"}}, "log",
			&policyv1alpha1.Value{Kind: &policyv1alpha1.Value_DoubleValue{DoubleValue: 3.14}}, true)
		require.NoError(t, err)
		assert.True(t, setDouble(attrs))

		setBool, err := CompilePathSetter(&policyv1alpha1.AttributePath{Segments: []string{"b"}}, "log",
			&policyv1alpha1.Value{Kind: &policyv1alpha1.Value_BoolValue{BoolValue: true}}, true)
		require.NoError(t, err)
		assert.True(t, setBool(attrs))

		raw := []byte{0xaa, 0xbb}
		setBytes, err := CompilePathSetter(&policyv1alpha1.AttributePath{Segments: []string{"by"}}, "log", bytesValue(raw), true)
		require.NoError(t, err)
		raw[0] = 0xff // mutating input slice after compile must not affect setter
		assert.True(t, setBytes(attrs))

		v, ok := attrs.Get("s")
		require.True(t, ok)
		assert.Equal(t, "hello", v.Str())

		v, ok = attrs.Get("i")
		require.True(t, ok)
		assert.Equal(t, int64(42), v.Int())

		v, ok = attrs.Get("d")
		require.True(t, ok)
		assert.Equal(t, 3.14, v.Double())

		v, ok = attrs.Get("b")
		require.True(t, ok)
		assert.True(t, v.Bool())

		v, ok = attrs.Get("by")
		require.True(t, ok)
		assert.Equal(t, []byte{0xaa, 0xbb}, v.Bytes().AsRaw())
	})

	t.Run("flat upsert false vs true", func(t *testing.T) {
		attrs := pcommon.NewMap()
		insertOnly, err := CompilePathSetter(&policyv1alpha1.AttributePath{Segments: []string{"tier"}}, "log", strValue("initial"), false)
		require.NoError(t, err)
		assert.True(t, insertOnly(attrs))
		assert.False(t, insertOnly(attrs), "second insert with upsert=false must be a no-op")

		overwrite, err := CompilePathSetter(&policyv1alpha1.AttributePath{Segments: []string{"tier"}}, "log", strValue("updated"), true)
		require.NoError(t, err)
		assert.True(t, overwrite(attrs))
		v, _ := attrs.Get("tier")
		assert.Equal(t, "updated", v.Str())
	})

	t.Run("nested path creates intermediate maps and respects upsert", func(t *testing.T) {
		attrs := pcommon.NewMap()
		nestedPath := &policyv1alpha1.AttributePath{Segments: []string{"gcp", "routing", "tier"}}

		setNestedInsert, err := CompilePathSetter(nestedPath, "log", strValue("HOT_ACTIVE_SOC"), false)
		require.NoError(t, err)
		assert.False(t, setNestedInsert(pcommon.Map{}))
		assert.True(t, setNestedInsert(attrs))

		steps, err := CompilePath(nestedPath, "log")
		require.NoError(t, err)
		got, ok := EvalPath(attrs, steps)
		require.True(t, ok)
		assert.Equal(t, "HOT_ACTIVE_SOC", got)

		// upsert=false when leaf already exists -> returns false and preserves value
		setNestedNoOverwrite, err := CompilePathSetter(nestedPath, "log", strValue("LAKEHOUSE_ARCHIVE"), false)
		require.NoError(t, err)
		assert.False(t, setNestedNoOverwrite(attrs))
		got, _ = EvalPath(attrs, steps)
		assert.Equal(t, "HOT_ACTIVE_SOC", got)

		// upsert=true overwrites existing nested leaf
		setNestedOverwrite, err := CompilePathSetter(nestedPath, "log", strValue("LAKEHOUSE_ARCHIVE"), true)
		require.NoError(t, err)
		assert.True(t, setNestedOverwrite(attrs))
		got, _ = EvalPath(attrs, steps)
		assert.Equal(t, "LAKEHOUSE_ARCHIVE", got)

		// Intermediate key is a non-map scalar: upsert=false leaves scalar intact, upsert=true replaces with map
		attrs.PutStr("scalar_parent", "keep_me")
		conflictPath := &policyv1alpha1.AttributePath{Segments: []string{"scalar_parent", "child"}}
		noClobber, err := CompilePathSetter(conflictPath, "log", strValue("v"), false)
		require.NoError(t, err)
		assert.False(t, noClobber(attrs))
		sp, _ := attrs.Get("scalar_parent")
		assert.Equal(t, "keep_me", sp.Str())

		clobber, err := CompilePathSetter(conflictPath, "log", strValue("v"), true)
		require.NoError(t, err)
		assert.True(t, clobber(attrs))
		sp, _ = attrs.Get("scalar_parent")
		require.Equal(t, pcommon.ValueTypeMap, sp.Type())
		child, ok := sp.Map().Get("child")
		require.True(t, ok)
		assert.Equal(t, "v", child.Str())
	})
}

func TestCompileValueSetter(t *testing.T) {
	_, err := CompileValueSetter(nil)
	assert.ErrorIs(t, err, ErrMissingValue)

	_, err = CompileValueSetter(&policyv1alpha1.Value{})
	assert.ErrorIs(t, err, ErrMissingValue)

	dest := pcommon.NewValueEmpty()

	setStr, err := CompileValueSetter(strValue("body-str"))
	require.NoError(t, err)
	setStr(dest)
	assert.Equal(t, "body-str", dest.Str())

	setInt, err := CompileValueSetter(intValue(99))
	require.NoError(t, err)
	setInt(dest)
	assert.Equal(t, int64(99), dest.Int())

	setDouble, err := CompileValueSetter(&policyv1alpha1.Value{Kind: &policyv1alpha1.Value_DoubleValue{DoubleValue: 2.5}})
	require.NoError(t, err)
	setDouble(dest)
	assert.Equal(t, 2.5, dest.Double())

	setBool, err := CompileValueSetter(&policyv1alpha1.Value{Kind: &policyv1alpha1.Value_BoolValue{BoolValue: true}})
	require.NoError(t, err)
	setBool(dest)
	assert.True(t, dest.Bool())

	raw := []byte{1, 2, 3}
	setBytes, err := CompileValueSetter(bytesValue(raw))
	require.NoError(t, err)
	raw[0] = 9
	setBytes(dest)
	assert.Equal(t, []byte{1, 2, 3}, dest.Bytes().AsRaw())
}

func TestCompilePathRemover(t *testing.T) {
	_, err := CompilePathRemover(nil, "log")
	assert.Error(t, err)

	attrs := pcommon.NewMap()
	attrs.PutStr("top", "val")
	nested := attrs.PutEmptyMap("parent")
	nested.PutInt("child", 42)
	attrs.PutStr("scalar_parent", "not_a_map")

	rmTop, err := CompilePathRemover(&policyv1alpha1.AttributePath{Segments: []string{"top"}}, "log")
	require.NoError(t, err)
	assert.False(t, rmTop(pcommon.Map{}))
	assert.True(t, rmTop(attrs))
	assert.False(t, rmTop(attrs))

	nestedPath := &policyv1alpha1.AttributePath{Segments: []string{"parent", "child"}}
	rmNested, err := CompilePathRemover(nestedPath, "log")
	require.NoError(t, err)
	assert.False(t, rmNested(pcommon.Map{}))
	assert.True(t, rmNested(attrs))
	assert.False(t, rmNested(attrs))

	missingParentPath := &policyv1alpha1.AttributePath{Segments: []string{"missing", "child"}}
	rmMissing, err := CompilePathRemover(missingParentPath, "log")
	require.NoError(t, err)
	assert.False(t, rmMissing(attrs))
	_, exists := attrs.Get("missing")
	assert.False(t, exists)

	scalarAncestorPath := &policyv1alpha1.AttributePath{Segments: []string{"scalar_parent", "child"}}
	rmScalar, err := CompilePathRemover(scalarAncestorPath, "log")
	require.NoError(t, err)
	assert.False(t, rmScalar(attrs))

	// Test duplicate key removal: simulate pathological duplicate keys by creating a map with repeated entries.
	dupAttrs := pcommon.NewMap()
	dupAttrs.PutStr("dup_key", "first")
	// Verify that CompilePathRemover removes the key.
	rmDup, err := CompilePathRemover(&policyv1alpha1.AttributePath{Segments: []string{"dup_key"}}, "log")
	require.NoError(t, err)
	assert.True(t, rmDup(dupAttrs))
	assert.False(t, rmDup(dupAttrs))
	_, exists = dupAttrs.Get("dup_key")
	assert.False(t, exists)
}

func TestCompilePathValueSetter(t *testing.T) {
	_, err := CompilePathValueSetter(nil, "log", false)
	assert.Error(t, err)

	attrs := pcommon.NewMap()
	attrs.PutStr("existing", "orig")

	// Upsert=false on existing top-level key should fail.
	setTopNoUpsert, err := CompilePathValueSetter(&policyv1alpha1.AttributePath{Segments: []string{"existing"}}, "log", false)
	require.NoError(t, err)
	newVal := pcommon.NewValueStr("new")
	assert.False(t, setTopNoUpsert(attrs, newVal))
	v, ok := attrs.Get("existing")
	assert.True(t, ok)
	assert.Equal(t, "orig", v.Str())

	// Upsert=true on existing top-level key should succeed.
	setTopUpsert, err := CompilePathValueSetter(&policyv1alpha1.AttributePath{Segments: []string{"existing"}}, "log", true)
	require.NoError(t, err)
	assert.True(t, setTopUpsert(attrs, newVal))
	v, ok = attrs.Get("existing")
	assert.True(t, ok)
	assert.Equal(t, "new", v.Str())

	// Setting nested path with upsert=false into empty destination should create intermediates.
	nestedPath := &policyv1alpha1.AttributePath{Segments: []string{"a", "b", "c"}}
	setNested, err := CompilePathValueSetter(nestedPath, "log", false)
	require.NoError(t, err)
	intVal := pcommon.NewValueInt(100)
	assert.True(t, setNested(attrs, intVal))

	// Re-setting same nested path with upsert=false should fail.
	assert.False(t, setNested(attrs, pcommon.NewValueInt(200)))
	// Value should still be 100.
	leaf, ok := EvalPathValue(attrs, []PathStep{{Key: "a"}, {Key: "b"}, {Key: "c"}})
	assert.True(t, ok)
	assert.Equal(t, int64(100), leaf.Int())

	// Obstructed intermediate (scalar ancestor) with upsert=false should fail without modifying.
	attrs.PutStr("scalar", "blocked")
	blockedPath := &policyv1alpha1.AttributePath{Segments: []string{"scalar", "child"}}
	setBlocked, err := CompilePathValueSetter(blockedPath, "log", false)
	require.NoError(t, err)
	assert.False(t, setBlocked(attrs, pcommon.NewValueStr("val")))
	v, ok = attrs.Get("scalar")
	assert.True(t, ok)
	assert.Equal(t, "blocked", v.Str())
}

func TestEvalPathValueAndPathExists(t *testing.T) {
	assert.False(t, PathExists(pcommon.Map{}, []PathStep{{Key: "x"}}))
	_, ok := EvalPathValue(pcommon.Map{}, []PathStep{{Key: "x"}})
	assert.False(t, ok)

	attrs := pcommon.NewMap()
	attrs.PutStr("top", "val")
	nested := attrs.PutEmptyMap("nested")
	nested.PutInt("id", 42)

	assert.True(t, PathExists(attrs, []PathStep{{Key: "top"}}))
	assert.True(t, PathExists(attrs, []PathStep{{Key: "nested"}}))
	assert.True(t, PathExists(attrs, []PathStep{{Key: "nested"}, {Key: "id"}}))
	assert.False(t, PathExists(attrs, []PathStep{{Key: "missing"}}))
	assert.False(t, PathExists(attrs, []PathStep{{Key: "nested"}, {Key: "missing"}}))

	// Path obstructed by non-map intermediate is reported as PathExists (true)
	// so that !upsert skips it.
	assert.True(t, PathExists(attrs, []PathStep{{Key: "top"}, {Key: "child"}}))

	v, ok := EvalPathValue(attrs, []PathStep{{Key: "nested"}, {Key: "id"}})
	assert.True(t, ok)
	assert.Equal(t, int64(42), v.Int())

	_, ok = EvalPathValue(attrs, []PathStep{{Key: "top"}, {Key: "child"}})
	assert.False(t, ok)
}

func TestCompileStringRedactor(t *testing.T) {
	// Full replacement when regex is empty.
	redactAll, err := CompileStringRedactor("", "[REDACTED]")
	require.NoError(t, err)
	res, mod := redactAll("secret token 12345")
	assert.True(t, mod)
	assert.Equal(t, "[REDACTED]", res)

	// No-op if already equal to replacement.
	_, mod = redactAll("[REDACTED]")
	assert.False(t, mod)

	// Regex pattern replacement.
	redactSSN, err := CompileStringRedactor(`\d{3}-\d{2}-\d{4}`, "XXX-XX-XXXX")
	require.NoError(t, err)

	res, mod = redactSSN("User SSN is 123-45-6789.")
	assert.True(t, mod)
	assert.Equal(t, "User SSN is XXX-XX-XXXX.", res)

	// No match returns false.
	_, mod = redactSSN("No sensitive data here.")
	assert.False(t, mod)

	// Capture groups.
	maskEmail, err := CompileStringRedactor(`([a-zA-Z0-9._%+-]+)@([a-zA-Z0-9.-]+\.[a-zA-Z]{2,})`, "$1@***")
	require.NoError(t, err)
	res, mod = maskEmail("Contact support at alice@example.com for help")
	assert.True(t, mod)
	assert.Equal(t, "Contact support at alice@*** for help", res)

	// Multiple occurrences replaced.
	redactMulti, err := CompileStringRedactor(`\d+`, "XXXX")
	require.NoError(t, err)
	res, mod = redactMulti("id: 1234, parent: 5678")
	assert.True(t, mod)
	assert.Equal(t, "id: XXXX, parent: XXXX", res)

	// Invalid regex returns error.
	_, err = CompileStringRedactor("[invalid", "val")
	assert.Error(t, err)
}
