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

	"github.com/NVIDIA/go-dcgm/pkg/dcgm"
)

// For each metric, we need to track:
type metricStats struct {
	// Timestamp (us)
	// Last value (for gauge metrics), as int or float
	lastFieldValue *dcgm.FieldValue_v2
	// Integrated rate (always int), as {unit-seconds,unit-microseconds}
	integratedRateSeconds      int64
	integratedRateMicroseconds int64
	// Cumulative value (always int)
	initialCumulativeValue int64
	cumulativeValue        int64
}

func asInt64(fieldValue dcgm.FieldValue_v2) (int64, bool) {
	// TODO: dcgm's Float64 and Int64 use undefined behavior
	switch fieldValue.FieldType {
	case dcgm.DCGM_FT_DOUBLE:
		return int64(fieldValue.Float64()), true
	case dcgm.DCGM_FT_INT64:
		return fieldValue.Int64(), true
	}
	return 0, false
}

func asFloat64(fieldValue dcgm.FieldValue_v2) (float64, bool) {
	switch fieldValue.FieldType {
	case dcgm.DCGM_FT_DOUBLE:
		return fieldValue.Float64(), true
	case dcgm.DCGM_FT_INT64:
		return float64(fieldValue.Int64()), true
	}
	return 0, false
}

func (m *metricStats) Update(fieldValue dcgm.FieldValue_v2) {
	ts := fieldValue.Ts
	intValue, intOk := asInt64(fieldValue)
	if !intOk {
		return
	}
	if m.lastFieldValue == nil {
		m.initialCumulativeValue = intValue
	} else {
		if m.lastFieldValue.Ts >= ts {
			return
		}
		m.cumulativeValue = intValue - m.initialCumulativeValue

		tsDelta := ts - m.lastFieldValue.Ts
		if fieldValue.FieldType == dcgm.DCGM_FT_DOUBLE {
			m.integratedRateMicroseconds += int64(float64(tsDelta) * fieldValue.Float64())
		} else {
			m.integratedRateMicroseconds += tsDelta * intValue
		}
		m.integratedRateSeconds += m.integratedRateMicroseconds / 1000000
		m.integratedRateMicroseconds %= 1000000
	}
	m.lastFieldValue = &fieldValue
}

type MetricsMap map[string]*metricStats

func (m MetricsMap) LastFloat64(name string) (float64, bool) {
	metric, ok := m[name]
	if ok && metric.lastFieldValue != nil {
		return asFloat64(*metric.lastFieldValue)
	}
	return 0, false
}
func (m MetricsMap) LastInt64(name string) (int64, bool) {
	metric, ok := m[name]
	if ok && metric.lastFieldValue != nil {
		return asInt64(*metric.lastFieldValue)
	}
	return 0, false
}
func (m MetricsMap) IntegratedRate(name string) (int64, bool) {
	metric, ok := m[name]
	if ok {
		return metric.integratedRateSeconds, true
	}
	return 0, false
}
func (m MetricsMap) CumulativeTotal(name string) (int64, bool) {
	metric, ok := m[name]
	if ok {
		return metric.cumulativeValue, true
	}
	return 0, false
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

func isValidValue(fieldValue dcgm.FieldValue_v2) error {
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
