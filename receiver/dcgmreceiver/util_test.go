// Copyright 2024 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

//go:build gpu
// +build gpu

package dcgmreceiver

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testRateIntegrator[V int64 | float64](t *testing.T) {
	origNowUnixMicro := nowUnixMicro
	nowUnixMicro = func() int64 { return 10 }
	defer func() { nowUnixMicro = origNowUnixMicro }()

	type P struct {
		ts int64
		v  V
	}
	p := func(ts int64, v V) P { return P{ts, v} }

	var ri rateIntegrator[V]

	ri.Reset()
	require.Equal(t, P{10, 0}, p(ri.Value()))
	// Ensure updates affect aggregated values.
	ri.Update(15, 1e6)
	assert.Equal(t, P{15, 5}, p(ri.Value()))
	// Ensure stale points are ignored.
	ri.Update(12, 1e8)
	assert.Equal(t, P{15, 5}, p(ri.Value()))
	ri.Update(15, 1.e8)
	assert.Equal(t, P{15, 5}, p(ri.Value()))
	// Ensure updates affect aggregated values.
	ri.Update(20, 2.e6)
	assert.Equal(t, P{20, 15}, p(ri.Value()))
	// Ensure zero rates don't change the aggregated value.
	ri.Update(25, 0)
	assert.Equal(t, P{25, 15}, p(ri.Value()))

	// Ensure the value is cleared on reset.
	ri.Reset()
	assert.Equal(t, P{10, 0}, p(ri.Value()))
}

func TestRateIntegratorInt64(t *testing.T) {
	testRateIntegrator[int64](t)
}

func TestRateIntegratorFloat64(t *testing.T) {
	testRateIntegrator[float64](t)
}

func testCumulativeTracker[V int64 | float64](t *testing.T) {
	origNowUnixMicro := nowUnixMicro
	nowUnixMicro = func() int64 { return 10 }
	defer func() { nowUnixMicro = origNowUnixMicro }()

	type P struct {
		ts int64
		v  V
	}
	p := func(ts int64, v V) P { return P{ts, v} }

	var ct cumulativeTracker[V]

	ct.Reset()
	require.Equal(t, P{0, 0}, p(ct.Baseline()))
	require.Equal(t, P{10, 0}, p(ct.Value()))
	// Ensure first updates sets the baseline.
	ct.Update(15, 50)
	require.Equal(t, P{15, 50}, p(ct.Baseline()))
	assert.Equal(t, P{15, 0}, p(ct.Value()))
	// Ensure updates affect values, but not the baseline.
	ct.Update(20, 80)
	assert.Equal(t, P{15, 50}, p(ct.Baseline()))
	assert.Equal(t, P{20, 30}, p(ct.Value()))
	// Ensure stale points are ignored.
	ct.Update(18, 1e8)
	assert.Equal(t, P{20, 30}, p(ct.Value()))
	ct.Update(20, 1e8)
	assert.Equal(t, P{20, 30}, p(ct.Value()))
	// Ensure updates affect values.
	ct.Update(25, 100)
	assert.Equal(t, P{25, 50}, p(ct.Value()))
	// Ensure same inputs don't affect values.
	ct.Update(30, 100)
	assert.Equal(t, P{30, 50}, p(ct.Value()))

	// Ensure the value and baseline are cleared on reset.
	ct.Reset()
	assert.Equal(t, P{0, 0}, p(ct.Baseline()))
	assert.Equal(t, P{10, 0}, p(ct.Value()))
}

func TestCumulativeTrackerInt64(t *testing.T) {
	testCumulativeTracker[int64](t)
}

func TestCumulativeTrackerFloat64(t *testing.T) {
	testCumulativeTracker[float64](t)
}

func TestDefaultMap(t *testing.T) {
	called := false
	m := newDefaultMap[int, int64](func() int64 {
		called = true
		return 8
	})
	_, ok := m.TryGet(3)
	assert.False(t, ok)
	assert.False(t, called)
	v := m.Get(3)
	assert.True(t, called)
	assert.Equal(t, int64(8), v)
	_, ok = m.TryGet(3)
	assert.True(t, ok)
}
