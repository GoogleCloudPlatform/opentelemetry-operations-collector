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
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/cenkalti/backoff/v4"
	"google.golang.org/api/googleapi"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestShouldRetryCreateVM(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name: "502 HTML page",
			err: errors.New("Command failed: [gcloud beta compute instances create test-vm]\nexit status 1\n" +
				"stdout+stderr: ERROR: (gcloud.beta.compute.instances.create) Could not fetch resource:\n" +
				" - <!DOCTYPE html>\n<html lang=en>\n  <title>Error 502 (Server Error)!!1</title>\n" +
				"  <p><b>502.</b> <ins>That’s an error.</ins>"),
			expected: true,
		},
		{
			name:     "503 error",
			err:      errors.New("Error 503: Service Unavailable"),
			expected: true,
		},
		{
			name:     "quota error",
			err:      errors.New("Quota 'CPUS' exceeded. Limit: 24.0 in region us-central1."),
			expected: true,
		},
		{
			name:     "currently unavailable",
			err:      errors.New("The service is currently unavailable."),
			expected: true,
		},
		{
			name:     "ip assignment timeout",
			err:      errors.New("timed out waiting for instance \"test-vm\" to be assigned an IP address"),
			expected: true,
		},
		{
			name:     "instance ready timeout",
			err:      errors.New("timed out waiting for instance \"test-vm\" to be ready"),
			expected: true,
		},
		{
			name:     "zone resource pool exhausted stockout",
			err:      errors.New("ZONE_RESOURCE_POOL_EXHAUSTED: A t2a-standard-2 VM instance is currently unavailable in the us-central1-b zone"),
			expected: true,
		},
		{
			name:     "400 error",
			err:      errors.New("Error 400: Bad Request"),
			expected: false,
		},
		{
			name:     "404 error",
			err:      errors.New("Error 404: The resource 'projects/p/zones/z/instances/test-vm' was not found"),
			expected: false,
		},
		{
			name:     "googleapi 403 quota error",
			err:      &googleapi.Error{Code: 403, Message: "Quota exceeded"},
			expected: true,
		},
		{
			name:     "googleapi 429 rate limit error",
			err:      &googleapi.Error{Code: 429, Message: "Rate limit exceeded"},
			expected: true,
		},
		{
			name:     "googleapi 503 service unavailable",
			err:      &googleapi.Error{Code: 503, Message: "Backend unavailable"},
			expected: true,
		},
		{
			name:     "googleapi 504 gateway timeout",
			err:      &googleapi.Error{Code: 504, Message: "Gateway timeout"},
			expected: true,
		},
		{
			name:     "googleapi 400 bad request",
			err:      &googleapi.Error{Code: 400, Message: "Bad Request"},
			expected: false,
		},
		{
			name:     "grpc status unavailable",
			err:      status.Error(codes.Unavailable, "service unavailable"),
			expected: true,
		},
		{
			name:     "grpc status resource exhausted",
			err:      status.Error(codes.ResourceExhausted, "resource exhausted"),
			expected: true,
		},
		{
			name:     "grpc status invalid argument",
			err:      status.Error(codes.InvalidArgument, "invalid argument"),
			expected: false,
		},
	}

	options := VMOptions{ImageSpec: "debian-cloud:debian-12"}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			actual := shouldRetryCreateVM(tc.err, options)
			if actual != tc.expected {
				t.Errorf("shouldRetryCreateVM(%v) = %v; want %v", tc.err, actual, tc.expected)
			}
		})
	}
}

func TestIsRetriableLookupError(t *testing.T) {
	cancelledCtx, cancel := context.WithCancel(context.Background())
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
			name:     "not found",
			err:      status.Error(codes.NotFound, "metric descriptor not found"),
			expected: true,
		},
		{
			name:     "internal",
			err:      status.Error(codes.Internal, "internal error"),
			expected: true,
		},
		{
			name:     "resource exhausted",
			err:      status.Error(codes.ResourceExhausted, "quota exceeded"),
			expected: true,
		},
		{
			name:     "invalid iterator length",
			err:      ErrInvalidIteratorLength,
			expected: true,
		},
		{
			name:     "cancelled by the API",
			err:      status.Error(codes.Canceled, "CANCELLED"),
			expected: true,
		},
		{
			name:     "cancelled because our context was cancelled",
			ctx:      cancelledCtx,
			err:      status.Error(codes.Canceled, "context canceled"),
			expected: false,
		},
		{
			name:     "cancelled after our context's deadline passed",
			ctx:      expiredCtx,
			err:      status.Error(codes.Canceled, "CANCELLED"),
			expected: false,
		},
		{
			name:     "permission denied",
			err:      status.Error(codes.PermissionDenied, "permission denied"),
			expected: false,
		},
		{
			name:     "non-gRPC error",
			err:      errors.New("some error"),
			expected: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := tc.ctx
			if ctx == nil {
				ctx = context.Background()
			}
			actual := isRetriableLookupError(ctx, tc.err)
			if actual != tc.expected {
				t.Errorf("isRetriableLookupError(ctx with Err() = %v, %v) = %v; want %v", ctx.Err(), tc.err, actual, tc.expected)
			}
		})
	}
}

func TestShouldRetryCreateManagedInstanceGroupVM(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "ErrMIGTimeout sentinel error",
			err:      ErrMIGTimeout,
			expected: true,
		},
		{
			name:     "wrapped ErrMIGTimeout",
			err:      fmt.Errorf("waitForMIGStable failed: %w", ErrMIGTimeout),
			expected: true,
		},
		{
			name:     "Timeout waiting for stable string",
			err:      errors.New("Timeout while waiting for group to become stable."),
			expected: true,
		},
		{
			name:     "wrapped timeout waiting for stable",
			err:      fmt.Errorf("step 4 failed: %w", errors.New("Timeout while waiting for group to become stable.")),
			expected: true,
		},
		{
			name:     "quota error",
			err:      errors.New("Quota 'CPUS' exceeded. Limit: 24.0 in region us-central1."),
			expected: true,
		},
		{
			name:     "503 service unavailable",
			err:      status.Error(codes.Unavailable, "service unavailable"),
			expected: true,
		},
		{
			name:     "400 bad request",
			err:      status.Error(codes.InvalidArgument, "invalid argument"),
			expected: false,
		},
	}

	options := VMOptions{ImageSpec: "debian-cloud:debian-12"}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			actual := shouldRetryCreateManagedInstanceGroupVM(tc.err, options)
			if actual != tc.expected {
				t.Errorf("shouldRetryCreateManagedInstanceGroupVM(%v) = %v; want %v", tc.err, actual, tc.expected)
			}
		})
	}
}

func TestIsNotFound(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{"nil error", nil, false},
		{"googleapi 404", &googleapi.Error{Code: 404, Message: "Not Found"}, true},
		{"googleapi 500", &googleapi.Error{Code: 500, Message: "Internal Server Error"}, false},
		{"grpc codes.NotFound", status.Error(codes.NotFound, "resource not found"), true},
		{"grpc codes.Unavailable", status.Error(codes.Unavailable, "unavailable"), false},
		{"string 404", errors.New("Error 404: Not Found"), true},
		{"string not found", errors.New("the resource was not found"), true},
		{"string notFound", errors.New("resource notFound on server"), true},
		{"string other error", errors.New("connection reset by peer"), false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			actual := isNotFound(tc.err)
			if actual != tc.expected {
				t.Errorf("isNotFound(%v) = %v, want %v", tc.err, actual, tc.expected)
			}
		})
	}
}

func TestIsResourceNotReady(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{"nil error", nil, false},
		{
			name: "googleapi 400 with resourceNotReady reason",
			err: &googleapi.Error{
				Code:    400,
				Message: "The resource 'projects/p/zones/z/instanceGroupManagers/mig' is not ready",
				Errors: []googleapi.ErrorItem{
					{Reason: "resourceNotReady"},
				},
			},
			expected: true,
		},
		{
			name: "googleapi 409 with is not ready message",
			err: &googleapi.Error{
				Code:    409,
				Message: "The resource 'projects/p/zones/z/instanceGroupManagers/mig' is not ready",
			},
			expected: true,
		},
		{
			name:     "computepb operation error string",
			err:      errors.New("operation failed: code RESOURCE_NOT_READY: The resource 'mig' is not ready"),
			expected: true,
		},
		{
			name:     "gcloud command error string",
			err:      errors.New("Command failed: [gcloud compute instance-groups managed delete test-mig]: The resource 'projects/p/zones/z/instanceGroupManagers/test-mig' is not ready"),
			expected: true,
		},
		{
			name:     "unrelated 400 error",
			err:      &googleapi.Error{Code: 400, Message: "Invalid value for field"},
			expected: false,
		},
		{
			name:     "generic unrelated error",
			err:      errors.New("connection reset by peer"),
			expected: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			actual := isResourceNotReady(tc.err)
			if actual != tc.expected {
				t.Errorf("isResourceNotReady(%v) = %v, want %v", tc.err, actual, tc.expected)
			}
		})
	}
}

func TestHandleDeleteError_ResourceNotReady(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		expectPerm bool
	}{
		{
			name: "googleapi 400 resourceNotReady reason",
			err: &googleapi.Error{
				Code:    400,
				Message: "The resource 'projects/p/zones/z/instanceGroupManagers/mig' is not ready",
				Errors: []googleapi.ErrorItem{
					{Reason: "resourceNotReady"},
				},
			},
			expectPerm: false,
		},
		{
			name: "googleapi 400 is not ready message without reason item",
			err: &googleapi.Error{
				Code:    400,
				Message: "The resource 'projects/p/zones/z/instanceGroupManagers/mig' is not ready",
			},
			expectPerm: false,
		},
		{
			name:       "gcloud resource not ready string",
			err:        errors.New("The resource 'projects/p/zones/z/instanceGroupManagers/test-mig' is not ready"),
			expectPerm: false,
		},
		{
			name: "unrelated 400 error is permanent",
			err: &googleapi.Error{
				Code:    400,
				Message: "Invalid value for field",
			},
			expectPerm: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := handleDeleteError(tc.err, 1)
			if tc.expectPerm {
				if res == nil {
					t.Errorf("handleDeleteError(%v, 1) = nil, want PermanentError", tc.err)
				} else if _, isPerm := res.(*backoff.PermanentError); !isPerm {
					t.Errorf("handleDeleteError(%v, 1) = %T (%v), want PermanentError", tc.err, res, res)
				}
			} else {
				if res == nil {
					t.Errorf("handleDeleteError(%v, 1) = nil, want retriable error", tc.err)
				} else if _, isPerm := res.(*backoff.PermanentError); isPerm {
					t.Errorf("handleDeleteError(%v, 1) returned PermanentError, want retriable error", tc.err)
				}
			}
		})
	}
}

func TestIsTransientError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{"nil error", nil, false},
		{"googleapi 403 quota", &googleapi.Error{Code: 403, Message: "Quota exceeded"}, true},
		{"googleapi 429 rate limit", &googleapi.Error{Code: 429, Message: "Rate limit exceeded"}, true},
		{"googleapi 500 internal server error", &googleapi.Error{Code: 500, Message: "Internal Server Error"}, true},
		{"googleapi 502 bad gateway", &googleapi.Error{Code: 502, Message: "Bad Gateway"}, true},
		{"googleapi 503 service unavailable", &googleapi.Error{Code: 503, Message: "Service Unavailable"}, true},
		{"googleapi 504 gateway timeout", &googleapi.Error{Code: 504, Message: "Gateway Timeout"}, true},
		{"grpc codes.ResourceExhausted", status.Error(codes.ResourceExhausted, "resource exhausted"), true},
		{"grpc codes.Unavailable", status.Error(codes.Unavailable, "service unavailable"), true},
		{"grpc codes.Internal", status.Error(codes.Internal, "internal server error"), true},
		{"string Quota", errors.New("Quota 'CPUS' exceeded. Limit: 24.0 in region us-central1."), true},
		{"string QUOTA", errors.New("QUOTA_EXCEEDED: Read requests per minute per region"), true},
		{"string rate limit", errors.New("rate limit exceeded, please retry"), true},
		{"string Rate limit", errors.New("Rate limit reached for API"), true},
		{"string Internal error", errors.New("Internal error encountered while processing request"), true},
		{"string Error 50", errors.New("Error 502 (Server Error)!!1"), true},
		{"string with 503 status code", errors.New("HTTP response status: 503"), true},
		{"string with : 503 followed by space", errors.New("server error: 503 upstream error"), true},
		{"string with : 503 followed by newline", errors.New("error code: 503\ntrying again"), true},
		{"string with trailing : 503", errors.New("request failed: 503"), true},
		{"string with : 502 followed by space", errors.New("gateway: 502 connection dropped"), true},
		{"string with : 502 followed by newline", errors.New("response: 502\nretrying"), true},
		{"string with trailing : 502", errors.New("request failed: 502"), true},
		{"string with 502 status code", errors.New("proxy error 502 Bad Gateway"), true},
		{"string database is locked", errors.New("sqlite3: database is locked"), true},
		{"negative substring collision vm-502 should not be transient", errors.New("instance vm-502 invalid"), false},
		{"negative substring collision vm-503-disk should not be transient", errors.New("disk vm-503-disk not found"), false},
		{"negative substring collision ip 10.0.50.3 should not be transient", errors.New("remote address 10.0.50.3 unreachable"), false},
		{"negative substring collision port 5030 should not be transient", errors.New("connection refused on port 5030"), false},
		{"negative substring collision port 5034 should not be transient", errors.New("dial tcp 10.0.0.1:5034: connection refused"), false},
		{"negative substring collision status code: 5030 should not be transient", errors.New("status code: 5030"), false},
		{"negative substring collision port: 5029 should not be transient", errors.New("port: 5029 in use"), false},
		{"negative substring :503 without boundary before alphanumeric", errors.New("error:503a"), false},
		{"googleapi 400 bad request", &googleapi.Error{Code: 400, Message: "Bad Request"}, false},
		{"googleapi 404 not found", &googleapi.Error{Code: 404, Message: "Not Found"}, false},
		{"grpc codes.InvalidArgument", status.Error(codes.InvalidArgument, "invalid argument"), false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			actual := isTransientError(tc.err)
			if actual != tc.expected {
				t.Errorf("isTransientError(%v) = %v, want %v", tc.err, actual, tc.expected)
			}
		})
	}
}

func TestIsStockoutError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{"nil error", nil, false},
		{"ZONE_RESOURCE_POOL_EXHAUSTED", errors.New("ZONE_RESOURCE_POOL_EXHAUSTED: A t2a-standard-2 VM instance is currently unavailable in the us-central1-b zone"), true},
		{"RESOURCE_POOL_EXHAUSTED", errors.New("RESOURCE_POOL_EXHAUSTED"), true},
		{"currently unavailable", errors.New("The service is currently unavailable in the requested zone."), true},
		{"unrelated error", errors.New("instance not found"), false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			actual := isStockoutError(tc.err)
			if actual != tc.expected {
				t.Errorf("isStockoutError(%v) = %v, want %v", tc.err, actual, tc.expected)
			}
		})
	}
}

func TestRetryOp(t *testing.T) {
	t.Run("succeeds on first attempt", func(t *testing.T) {
		calls := 0
		err := retryOp(context.Background(), backoff.NewConstantBackOff(time.Millisecond), isTransientError, func() error {
			calls++
			return nil
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if calls != 1 {
			t.Errorf("calls = %d, want 1", calls)
		}
	})

	t.Run("retries on transient error and succeeds", func(t *testing.T) {
		calls := 0
		err := retryOp(context.Background(), backoff.NewConstantBackOff(time.Millisecond), isTransientError, func() error {
			calls++
			if calls < 3 {
				return &googleapi.Error{Code: 429, Message: "rate limit"}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if calls != 3 {
			t.Errorf("calls = %d, want 3", calls)
		}
	})

	t.Run("aborts immediately on permanent non-retryable error", func(t *testing.T) {
		calls := 0
		permanentErr := &googleapi.Error{Code: 400, Message: "bad request"}
		err := retryOp(context.Background(), backoff.NewConstantBackOff(time.Millisecond), isTransientError, func() error {
			calls++
			return permanentErr
		})
		if !errors.Is(err, permanentErr) {
			t.Fatalf("expected permanentErr, got: %v", err)
		}
		if calls != 1 {
			t.Errorf("calls = %d, want 1 (should not retry)", calls)
		}
	})
}
