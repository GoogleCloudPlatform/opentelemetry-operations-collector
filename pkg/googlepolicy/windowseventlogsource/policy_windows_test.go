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
	// Verify that Windows Win32 LCMapStringEx handles non-ASCII case pairs correctly.
	assert.Equal(t, "SECURITY", caseFoldChannel("Security"))
	assert.Equal(t, "SECURITY", caseFoldChannel("ſecurity"))
	assert.Equal(t, "ID", caseFoldChannel("ıd"))
	assert.Equal(t, "ΟΔΟΣ", caseFoldChannel("ΟΔΟς"))
	assert.Equal(t, "ΟΔΟΣ", caseFoldChannel("ΟΔΟΣ"))

	// Verify that SanitizeChannelName produces identical receiver IDs for these casing pairs on Windows
	assert.Equal(t, SanitizeChannelName("Security"), SanitizeChannelName("ſecurity"))
	assert.Equal(t, SanitizeChannelName("id"), SanitizeChannelName("ıd"))
	assert.Equal(t, SanitizeChannelName("ΟΔΟΣ"), SanitizeChannelName("ΟΔΟς"))
}
