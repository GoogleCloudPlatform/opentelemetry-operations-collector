// Copyright 2026 Google LLC
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

package gce

import (
	"errors"
	"testing"
)

func TestShouldRetryStartVM(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "quota error",
			err:      errors.New("Quotas exceeded for cpu"),
			expected: true,
		},
		{
			name:     "currently unavailable stockout",
			err:      errors.New("A t2a-standard-2 VM instance is currently unavailable in the us-central1-a zone"),
			expected: true,
		},
		{
			name:     "zone resource pool exhausted",
			err:      errors.New("ERROR: (gcloud.compute.instances.start) --- code: ZONE_RESOURCE_POOL_EXHAUSTED_WITH_DETAILS"),
			expected: true,
		},
		{
			name:     "internal error",
			err:      errors.New("Internal error encountered while processing request"),
			expected: true,
		},
		{
			name:     "database is locked concurrency error",
			err:      errors.New("sqlite3 error: database is locked"),
			expected: true,
		},
		{
			name:     "non-retryable error",
			err:      errors.New("instance test-vm not found in zone us-central1-a"),
			expected: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			actual := shouldRetryStartVM(tc.err)
			if actual != tc.expected {
				t.Errorf("shouldRetryStartVM(%v) = %v; want %v", tc.err, actual, tc.expected)
			}
		})
	}
}
