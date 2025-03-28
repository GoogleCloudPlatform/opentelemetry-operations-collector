// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package oauth2clientauthextension

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

// To get a subjectToken, connect to the sts-test eks cluster and run the following command:
// kubectl create token test-ksa --audience=//iam.googleapis.com/projects/496167864958/locations/global/workloadIdentityPools/sts-test/providers/aws-eks  --duration=24h
func TestGetToken(t *testing.T) {
	// test files for TLS testing
	var (
		// Sample token that has already expired
		subjectToken     = "eyJhbGciOiJSUzI1NiIsImtpZCI6ImJiNjU5MWZjZWM5OGNjYWI1NTRlMzRjYjgwMzNkYzY2ZTQ4M2ZlM2QifQ.eyJhdWQiOlsiLy9pYW0uZ29vZ2xlYXBpcy5jb20vcHJvamVjdHMvNDk2MTY3ODY0OTU4L2xvY2F0aW9ucy9nbG9iYWwvd29ya2xvYWRJZGVudGl0eVBvb2xzL3N0cy10ZXN0L3Byb3ZpZGVycy9hd3MtZWtzIl0sImV4cCI6MTc0MzE3OTU5MSwiaWF0IjoxNzQzMTc1OTkxLCJpc3MiOiJodHRwczovL29pZGMuZWtzLnVzLWVhc3QtMS5hbWF6b25hd3MuY29tL2lkL0ExNkY0RTc2QUVENkU0MzBDQUI5NjZFM0RFREQ2MkY5IiwianRpIjoiZWVjOWU5MjQtNGY3YS00NTY1LWE3NDYtYjE4ZWJmYWViYjlmIiwia3ViZXJuZXRlcy5pbyI6eyJuYW1lc3BhY2UiOiJkZWZhdWx0Iiwic2VydmljZWFjY291bnQiOnsibmFtZSI6InRlc3Qta3NhIiwidWlkIjoiZjgzYmRlZTUtMTUwNi00ZDI4LWI4NGEtMWI1YTBkZmM1NWYwIn19LCJuYmYiOjE3NDMxNzU5OTEsInN1YiI6InN5c3RlbTpzZXJ2aWNlYWNjb3VudDpkZWZhdWx0OnRlc3Qta3NhIn0.UcSTFCKDqdVp0XioqqCvdVWjR0Bx2dIWT-sy-TBNYDE3-iM18nbIaYpq_wO9F4QxKuIYMuSylUlXn9pioc68iR_oPj6OLR1z2zp2TU0AG6H_1YVxrPnZ_ELgN8QhOGa4rTxcvqK62igZ976nA2-eDA-3b1LNUTZO0-N7-dJ60a_Cb5vPHA-hPtiKyNC1XgujPFOLFLKaGFdTrJE7ed_pBlJozvH85TdPRL22w2NpQgi-pjSZLQ6uHZkb1CvkvZC82-AGdt_f9myjrD53XHPfFxNpOdUsNpKFD0yoeZcLDmin2ofsePASZv-v_wlddMjtqPcXRnUia8iOeD03jUi7bw"
		subjectTokenType = "urn:ietf:params:oauth:token-type:jwt"
		tokenURL         = "https://sts.googleapis.com/v1/token"
		scopes           = []string{"https://www.googleapis.com/auth/cloud-platform"}
		audience         = "//iam.googleapis.com/projects/496167864958/locations/global/workloadIdentityPools/sts-test/providers/aws-eks"
	)

	tests := []struct {
		name          string
		settings      *Config
		shouldError   bool
		expectedError string
	}{
		{
			name: "get_token_via_sts",
			settings: &Config{
				SubjectToken:     subjectToken,
				SubjectTokenType: subjectTokenType,
				TokenURL:         tokenURL,
				Scopes:           scopes,
				Audience:         audience,
			},
			shouldError:   false,
			expectedError: "",
		},
	}

	for _, test := range tests {
		t.Skip("This test should only be run manually for now.")
		t.Run(test.name, func(t *testing.T) {
			rc, err := newStsClientAuthenticator(test.settings, zap.NewNop())
			assert.NoError(t, err)

			cred, err := rc.PerRPCCredentials()
			assert.NoError(t, err)

			reqMetadata, err := cred.GetRequestMetadata(context.Background())
			assert.NoError(t, err)

			token := strings.TrimPrefix("Bearer ", reqMetadata["authorization"])

			assert.Greater(t, len(token), 0)
		})
	}
}
