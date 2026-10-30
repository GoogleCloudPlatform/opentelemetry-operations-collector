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

	policyv1alpha1 "github.com/GoogleCloudPlatform/opentelemetry-operations-collector/gen/go/policy/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPathGuardrail_PipelineStages(t *testing.T) {
	t.Run("Stage 1 - SanitizePattern", func(t *testing.T) {
		assert.ErrorIs(t, SanitizePattern(""), ErrEmptyIncludePattern)
		assert.ErrorIs(t, SanitizePattern("/var/log/../etc/passwd"), ErrPathTraversal)
		assert.ErrorIs(t, SanitizePattern(`C:\ProgramData\..\Windows\System32`), ErrPathTraversal)
		assert.ErrorIs(t, SanitizePattern("/var/log/[a-z"), ErrInvalidGlobPattern)
		assert.ErrorIs(t, SanitizePattern("/var/log/${file:/etc/shadow}"), ErrDollarNotAllowed)
		assert.ErrorIs(t, SanitizePattern("/var/log/app$log.txt"), ErrDollarNotAllowed)
		assert.NoError(t, SanitizePattern("/var/log/*.log"))
		assert.NoError(t, SanitizePattern("/var/log/app..log"))
		assert.NoError(t, SanitizePattern("/var/log/*..*"))
		assert.NoError(t, SanitizePattern(`C:\ProgramData\App\logs\*.log`))
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

	t.Run("Stage 2 - NormalizePattern", func(t *testing.T) {
		// Linux path preserves case and converts separators
		assert.Equal(t, "/var/log/App.log", NormalizePattern("/var/log/App.log"))
		assert.Equal(t, "/opt/MyService/logs/*.log", NormalizePattern("/opt/MyService/logs/*.log"))

		// Windows path normalizes drive to uppercase, rest to lowercase, separators to '/'
		assert.Equal(t, "C:/programdata/myapp/logs/*.log", NormalizePattern(`C:\ProgramData\MyApp\logs\*.log`))
		assert.Equal(t, "C:/programdata/myapp/logs/*.log", NormalizePattern(`c:/ProgramData/MyApp/logs/*.log`))
		assert.Equal(t, "C:/inetpub/logs/*.log", NormalizePattern(`%SystemDrive%\inetpub\logs\*.log`))
	})

	t.Run("Stage 3 - ParseAnatomy", func(t *testing.T) {
		prevOS := currentOS
		currentOS = "linux"
		t.Cleanup(func() { currentOS = prevOS })

		// Relative path rejected
		_, err := ParseAnatomy("var/log/*.log", "var/log/*.log")
		require.ErrorIs(t, err, ErrRelativePath)

		// Bare directories rejected
		_, err = ParseAnatomy("/var/log", "/var/log")
		require.ErrorIs(t, err, ErrBareDirectoryPath)

		_, err = ParseAnatomy("/opt/app/logs/", "/opt/app/logs")
		require.ErrorIs(t, err, ErrBareDirectoryPath)

		// Linux anatomy structural parsing
		anatomy, err := ParseAnatomy("/opt/my-service/sub/logs/prod/*.log", "/opt/my-service/sub/logs/prod/*.log")
		require.NoError(t, err)
		assert.False(t, anatomy.IsWindows)
		assert.Equal(t, "opt", anatomy.Root)
		assert.Equal(t, []string{"my-service", "sub", "logs", "prod"}, anatomy.Segments)
		assert.Equal(t, "*.log", anatomy.Leaf)

		// Windows anatomy structural parsing (under windows OS setting)
		currentOS = "windows"
		anatomyWin, err := ParseAnatomy(`C:\ProgramData\App\logs\*.log`, "C:/programdata/app/logs/*.log")
		require.NoError(t, err)
		assert.True(t, anatomyWin.IsWindows)
		assert.Equal(t, "C", anatomyWin.DriveLetter)
		assert.Equal(t, "programdata", anatomyWin.Root)
		assert.Equal(t, []string{"app", "logs"}, anatomyWin.Segments)
		assert.Equal(t, "*.log", anatomyWin.Leaf)
	})

	t.Run("Stage 4 - VerifyLogRoot", func(t *testing.T) {
		// Sensitive directories rejected
		anatomy, err := ParseAnatomy("/etc/shadow", "/etc/shadow")
		require.NoError(t, err)
		assert.ErrorIs(t, VerifyLogRoot(anatomy, "/etc/shadow"), ErrSensitiveDirectory)

		// Disallowed root rejected
		anatomyTmp, err := ParseAnatomy("/tmp/app.log", "/tmp/app.log")
		require.NoError(t, err)
		assert.ErrorIs(t, VerifyLogRoot(anatomyTmp, "/tmp/app.log"), ErrDisallowedLogRoot)

		// Allowed /var/log/**
		anatomyVarLog, err := ParseAnatomy("/var/log/**/*.log", "/var/log/**/*.log")
		require.NoError(t, err)
		assert.NoError(t, VerifyLogRoot(anatomyVarLog, "/var/log/**/*.log"))

		// Allowed /opt/**/logs/**
		anatomyOpt, err := ParseAnatomy("/opt/app/logs/prod/*.log", "/opt/app/logs/prod/*.log")
		require.NoError(t, err)
		assert.NoError(t, VerifyLogRoot(anatomyOpt, "/opt/app/logs/prod/*.log"))
	})
}

func TestPathGuardrail_BraceExpansionSecurity(t *testing.T) {
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
			name:        "Dashpole exploit: brace expansion escaping allowed root under /opt",
			pattern:     "/opt/{a/logs/x,secret/id_rsa}",
			expectedErr: ErrDisallowedLogRoot,
		},
		{
			name:        "brace expansion mixing valid logs with config files",
			pattern:     "/opt/app/{logs/*.log,conf/*.yaml}",
			expectedErr: ErrDisallowedLogRoot,
		},
		{
			name:        "brace expansion attempting path traversal to /etc",
			pattern:     "/var/log/{app.log,../../etc/shadow}",
			expectedErr: ErrPathTraversal,
		},
		{
			name:        "brace expansion attempting sensitive directory",
			pattern:     "/{var/log/app.log,etc/shadow}",
			expectedErr: ErrSensitiveDirectory,
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
			pb := &policyv1alpha1.FileLogSourcePolicy{
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

func TestVerifyLogRoot_NilAnatomy(t *testing.T) {
	err := VerifyLogRoot(nil, "/var/log/*.log")
	require.Error(t, err)
	assert.Equal(t, "path anatomy cannot be nil", err.Error())
}

func TestPathGuardrail_CrossOSGating(t *testing.T) {
	t.Run("rejects Windows paths on Linux", func(t *testing.T) {
		prevOS := currentOS
		currentOS = "linux"
		t.Cleanup(func() { currentOS = prevOS })

		err := ValidateIncludePattern(`C:\ProgramData\MyApp\logs\*.log`)
		require.ErrorIs(t, err, ErrDisallowedLogRoot)
		assert.Contains(t, err.Error(), "Windows paths not allowed on non-Windows platforms")

		err = ValidateIncludePattern(`%ProgramData%\MyApp\logs\*.log`)
		require.ErrorIs(t, err, ErrDisallowedLogRoot)
	})

	t.Run("rejects POSIX paths on Windows", func(t *testing.T) {
		prevOS := currentOS
		currentOS = "windows"
		t.Cleanup(func() { currentOS = prevOS })

		err := ValidateIncludePattern("/var/log/*.log")
		require.ErrorIs(t, err, ErrDisallowedLogRoot)
		assert.Contains(t, err.Error(), "POSIX paths not allowed on Windows")
	})
}

func TestPathGuardrail_PathTraversalPrecision(t *testing.T) {
	prevOS := currentOS
	currentOS = "linux"
	t.Cleanup(func() { currentOS = prevOS })

	// Legitimate files with double dots should pass
	assert.NoError(t, ValidateIncludePattern("/var/log/app..log"))
	assert.NoError(t, ValidateIncludePattern("/var/log/*..*"))
	assert.NoError(t, ValidateIncludePattern("/var/log/my..service..log"))

	// Actual traversal attempts must be rejected
	assert.ErrorIs(t, ValidateIncludePattern("/var/log/../etc/shadow"), ErrPathTraversal)
	assert.ErrorIs(t, ValidateIncludePattern("/var/log/sub/../../etc/passwd"), ErrPathTraversal)
	assert.ErrorIs(t, ValidateIncludePattern("/var/log/{app,../etc}/log"), ErrPathTraversal)
}

func TestPathGuardrail_AnchoredEnvExpansion(t *testing.T) {
	// Leading %ProgramData% expands
	expanded := expandWindowsEnv(`%ProgramData%\MyApp\logs\*.log`)
	assert.True(t, strings.HasPrefix(expanded, `C:\ProgramData\`))

	// Middle of path %ProgramData% does NOT expand
	middle := `C:\logs\%ProgramData%\app.log`
	assert.Equal(t, middle, expandWindowsEnv(middle))
}
