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

package gce

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestBuildLogFilter(t *testing.T) {
	vm := &VM{Project: "test-project", ID: 12345}
	start := time.Date(2026, 11, 18, 20, 0, 0, 0, time.UTC)

	vmScoped := buildLogFilter(vm, true, ".*", start, `textPayload="hello"`)
	wantVM := `logName=~"projects/test-project/logs/.*" AND resource.labels.instance_id="12345" AND timestamp > "2026-11-18T20:00:00Z" AND textPayload="hello"`
	if vmScoped != wantVM {
		t.Errorf("buildLogFilter(includeInstanceID=true) = %q, want %q", vmScoped, wantVM)
	}

	projectScoped := buildLogFilter(vm, false, ".*", start, `resource.type="generic_node"`)
	if strings.Contains(projectScoped, "instance_id") {
		t.Errorf("buildLogFilter(includeInstanceID=false) unexpectedly included instance_id: %q", projectScoped)
	}
	wantProject := `logName=~"projects/test-project/logs/.*" AND timestamp > "2026-11-18T20:00:00Z" AND resource.type="generic_node"`
	if projectScoped != wantProject {
		t.Errorf("buildLogFilter(includeInstanceID=false) = %q, want %q", projectScoped, wantProject)
	}
}

func TestShouldRetryHasMatchingLog(t *testing.T) {
	if !shouldRetryHasMatchingLog(errors.New("rpc error: code = ResourceExhausted desc = Quota exceeded")) {
		t.Errorf("expected Quota error to be retriable")
	}
	if !shouldRetryHasMatchingLog(errors.New("rpc error: code = Internal desc = Internal error encountered")) {
		t.Errorf("expected Internal error encountered to be retriable")
	}
	if shouldRetryHasMatchingLog(errors.New("rpc error: code = PermissionDenied desc = caller missing logging.entries.list")) {
		t.Errorf("expected PermissionDenied not to be retriable")
	}
}
