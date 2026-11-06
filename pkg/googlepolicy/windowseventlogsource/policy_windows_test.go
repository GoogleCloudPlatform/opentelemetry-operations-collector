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
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPolicy_Windows_CaseFolding_NonASCII(t *testing.T) {
	// Verify that Windows Win32 LCMapStringEx handles ASCII case pairs correctly.
	assert.Equal(t, "SECURITY", caseFoldChannel("Security"))
	assert.Equal(t, "SECURITY", caseFoldChannel("security"))
	assert.Equal(t, "SYSTEM", caseFoldChannel("System"))
	assert.Equal(t, "APPLICATION", caseFoldChannel("Application"))

	// Verify that Windows invariant ordinal uppercase does not fold special non-ASCII casing pairs,
	// preserving the exact channel identity used by the Win32 Event Log service and CompareStringOrdinal.
	assert.Equal(t, "ſECURITY", caseFoldChannel("ſecurity"))
	assert.Equal(t, "ıD", caseFoldChannel("ıd"))
	assert.Equal(t, "ΟΔΟς", caseFoldChannel("ΟΔΟς"))
	assert.Equal(t, "ΟΔΟΣ", caseFoldChannel("ΟΔΟΣ"))

	// Verify that SanitizeChannelName produces identical receiver IDs for ASCII casing pairs on Windows
	assert.Equal(t, SanitizeChannelName("Security"), SanitizeChannelName("security"))
	assert.Equal(t, SanitizeChannelName("Security"), SanitizeChannelName("SECURITY"))
	assert.Equal(t, SanitizeChannelName("System"), SanitizeChannelName("SYSTEM"))
	assert.Equal(t, SanitizeChannelName("Application"), SanitizeChannelName("application"))

	// Non-ASCII channels remain distinct under Windows OS ordinal casing
	assert.NotEqual(t, SanitizeChannelName("Security"), SanitizeChannelName("ſecurity"))
	assert.NotEqual(t, SanitizeChannelName("id"), SanitizeChannelName("ıd"))
	assert.NotEqual(t, SanitizeChannelName("ΟΔΟΣ"), SanitizeChannelName("ΟΔΟς"))
}
