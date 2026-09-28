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

package googlepolicy

import (
	"context"
	"errors"
	"testing"

	discoveryv3 "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	grpcstatus "google.golang.org/grpc/status"
)

// errTokenSource is a TokenSource that always fails, standing in for a metadata
// server that is briefly unreachable.
type errTokenSource struct{ err error }

func (s errTokenSource) Token() (*oauth2.Token, error) { return nil, s.err }

// insecureTokenAuth is TokenAuth with the transport security requirement
// dropped.
//
// The behaviour under test is how gRPC treats the error returned by
// GetRequestMetadata, which is independent of transport security. The real
// TokenAuth requires it (correctly -- bearer tokens must not cross a plaintext
// connection), but grpc.NewClient rejects that combination against the
// in-memory insecure listener these tests use, before any of the code under
// test runs. Only RequireTransportSecurity is substituted; GetRequestMetadata
// is the real one.
type insecureTokenAuth struct{ *TokenAuth }

func (insecureTokenAuth) RequireTransportSecurity() bool { return false }

var _ credentials.PerRPCCredentials = insecureTokenAuth{}

// bareErrTokenAuth returns the TokenSource error unwrapped, which is what
// TokenAuth used to do. It exists so the test below can demonstrate that the
// wrapping is what changes the outcome, rather than asserting into a vacuum.
type bareErrTokenAuth struct{ ts oauth2.TokenSource }

func (a bareErrTokenAuth) GetRequestMetadata(context.Context, ...string) (map[string]string, error) {
	tok, err := a.ts.Token()
	if err != nil {
		return nil, err
	}
	return map[string]string{"authorization": "Bearer " + tok.AccessToken}, nil
}

func (bareErrTokenAuth) RequireTransportSecurity() bool { return false }

func TestTokenAuth_LocalTokenFailureIsUnavailable(t *testing.T) {
	auth := &TokenAuth{TS: errTokenSource{err: errors.New("metadata server unreachable")}}

	_, err := auth.GetRequestMetadata(context.Background())
	require.Error(t, err)

	assert.Equal(t, codes.Unavailable, grpcstatus.Code(err),
		"a local token fetch failure must not look like a credential rejection")
	assert.Contains(t, err.Error(), "metadata server unreachable",
		"the underlying cause must survive so the log is diagnosable")
}

// TestTokenAuth_LocalTokenFailureIsRetryableThroughGRPC drives a real gRPC
// client so the assertion covers what the transport does to the error, not just
// what we return.
//
// gRPC rewrites a bare error from per-RPC credentials into codes.Unauthenticated,
// whereas wrapping the error with codes.Unavailable preserves the status code
// across the transport.
func TestTokenAuth_LocalTokenFailureIsRetryableThroughGRPC(t *testing.T) {
	tokenErr := errors.New("metadata server unreachable")

	tests := []struct {
		name     string
		creds    credentials.PerRPCCredentials
		wantCode codes.Code
	}{
		{
			name:     "bare error is relabelled by gRPC",
			creds:    bareErrTokenAuth{ts: errTokenSource{err: tokenErr}},
			wantCode: codes.Unauthenticated,
		},
		{
			name:     "status-wrapped error survives",
			creds:    insecureTokenAuth{&TokenAuth{TS: errTokenSource{err: tokenErr}}},
			wantCode: codes.Unavailable,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dialOpt := startFakeADSServer(t, &fakeADSServer{})

			conn, err := grpc.NewClient("passthrough:///bufnet",
				dialOpt,
				grpc.WithTransportCredentials(insecure.NewCredentials()),
				grpc.WithPerRPCCredentials(tc.creds),
			)
			require.NoError(t, err)
			t.Cleanup(func() { _ = conn.Close() })

			// The stream never opens: gRPC fetches per-RPC credentials while
			// building the request headers, so the failure surfaces here.
			_, err = discoveryv3.NewAggregatedDiscoveryServiceClient(conn).
				StreamAggregatedResources(context.Background())
			require.Error(t, err)

			assert.Equal(t, tc.wantCode, grpcstatus.Code(err))
		})
	}
}
