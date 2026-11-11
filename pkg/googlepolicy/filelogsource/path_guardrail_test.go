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
	"strings"
	"testing"

	policyv1 "github.com/GoogleCloudPlatform/opentelemetry-operations-collector/gen/go/policy/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPathGuardrail_PipelineStages(t *testing.T) {
	t.Run("Stage 1 - sanitizePattern", func(t *testing.T) {
		assert.ErrorIs(t, sanitizePattern(""), ErrEmptyIncludePattern)
		assert.ErrorIs(t, sanitizePattern("/var/log/../etc/passwd"), ErrPathTraversal)
		assert.ErrorIs(t, sanitizePattern(`C:\ProgramData\..\Windows\System32`), ErrPathTraversal)
		assert.ErrorIs(t, sanitizePattern("/var/log/[a-z"), ErrInvalidGlobPattern)
		assert.ErrorIs(t, sanitizePattern("/var/log/${file:/etc/shadow}"), ErrDollarNotAllowed)
		assert.ErrorIs(t, sanitizePattern("/var/log/app$log.txt"), ErrDollarNotAllowed)
		assert.NoError(t, sanitizePattern("/var/log/*.log"))
		assert.NoError(t, sanitizePattern("/var/log/app..log"))
		assert.NoError(t, sanitizePattern("/var/log/*..*"))
		assert.NoError(t, sanitizePattern(`C:\ProgramData\App\logs\*.log`))
	})

	t.Run("Stage 1 - expandBraces", func(t *testing.T) {
		// Simple pattern without braces
		branches, err := expandBraces("/var/log/*.log")
		require.NoError(t, err)
		assert.Equal(t, []string{"/var/log/*.log"}, branches)

		// Comma-separated braces
		branches, err = expandBraces("/var/log/{nginx,apache2}/*.log")
		require.NoError(t, err)
		assert.Equal(t, []string{"/var/log/nginx/*.log", "/var/log/apache2/*.log"}, branches)

		// Multiple brace sets
		branches, err = expandBraces("/opt/{app1,app2}/logs/{prod,dev}/*.log")
		require.NoError(t, err)
		assert.Equal(t, []string{
			"/opt/app1/logs/prod/*.log",
			"/opt/app1/logs/dev/*.log",
			"/opt/app2/logs/prod/*.log",
			"/opt/app2/logs/dev/*.log",
		}, branches)

		// Unmatched opening brace
		_, err = expandBraces("/var/log/{nginx,apache2")
		require.Error(t, err)

		// Unmatched closing brace
		_, err = expandBraces("/var/log/nginx,apache2}/*.log")
		require.Error(t, err)
	})

	t.Run("Stage 2 - normalizePattern", func(t *testing.T) {
		// Linux path preserves case and does not replace backslash escape sequences
		assert.Equal(t, "/var/log/App.log", normalizePattern("/var/log/App.log"))
		assert.Equal(t, "/opt/MyService/logs/*.log", normalizePattern("/opt/MyService/logs/*.log"))
		assert.Equal(t, `/var/log/app\[1\].log`, normalizePattern(`/var/log/app\[1\].log`))
		assert.Equal(t, `/var/log/\{special\}.log`, normalizePattern(`/var/log/\{special\}.log`))

		// Windows path normalizes drive to uppercase, rest to lowercase, separators to '/'
		assert.Equal(t, "C:/programdata/myapp/logs/*.log", normalizePattern(`C:\ProgramData\MyApp\logs\*.log`))
		assert.Equal(t, "C:/programdata/myapp/logs/*.log", normalizePattern(`c:/ProgramData/MyApp/logs/*.log`))
		assert.Equal(t, "C:/inetpub/logs/*.log", normalizePattern(`%SystemDrive%\inetpub\logs\*.log`))
	})

	t.Run("Validation - Absolute Paths and Bare Directories", func(t *testing.T) {
		t.Cleanup(setOSForTest("linux"))

		// Relative path rejected
		err := validateIncludePattern("var/log/*.log")
		require.ErrorIs(t, err, ErrRelativePath)

		err = validateIncludePattern("./app.log")
		require.ErrorIs(t, err, ErrPathTraversal)

		// Bare directories rejected (structural trailing slashes or roots)
		err = validateIncludePattern("/var/log/")
		require.ErrorIs(t, err, ErrBareDirectoryPath)

		err = validateIncludePattern("/opt/app/logs/")
		require.ErrorIs(t, err, ErrBareDirectoryPath)

		err = validateIncludePattern("/")
		require.ErrorIs(t, err, ErrBareDirectoryPath)

		// Valid custom application paths permitted (no hardcoded OS log root restrictions)
		assert.NoError(t, validateIncludePattern("/var/log"))
		assert.NoError(t, validateIncludePattern("/opt/app/logs"))
		assert.NoError(t, validateIncludePattern("/tmp/app.log"))
		assert.NoError(t, validateIncludePattern("/app/logs/*.log"))
		assert.NoError(t, validateIncludePattern("/srv/custom/output.log"))
		assert.NoError(t, validateIncludePattern("/opt/myapp/data/*.log"))
	})
}

func TestPathGuardrail_BraceExpansionSecurity(t *testing.T) {
	t.Cleanup(setOSForTest("linux"))

	testCases := []struct {
		name        string
		pattern     string
		expectedErr error
	}{
		{
			name:        "valid brace expansion in /var/log",
			pattern:     "/var/log/{nginx,apache2}/*.log",
			expectedErr: nil,
		},
		{
			name:        "valid brace expansion under /opt with logs in all branches",
			pattern:     "/opt/{app1,app2}/logs/*.log",
			expectedErr: nil,
		},
		{
			name:        "valid brace expansion targeting multiple files across directories",
			pattern:     "/opt/{a/logs/x,b/data/y}.log",
			expectedErr: nil,
		},
		{
			name:        "brace expansion mixing log files with config files is valid under relaxed roots",
			pattern:     "/opt/app/{logs/*.log,conf/*.yaml}",
			expectedErr: nil,
		},
		{
			name:        "brace expansion attempting path traversal to /etc is strictly rejected",
			pattern:     "/var/log/{app.log,../../etc/shadow}",
			expectedErr: ErrPathTraversal,
		},
		{
			name:        "exponential brace expansion DoS attack is bounded and rejected",
			pattern:     "/var/log/{a,b}{a,b}{a,b}{a,b}{a,b}{a,b}{a,b}/*.log",
			expectedErr: ErrTooManyBraceExpansions,
		},
		{
			name:        "bare directory . leaf rejected",
			pattern:     "/var/log/.",
			expectedErr: ErrBareDirectoryPath,
		},
		{
			name:        "bare directory ./ leaf rejected",
			pattern:     "/var/log/./",
			expectedErr: ErrBareDirectoryPath,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			pb := &policyv1.FileLogSourcePolicy{
				Id:            "brace-test",
				IncludedPaths: []string{tc.pattern},
			}
			_, err := NewPolicyFromProto(pb)
			if tc.expectedErr == nil {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, tc.expectedErr)
			}
		})
	}
}

func TestPathGuardrail_CrossOSGating(t *testing.T) {
	t.Run("rejects Windows paths on Linux", func(t *testing.T) {
		t.Cleanup(setOSForTest("linux"))

		err := validateIncludePattern(`C:\ProgramData\MyApp\logs\*.log`)
		require.ErrorIs(t, err, ErrRelativePath)

		err = validateIncludePattern(`%ProgramData%\MyApp\logs\*.log`)
		require.ErrorIs(t, err, ErrRelativePath)
	})

	t.Run("rejects POSIX paths on Windows", func(t *testing.T) {
		t.Cleanup(setOSForTest("windows"))

		err := validateIncludePattern("/var/log/*.log")
		require.ErrorIs(t, err, ErrRelativePath)
	})

	t.Run("accepts valid Windows paths on Windows", func(t *testing.T) {
		t.Cleanup(setOSForTest("windows"))

		assert.NoError(t, validateIncludePattern(`C:\ProgramData\MyApp\logs\*.log`))
		assert.NoError(t, validateIncludePattern(`D:\custom\app\data\*.log`))
		assert.NoError(t, validateIncludePattern(`%ProgramData%\MyApp\logs\*.log`))
		assert.NoError(t, validateIncludePattern(`%SystemDrive%\inetpub\logs\*.log`))

		// Bare Windows directories rejected
		assert.ErrorIs(t, validateIncludePattern(`C:\`), ErrBareDirectoryPath)
		assert.ErrorIs(t, validateIncludePattern(`C:\logs\`), ErrBareDirectoryPath)
		assert.NoError(t, validateIncludePattern(`C:\logs`))
	})
}

func TestPathGuardrail_PathTraversalPrecision(t *testing.T) {
	t.Cleanup(setOSForTest("linux"))

	// Legitimate files with double dots should pass
	assert.NoError(t, validateIncludePattern("/var/log/app..log"))
	assert.NoError(t, validateIncludePattern("/var/log/my..service..log"))

	// Actual traversal attempts must be rejected
	assert.ErrorIs(t, validateIncludePattern("/var/log/../etc/shadow"), ErrPathTraversal)
	assert.ErrorIs(t, validateIncludePattern("/var/log/sub/../../etc/passwd"), ErrPathTraversal)
	assert.ErrorIs(t, validateIncludePattern("/var/log/{app,../etc}/log"), ErrPathTraversal)
	assert.ErrorIs(t, validateIncludePattern("/opt/app/logs/../../etc/shadow"), ErrPathTraversal)

	// Adversarial evasion vectors: character classes matching '..'
	assert.ErrorIs(t, validateIncludePattern("/var/log/.[.]/etc/passwd"), ErrPathTraversal)
	assert.ErrorIs(t, validateIncludePattern("/var/log/[.][.]/etc/passwd"), ErrPathTraversal)
	assert.ErrorIs(t, validateIncludePattern("/var/log/[.]/etc/passwd"), ErrPathTraversal)
	assert.ErrorIs(t, validateIncludePattern("/var/log/*..*/app.log"), ErrPathTraversal)

	// Adversarial evasion vectors: brace expansion constructing '..'
	assert.ErrorIs(t, validateIncludePattern("/var/log/.{.,}./etc/passwd"), ErrPathTraversal)
	assert.ErrorIs(t, validateIncludePattern("/opt/app/logs/.{.,}./etc/shadow"), ErrPathTraversal)
}

func TestPathGuardrail_AnchoredEnvExpansion(t *testing.T) {
	// Leading %ProgramData% expands
	expanded := expandWindowsEnv(`%ProgramData%\MyApp\logs\*.log`)
	assert.True(t, strings.HasPrefix(expanded, `C:\ProgramData\`))

	// Middle of path %ProgramData% does NOT expand
	middle := `C:\logs\%ProgramData%\app.log`
	assert.Equal(t, middle, expandWindowsEnv(middle))
}
