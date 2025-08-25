// Copyright 2025 Google LLC
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

package googleservicecontrolexporter

import (
	"context"
	"fmt"
	"testing"

	scpb "cloud.google.com/go/servicecontrol/apiv1/servicecontrolpb"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	grpcmd "google.golang.org/grpc/metadata"
)

const grpcUserAgentPrefix = "grpc-go/" // gRPC adds its own user-agent in the format "grpc-go/1.74.2"

func TestNewServiceControllerClient(t *testing.T) {
	logger := zap.NewNop()
	server, mockServer, listener, err := StartMockServer()
	assert.NoError(t, err)
	defer StopMockServer(server, listener)
	defer server.Stop()

	var gotMetadata grpcmd.MD
	mockServer.SetReturnFunc(func(ctx context.Context, req *scpb.ReportRequest) (*scpb.ReportResponse, error) {
		gotMetadata, _ = grpcmd.FromIncomingContext(ctx)
		return &scpb.ReportResponse{}, nil
	})

	testCases := []struct {
		name                string
		useRawClient        bool
		insecure            bool
		debugHeaders        bool
		endpoint            string
		userAgent           string
		expectErrorOnCreate bool
		expectErrorOnReport bool
		expectedClient      interface{}
	}{
		{
			name:           "raw client",
			useRawClient:   true,
			insecure:       true,
			debugHeaders:   false,
			endpoint:       "bufnet",
			userAgent:      "",
			expectedClient: (*serviceControlClientRaw)(nil),
		},
		{
			name:           "library client",
			useRawClient:   false,
			insecure:       true,
			debugHeaders:   false,
			endpoint:       "bufnet",
			userAgent:      "",
			expectedClient: (*serviceControlClientLibrary)(nil),
		},
		{
			name:           "raw client with user agent",
			useRawClient:   true,
			insecure:       true,
			debugHeaders:   false,
			endpoint:       "bufnet",
			userAgent:      "test-user-agent-raw",
			expectedClient: (*serviceControlClientRaw)(nil),
		},
		{
			name:           "library client with user agent",
			useRawClient:   false,
			insecure:       true,
			debugHeaders:   false,
			endpoint:       "bufnet",
			userAgent:      "test-user-agent-library",
			expectedClient: (*serviceControlClientLibrary)(nil),
		},
		{
			// Debug header content is been testing separately in exporter_test.go TestRetriableErrorHeader
			name:           "library client with debug headers and user agent",
			useRawClient:   false,
			insecure:       true,
			debugHeaders:   true,
			endpoint:       "bufnet",
			userAgent:      "test-user-agent-debug",
			expectedClient: (*serviceControlClientLibrary)(nil),
		},
		{
			name:                "raw client dial error insecure",
			useRawClient:        true,
			insecure:            true,
			endpoint:            "invalid-endpoint:1234",
			expectErrorOnCreate: false,
			expectErrorOnReport: true,
			expectedClient:      (*serviceControlClientRaw)(nil),
		},
		{
			name:                "library client dial error insecure",
			useRawClient:        false,
			insecure:            true,
			endpoint:            "invalid-endpoint:1234",
			expectErrorOnCreate: false,
			expectErrorOnReport: true,
			expectedClient:      (*serviceControlClientLibrary)(nil),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var dialOpts []grpc.DialOption
			if tc.insecure {
				dialOpts = append(dialOpts, grpc.WithTransportCredentials(insecure.NewCredentials()))
			}
			if tc.endpoint == "bufnet" {
				dialOpts = append(dialOpts, grpc.WithContextDialer(BufDialer))
			}
			if tc.userAgent != "" {
				dialOpts = append(dialOpts, grpc.WithUserAgent(tc.userAgent))
			}

			client, err := NewServiceControllerClient(tc.endpoint, tc.useRawClient, tc.insecure, tc.debugHeaders, logger, dialOpts...)

			if tc.expectErrorOnCreate {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.NotNil(t, client)
			assert.IsType(t, tc.expectedClient, client)

			_, err = client.Report(context.Background(), &scpb.ReportRequest{})
			if tc.expectErrorOnReport {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				if userAgent, ok := gotMetadata["user-agent"]; ok {
					expected := fmt.Sprintf("%s%s", grpcUserAgentPrefix, grpc.Version)
					if tc.userAgent != "" {
						expected = fmt.Sprintf("%s %s", tc.userAgent, expected)
					}
					assert.Equal(t, expected, userAgent[0], "user-agent header mismatch")
				} else {
					t.Errorf("missing user-agent header")
				}
			}

			// Close should not fail even if report does
			assert.NoError(t, client.Close())
		})
	}
}
