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
	"testing"
	"time"

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
