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

//go:build windows

package windowseventlogsource

import (
	"fmt"
	"syscall"
	"unsafe"
)

const isWindows = true

var (
	kernel32          = syscall.NewLazyDLL("kernel32.dll")
	procLCMapStringEx = kernel32.NewProc("LCMapStringEx")
	localeInvariant   = []uint16{0} // L"" null-terminated UTF-16 string representing LOCALE_NAME_INVARIANT
)

const (
	lcmapUppercase = 0x00000200
)

// caseFoldChannel folds channel casing using Windows invariant ordinal uppercase mapping.
// This matches the casing table used by the Windows Event Log service and CompareStringOrdinal.
func caseFoldChannel(s string) string {
	if s == "" {
		return ""
	}
	src, err := syscall.UTF16FromString(s)
	if err != nil {
		panic(fmt.Sprintf("windowseventlogsource: invalid UTF-16 channel string %q: %v", s, err))
	}
	if len(src) == 0 {
		return s
	}

	// Query required buffer length (including terminating null).
	r1, _, err1 := procLCMapStringEx.Call(
		uintptr(unsafe.Pointer(&localeInvariant[0])),
		uintptr(lcmapUppercase),
		uintptr(unsafe.Pointer(&src[0])),
		uintptr(len(src)),
		0,
		0,
		0,
		0,
		0,
	)
	if r1 == 0 {
		panic(fmt.Sprintf("windowseventlogsource: LCMapStringEx buffer query failed for %q: %v", s, err1))
	}

	dst := make([]uint16, r1)
	r2, _, err2 := procLCMapStringEx.Call(
		uintptr(unsafe.Pointer(&localeInvariant[0])),
		uintptr(lcmapUppercase),
		uintptr(unsafe.Pointer(&src[0])),
		uintptr(len(src)),
		uintptr(unsafe.Pointer(&dst[0])),
		uintptr(len(dst)),
		0,
		0,
		0,
	)
	if r2 == 0 {
		panic(fmt.Sprintf("windowseventlogsource: LCMapStringEx mapping failed for %q: %v", s, err2))
	}
	return syscall.UTF16ToString(dst)
}
