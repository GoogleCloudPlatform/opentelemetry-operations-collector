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

package filelogsource

import (
	"context"
	"strings"
	"testing"

	policyv1alpha1 "github.com/GoogleCloudPlatform/opentelemetry-operations-collector/gen/go/policy/v1alpha1"
	"github.com/stretchr/testify/require"
)

func FuzzPolicyValidation(f *testing.F) {
	seeds := []string{
		"/var/log/**/*.log",
		"/var/log/syslog",
		"/var/log",
		"/var/log/",
		"/etc/shadow",
		"/tmp/test.log",
		"/app/logs/*.log",
		"/opt/custom/service.log",
		`C:\ProgramData\App\logs\*.log`,
		`%ProgramData%\App\logs\*.log`,
		`C:\inetpub\logs\*.log`,
		`C:\inetpub\logs`,
		`C:\Windows\System32\app.log`,
		`D:\custom\app\logs\*.log`,
		"/var/log/../etc/passwd",
		"relative/path/test.log",
		"./app.log",
		"/var/log/[a-z",
		"/var/log/{a,b",
		"/var/log/{nginx,apache2}/*.log",
		"/opt/{a/logs/x,secret/id_rsa}",
		"/opt/app/{logs/*.log,conf/*.yaml}",
		"/var/log/${file:/etc/shadow}",
		"/var/log/app..log",
		"/var/log/.[.]/etc/passwd",
		"/var/log/[.][.]/etc/passwd",
		"/var/log/.{.,}./etc/passwd",
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, pathStr string) {
		pb := &policyv1alpha1.FileLogSourcePolicy{
			Id:            "fuzz-policy",
			IncludedPaths: []string{pathStr},
		}
		// Validate should not panic on any input
		policy, err := NewPolicyFromProto(pb)
		if err == nil && policy != nil {
			require.NoError(t, policy.Validate())
			_, err := policy.Evaluate(context.Background())
			require.NoError(t, err)

			for _, pat := range policy.IncludedPaths() {
				require.NotEmpty(t, strings.TrimSpace(pat), "Pattern %q must not be empty or whitespace", pat)
				require.NoError(t, validateIncludePattern(pat))
			}
		}
	})
}
