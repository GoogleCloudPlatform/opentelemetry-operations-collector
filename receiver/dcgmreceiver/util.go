// Copyright 2023 Google LLC
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
	"fmt"
	"time"

	"github.com/NVIDIA/go-dcgm/pkg/dcgm"
)

var nowUnixMicro = func() int64 { return time.Now().UnixNano() / 1e3 }

// rateIntegrator converts timestamped values that represent rates into
// cumulative values. It assumes the rate stays constant since the last
// timestamp.
type rateIntegrator[V int64 | float64] struct {
	lastTimestamp    int64
	aggregatedRateUs V // the integration of the rate over microsecond timestamps.
}

func (ri *rateIntegrator[V]) Reset() {
	ri.lastTimestamp = nowUnixMicro()
	ri.aggregatedRateUs = V(0)
}

func (ri *rateIntegrator[V]) Update(ts int64, v V) {
	// Drop stale points.
	if ts <= ri.lastTimestamp {
		return
	}
	// v is the rate per second, and timestamps are in microseconds, so the
	// delta will be 1e6 times the actual increment.
	ri.aggregatedRateUs += v * V(ts-ri.lastTimestamp)
	ri.lastTimestamp = ts
}

func (ri *rateIntegrator[V]) Value() (int64, V) {
	return ri.lastTimestamp, ri.aggregatedRateUs / V(1e6)
}

type defaultMap[K comparable, V any] struct {
	m map[K]V
	f func() V
}

func newDefaultMap[K comparable, V any](f func() V) *defaultMap[K, V] {
	return &defaultMap[K, V]{
		m: make(map[K]V),
		f: f,
	}
}

func (m *defaultMap[K, V]) Get(k K) V {
	if v, ok := m.m[k]; ok {
		return v
	}
	v := m.f()
	m.m[k] = v
	return v
}

func (m *defaultMap[K, V]) TryGet(k K) (V, bool) {
	v, ok := m.m[k]
	return v, ok
}

var (
	errBlankValue       = fmt.Errorf("unspecified blank value")
	errDataNotFound     = fmt.Errorf("data not found")
	errNotSupported     = fmt.Errorf("field not supported")
	errPermissionDenied = fmt.Errorf("no permission to fetch value")
	errUnexpectedType   = fmt.Errorf("unexpected data type")
)

func (m *dcgmMetric) asFloat64() float64 {
	return m.value.(float64)
}

func (m *dcgmMetric) asInt64() int64 {
	return m.value.(int64)
}

func isValidValue(fieldValue dcgm.FieldValue_v1) error {
	switch fieldValue.FieldType {
	case dcgm.DCGM_FT_DOUBLE:
		switch v := fieldValue.Float64(); v {
		case dcgm.DCGM_FT_FP64_BLANK:
			return errBlankValue
		case dcgm.DCGM_FT_FP64_NOT_FOUND:
			return errDataNotFound
		case dcgm.DCGM_FT_FP64_NOT_SUPPORTED:
			return errNotSupported
		case dcgm.DCGM_FT_FP64_NOT_PERMISSIONED:
			return errPermissionDenied
		}

	case dcgm.DCGM_FT_INT64:
		switch v := fieldValue.Int64(); v {
		case dcgm.DCGM_FT_INT32_BLANK:
			return errBlankValue
		case dcgm.DCGM_FT_INT32_NOT_FOUND:
			return errDataNotFound
		case dcgm.DCGM_FT_INT32_NOT_SUPPORTED:
			return errNotSupported
		case dcgm.DCGM_FT_INT32_NOT_PERMISSIONED:
			return errPermissionDenied
		case dcgm.DCGM_FT_INT64_BLANK:
			return errBlankValue
		case dcgm.DCGM_FT_INT64_NOT_FOUND:
			return errDataNotFound
		case dcgm.DCGM_FT_INT64_NOT_SUPPORTED:
			return errNotSupported
		case dcgm.DCGM_FT_INT64_NOT_PERMISSIONED:
			return errPermissionDenied
		}

	// dcgm.DCGM_FT_STRING also exists but we don't expect it
	default:
		return errUnexpectedType
	}

	return nil
}
