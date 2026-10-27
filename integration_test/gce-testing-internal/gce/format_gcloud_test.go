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
	"bytes"
	"log"
	"strings"
	"testing"
)

func TestFormatEquivalentGcloud(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		expected string
	}{
		{
			name:     "simple command",
			args:     []string{"compute", "instances", "delete", "test-vm-1", "--quiet"},
			expected: "gcloud compute instances delete test-vm-1 --quiet",
		},
		{
			name:     "arguments with spaces and quotes",
			args:     []string{"compute", "instances", "add-metadata", "test-vm-1", "--metadata=ssh-keys=user:ssh-rsa AAAAB3..."},
			expected: `gcloud compute instances add-metadata test-vm-1 "--metadata=ssh-keys=user:ssh-rsa AAAAB3..."`,
		},
		{
			name:     "empty args",
			args:     []string{},
			expected: "gcloud",
		},
		{
			name:     "empty string argument",
			args:     []string{"compute", "instances", "describe", ""},
			expected: `gcloud compute instances describe ""`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := FormatEquivalentGcloud(tc.args...)
			if got != tc.expected {
				t.Errorf("FormatEquivalentGcloud(%v) = %q, want %q", tc.args, got, tc.expected)
			}
		})
	}
}

func TestLogEquivalentGcloud(t *testing.T) {
	var buf bytes.Buffer
	logger := log.New(&buf, "", 0)

	LogEquivalentGcloud(logger, "compute", "disks", "describe", "my-disk", "--zone=us-central1-a")
	output := strings.TrimSpace(buf.String())
	expected := "Equivalent command: gcloud compute disks describe my-disk --zone=us-central1-a"
	if output != expected {
		t.Errorf("LogEquivalentGcloud output = %q, want %q", output, expected)
	}

	// Should not panic with nil logger
	LogEquivalentGcloud(nil, "compute", "instances", "list")
}
