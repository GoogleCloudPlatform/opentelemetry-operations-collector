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
