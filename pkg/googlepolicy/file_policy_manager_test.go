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
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestParsePolicyDir(t *testing.T) {
	tests := []struct {
		name     string
		rawURI   string
		expected string
	}{
		{
			name:     "windows opaque URI single colon (file:C:/...)",
			rawURI:   "file:C:/Users/runneradmin/work/policies",
			expected: "C:/Users/runneradmin/work/policies",
		},
		{
			name:     "unix 3 slashes",
			rawURI:   "file:///etc/otel/policies",
			expected: "/etc/otel/policies",
		},
		{
			name:     "unix localhost host",
			rawURI:   "file://localhost/etc/otel/policies",
			expected: "/etc/otel/policies",
		},
		{
			name:     "windows 2 slashes uppercase drive (the exact runner bug: Host=C:, Path=/...)",
			rawURI:   "file://C:/Users/runneradmin/work/policies",
			expected: "C:/Users/runneradmin/work/policies",
		},
		{
			name:     "windows 3 slashes uppercase drive (Host='', Path=/C:/...)",
			rawURI:   "file:///C:/Users/runneradmin/work/policies",
			expected: "C:/Users/runneradmin/work/policies",
		},
		{
			name:     "windows 2 slashes lowercase drive (Host=d:, Path=/...)",
			rawURI:   "file://d:/otel/policies",
			expected: "d:/otel/policies",
		},
		{
			name:     "windows 3 slashes lowercase drive (Host='', Path=/d:/...)",
			rawURI:   "file:///d:/otel/policies",
			expected: "d:/otel/policies",
		},
		{
			name:     "windows path with url-encoded spaces",
			rawURI:   "file:///C:/Program%20Files/otel/policies",
			expected: "C:/Program Files/otel/policies",
		},
		{
			name:     "windows trailing slash",
			rawURI:   "file:///C:/otel/policies/",
			expected: "C:/otel/policies/",
		},
		{
			name:     "unix path with url-encoded spaces",
			rawURI:   "file:///opt/my%20app/policies",
			expected: "/opt/my app/policies",
		},
		{
			name:     "windows drive root 3 slashes",
			rawURI:   "file:///C:/",
			expected: "C:/",
		},
		{
			name:     "windows drive root 2 slashes",
			rawURI:   "file://C:/",
			expected: "C:/",
		},
		{
			name:     "unix root",
			rawURI:   "file:///",
			expected: "/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u, err := url.Parse(tt.rawURI)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, parsePolicyDir(u))
		})
	}
}

func TestNewFilePolicyManager(t *testing.T) {
	tempDir := t.TempDir()
	require.DirExists(t, tempDir)

	logger := zap.NewNop()

	t.Run("valid existing directory 3 slashes (file:///...)", func(t *testing.T) {
		slashPath := filepath.ToSlash(tempDir)
		if !strings.HasPrefix(slashPath, "/") {
			slashPath = "/" + slashPath
		}
		u, err := url.Parse("file://" + slashPath)
		require.NoError(t, err)

		mgr, err := NewFilePolicyManager(logger, u)
		require.NoError(t, err)
		require.NotNil(t, mgr)
		assert.NoError(t, mgr.Stop())
	})

	t.Run("valid existing directory 2 slashes (file://...)", func(t *testing.T) {
		slashPath := filepath.ToSlash(tempDir)
		// On Windows tempDir starts with C:/ so file://C:/... has Host="C:"
		// On Unix tempDir starts with /tmp/... so file:///tmp/...
		u, err := url.Parse("file://" + slashPath)
		require.NoError(t, err)

		mgr, err := NewFilePolicyManager(logger, u)
		require.NoError(t, err)
		require.NotNil(t, mgr)
		assert.NoError(t, mgr.Stop())
	})

	t.Run("non-existent directory preserves parsed path in error", func(t *testing.T) {
		u, err := url.Parse("file://C:/non/existent/policy/dir")
		require.NoError(t, err)

		mgr, err := NewFilePolicyManager(logger, u)
		require.Error(t, err)
		assert.Nil(t, mgr)
		assert.Contains(t, err.Error(), "policy directory does not exist")
		assert.Contains(t, err.Error(), "C:/non/existent/policy/dir")
	})

	t.Run("file instead of directory", func(t *testing.T) {
		filePath := filepath.Join(tempDir, "policy.json")
		require.NoError(t, os.WriteFile(filePath, []byte("{}"), 0644))

		slashPath := filepath.ToSlash(filePath)
		if !strings.HasPrefix(slashPath, "/") {
			slashPath = "/" + slashPath
		}
		u, err := url.Parse("file://" + slashPath)
		require.NoError(t, err)

		mgr, err := NewFilePolicyManager(logger, u)
		require.Error(t, err)
		assert.Nil(t, mgr)
		assert.Contains(t, err.Error(), "is not a directory")
	})

	t.Run("nil uri", func(t *testing.T) {
		mgr, err := NewFilePolicyManager(logger, nil)
		require.Error(t, err)
		assert.Nil(t, mgr)
		assert.Contains(t, err.Error(), "policy URI cannot be nil")
	})
}
