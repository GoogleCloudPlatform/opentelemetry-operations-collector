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
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	expiredCtx, cancelExpired := context.WithDeadline(context.Background(), time.Unix(0, 0))
	defer cancelExpired()

	tests := []struct {
		name     string
		ctx      context.Context // nil means context.Background().
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
			err:      errors.New("rpc error: code = ResourceExhausted desc = Quota exceeded"),
			expected: true,
		},
		{
			name:     "internal error encountered",
			err:      errors.New("rpc error: code = Internal desc = Internal error encountered"),
			expected: true,
		},
		{
			name:     "context deadline exceeded with active parent context",
			err:      context.DeadlineExceeded,
			expected: true,
		},
		{
			name:     "wrapped context deadline exceeded with active parent context",
			err:      fmt.Errorf("iterating log entries: %w", context.DeadlineExceeded),
			expected: true,
		},
		{
			name:     "grpc deadline exceeded with active parent context",
			err:      status.Error(codes.DeadlineExceeded, "context deadline exceeded"),
			expected: true,
		},
		{
			name:     "wrapped grpc deadline exceeded with active parent context",
			err:      fmt.Errorf("iterating log entries: %w", status.Error(codes.DeadlineExceeded, "context deadline exceeded")),
			expected: true,
		},
		{
			name:     "context deadline exceeded with expired parent context",
			ctx:      expiredCtx,
			err:      context.DeadlineExceeded,
			expected: false,
		},
		{
			name:     "grpc deadline exceeded with canceled parent context",
			ctx:      canceledCtx,
			err:      status.Error(codes.DeadlineExceeded, "context deadline exceeded"),
			expected: false,
		},
		{
			name:     "quota error with canceled parent context",
			ctx:      canceledCtx,
			err:      errors.New("rpc error: code = ResourceExhausted desc = Quota exceeded"),
			expected: false,
		},
		{
			name:     "context canceled with active parent context",
			err:      context.Canceled,
			expected: false,
		},
		{
			name:     "grpc canceled with active parent context",
			err:      status.Error(codes.Canceled, "context canceled"),
			expected: false,
		},
		{
			name:     "permission denied",
			err:      status.Error(codes.PermissionDenied, "caller missing logging.entries.list"),
			expected: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := tc.ctx
			if ctx == nil {
				ctx = context.Background()
			}
			if got := shouldRetryHasMatchingLog(ctx, tc.err); got != tc.expected {
				t.Errorf("shouldRetryHasMatchingLog(ctx.Err()=%v, %v) = %v, want %v", ctx.Err(), tc.err, got, tc.expected)
			}
		})
	}
}
