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
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPathGuardrail_ValidateIncludePattern(t *testing.T) {
	t.Run("rejects empty and whitespace-only patterns", func(t *testing.T) {
		assert.ErrorIs(t, validateIncludePattern(""), ErrEmptyIncludePattern)
		assert.ErrorIs(t, validateIncludePattern("   "), ErrEmptyIncludePattern)
		assert.ErrorIs(t, validateIncludePattern("\t\n"), ErrEmptyIncludePattern)
	})

	t.Run("rejects character classes, brace alternation, and non-Windows-drive backslashes", func(t *testing.T) {
		invalidGlobs := []string{
			"[",
			"]",
			"[]",
			"{",
			"}",
			`\`,
			"/var/log/[a-z]*.log",
			"/var/log/[a-z",
			"/var/log/unopened].log",
			"/var/log/[]",
			"/var/log/{nginx,apache2}/*.log",
			"/var/log/{app,web",
			"/var/log/}app",
			`/var/log/app\[1\].log`,
			`/var/log/sub\*.log`,
			`/var/log/trailing\`,
			`\\server\share\logs\*.log`,
			`%ProgramData%\logs\*.log`,
			`1:\ProgramData\logs\*.log`,
			`C:relative\logs\*.log`,
			`ab/logs\*.log`,
			`ab\logs\*.log`,
			`C:\ProgramData\MyApp\logs\[a-z].log`,
			`C:\ProgramData\MyApp\logs\[a-z`,
			`C:\ProgramData\MyApp\logs\{a,b}.log`,
		}
		for _, invalid := range invalidGlobs {
			assert.ErrorIs(t, validateIncludePattern(invalid), ErrInvalidGlobPattern, "pattern %q should be rejected", invalid)
		}
	})

	t.Run("accepts valid simple wildcard glob patterns across OS styles", func(t *testing.T) {
		validGlobs := []string{
			"*",
			"C:",
			"/var/log/*.log",
			"/var/log/**/*.log",
			"/var/log/app_?.log",
			"/var/log/app..log",
			"/var/log/*..*",
			"/var/log/app$log.txt",
			"/var/log/$ENV_VAR/app.log",
			"/var/log/",
			"relative/path/*.log",
			`C:\ProgramData\App\logs\*.log`,
			`C:\ProgramData\logs\**\*.log`,
			`C:\ProgramData\logs\dir\`,
			`C:/ProgramData/App/logs/*.log`,
			`C:/ProgramData/logs\*.log`,
			`C:/ProgramData/logs\dir\`,
		}
		for _, valid := range validGlobs {
			assert.NoError(t, validateIncludePattern(valid), "pattern %q should be accepted", valid)
		}
	})
}

func TestPathGuardrail_NormalizePattern(t *testing.T) {
	// Linux path preserves case unchanged.
	assert.Equal(t, "/var/log/App.log", normalizePattern("/var/log/App.log"))
	assert.Equal(t, "/opt/MyService/logs/*.log", normalizePattern("/opt/MyService/logs/*.log"))

	// Windows path normalizes drive to uppercase, rest to lowercase, separators to '/'.
	assert.Equal(t, "C:/programdata/myapp/logs/*.log", normalizePattern(`C:\ProgramData\MyApp\logs\*.log`))
	assert.Equal(t, "C:/programdata/myapp/logs/*.log", normalizePattern(`c:/ProgramData/MyApp/logs/*.log`))
}
