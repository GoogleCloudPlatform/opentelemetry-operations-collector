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

//go:build !windows
// +build !windows

package dcgmreceiver

import (
	"unsafe"

	"github.com/NVIDIA/go-dcgm/pkg/dcgm"
)

func (m *dcgmMetric) setFloat64(val float64) {
	*(*float64)(unsafe.Pointer(&m.value[0])) = val
}

func (m *dcgmMetric) asFloat64() float64 {
	return *(*float64)(unsafe.Pointer(&m.value[0]))
}

func (m *dcgmMetric) setInt64(val int64) {
	*(*int64)(unsafe.Pointer(&m.value[0])) = val
}

func (m *dcgmMetric) asInt64() int64 {
	return *(*int64)(unsafe.Pointer(&m.value[0]))
}

func isValidValue(fieldValue dcgm.FieldValue_v1) bool {
	switch fieldValue.FieldType {
	case dcgm.DCGM_FT_DOUBLE:
		switch v := fieldValue.Float64(); v {
		case dcgm.DCGM_FT_FP64_BLANK:
			return false
		case dcgm.DCGM_FT_FP64_NOT_FOUND:
			return false
		case dcgm.DCGM_FT_FP64_NOT_SUPPORTED:
			return false
		case dcgm.DCGM_FT_FP64_NOT_PERMISSIONED:
			return false
		}

	case dcgm.DCGM_FT_INT64:
		switch v := fieldValue.Int64(); v {
		case dcgm.DCGM_FT_INT32_BLANK:
			return false
		case dcgm.DCGM_FT_INT32_NOT_FOUND:
			return false
		case dcgm.DCGM_FT_INT32_NOT_SUPPORTED:
			return false
		case dcgm.DCGM_FT_INT32_NOT_PERMISSIONED:
			return false
		case dcgm.DCGM_FT_INT64_BLANK:
			return false
		case dcgm.DCGM_FT_INT64_NOT_FOUND:
			return false
		case dcgm.DCGM_FT_INT64_NOT_SUPPORTED:
			return false
		case dcgm.DCGM_FT_INT64_NOT_PERMISSIONED:
			return false
		}

	case dcgm.DCGM_FT_STRING:
		switch v := fieldValue.String(); v {
		case dcgm.DCGM_FT_STR_BLANK:
			return false
		case dcgm.DCGM_FT_STR_NOT_FOUND:
			return false
		case dcgm.DCGM_FT_STR_NOT_SUPPORTED:
			return false
		case dcgm.DCGM_FT_STR_NOT_PERMISSIONED:
			return false
		}

	default:
		return false
	}

	return true
}
