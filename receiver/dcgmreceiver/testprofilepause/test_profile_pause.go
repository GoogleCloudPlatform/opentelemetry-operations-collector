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

// Note: The DCGM library should be loaded to find the symbols

//go:build gpu
// +build gpu

package testprofilepause

/*
#include <stdint.h>
typedef uintptr_t dcgmHandle_t;
typedef enum dcgmReturn_enum { DCGM_ST_OK = 0 } dcgmReturn_t;
dcgmReturn_t dcgmProfPause(dcgmHandle_t pDcgmHandle);
dcgmReturn_t dcgmProfResume(dcgmHandle_t pDcgmHandle);
*/
import "C"
import (
	"fmt"
	_ "unsafe"

	"github.com/NVIDIA/go-dcgm/pkg/dcgm"
)

type dcgmHandle struct{ handle C.dcgmHandle_t }

//go:linkname handle github.com/NVIDIA/go-dcgm/pkg/dcgm.handle
var handle dcgmHandle

func PauseProfilingMetrics() {
	result := C.dcgmProfPause(handle.handle)
	if result != 0 {
		fmt.Printf("CUDA version %d", dcgm.DCGM_FI_CUDA_DRIVER_VERSION)
		fmt.Printf("Failed to pause profiling (result %d)\n", result)
	}
}

func ResumeProfilingMetrics() {
	result := C.dcgmProfResume(handle.handle)
	if result != 0 {
		fmt.Printf("Failed to resume profiling (result %d)\n", result)
	}
}
