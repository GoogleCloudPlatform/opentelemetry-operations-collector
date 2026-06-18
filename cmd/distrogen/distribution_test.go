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

package main

import (
	"os"
	"path/filepath"
	"testing"

	"gotest.tools/v3/assert"
)

var (
	testdataSubpath              = filepath.Join("generator", "distribution")
	testdataFullDistributionPath = filepath.Join("testdata", "generator", "distribution")
)

func TestDistributionTemplateGeneration(t *testing.T) {
	registry, err := LoadEmbeddedRegistry()
	assert.NilError(t, err)

	testDirs, err := os.ReadDir(testdataFullDistributionPath)
	assert.NilError(t, err)
	for _, d := range testDirs {
		if !d.IsDir() {
			continue
		}

		name := d.Name()
		t.Run(name, func(t *testing.T) {
			testGeneratorCase(t, registry, name)
		})
	}
}

func testGeneratorCase(t *testing.T, registry *Registry, testFolder string) {
	specPath := filepath.Join(testdataFullDistributionPath, testFolder, "spec.yaml")

	d, err := NewDistributionSpec(specPath)
	assert.NilError(t, err)

	g, err := NewDistributionGenerator(d, registry, true)
	assert.NilError(t, err)

	// If custom templates exist for the test case, use them.
	customTemplates := filepath.Join(testdataFullDistributionPath, testFolder, "templates")
	if _, err := os.Stat(customTemplates); err == nil {
		g.CustomTemplatesDir = os.DirFS(customTemplates)
	}

	err = g.Generate()
	assert.NilError(t, err)
	t.Cleanup(func() {
		g.Clean()
	})

	goldenPath := filepath.Join(testdataFullDistributionPath, testFolder, "golden")
	goldenSubPath := filepath.Join(testdataSubpath, testFolder, "golden")
	assertGoldenFiles(t, g.GeneratePath, goldenPath, goldenSubPath)
}

func TestSpecValidationError(t *testing.T) {
	testCases := []struct {
		name        string
		expectedErr error
	}{
		{
			name:        "boringcrypto_alpine_build_container",
			expectedErr: ErrSpecValidationBoringCryptoWithoutDebian,
		},
		{
			name:        "boringcrypto_cgo_off",
			expectedErr: ErrSpecValidationBoringCryptoWithoutCGO,
		},
		{
			name:        "vendor_deps_without_permanent_ocb",
			expectedErr: ErrSpecValidationVendorDepsWithoutPermanentOCB,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			testConfigPath := filepath.Join("testdata", "invalid", tc.name+".yaml")
			_, err := NewDistributionSpec(testConfigPath)
			assert.ErrorIs(t, err, tc.expectedErr)
		})
	}
}

func TestSpecQuery(t *testing.T) {
	otelVer := "v0.124.0"
	spec := &DistributionSpec{
		OpenTelemetryVersion: otelVer,
	}
	val, err := spec.Query("opentelemetry_version")
	assert.NilError(t, err)
	assert.Equal(t, val, otelVer)
}

func TestSpecQueryNotFound(t *testing.T) {
	spec := &DistributionSpec{}
	_, err := spec.Query("random_field_name")
	assert.ErrorIs(t, err, ErrQueryValueNotFound)
}

func TestDetectDistributionToolChange(t *testing.T) {
	original := &DistributionSpec{
		OpenTelemetryVersion: "v0.100.0",
		GoVersion:            "1.22.0",
		Description:          "original description",
	}

	testCases := []struct {
		name     string
		current  *DistributionSpec
		expected bool
	}{
		{
			name: "no changes",
			current: &DistributionSpec{
				OpenTelemetryVersion: "v0.100.0",
				GoVersion:            "1.22.0",
				Description:          "original description",
			},
			expected: false,
		},
		{
			name: "only description changes",
			current: &DistributionSpec{
				OpenTelemetryVersion: "v0.100.0",
				GoVersion:            "1.22.0",
				Description:          "new description",
			},
			expected: false,
		},
		{
			name: "opentelemetry version changes",
			current: &DistributionSpec{
				OpenTelemetryVersion: "v0.101.0",
				GoVersion:            "1.22.0",
				Description:          "original description",
			},
			expected: true,
		},
		{
			name: "go version changes",
			current: &DistributionSpec{
				OpenTelemetryVersion: "v0.100.0",
				GoVersion:            "1.23.0",
				Description:          "original description",
			},
			expected: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.current.DetectDistributionToolChange(original), tc.expected)
		})
	}
}

func TestDetectProjectToolChange(t *testing.T) {
	original := &DistributionSpec{
		OpenTelemetryVersion: "v0.100.0",
		DistrogenVersion:     "v0.1.0",
		Description:          "original description",
	}

	testCases := []struct {
		name     string
		current  *DistributionSpec
		expected bool
	}{
		{
			name: "no changes",
			current: &DistributionSpec{
				OpenTelemetryVersion: "v0.100.0",
				DistrogenVersion:     "v0.1.0",
				Description:          "original description",
			},
			expected: false,
		},
		{
			name: "only description changes",
			current: &DistributionSpec{
				OpenTelemetryVersion: "v0.100.0",
				DistrogenVersion:     "v0.1.0",
				Description:          "new description",
			},
			expected: false,
		},
		{
			name: "opentelemetry version changes",
			current: &DistributionSpec{
				OpenTelemetryVersion: "v0.101.0",
				DistrogenVersion:     "v0.1.0",
				Description:          "original description",
			},
			expected: true,
		},
		{
			name: "distrogen version changes",
			current: &DistributionSpec{
				OpenTelemetryVersion: "v0.100.0",
				DistrogenVersion:     "v0.2.0",
				Description:          "original description",
			},
			expected: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.current.DetectProjectToolChange(original), tc.expected)
		})
	}
}

func TestUpdateDistributionSpecFile(t *testing.T) {
	testCases := []struct {
		name           string
		initialContent string
		field          string
		value          string
		expectedErr    string
		expectedOutput string
	}{
		{
			name: "successful update preserves comments",
			initialContent: `# a comment
go_version: 1.22.0
`,
			field:          "go_version",
			value:          "1.23.0",
			expectedOutput: `# a comment
go_version: 1.23.0
`,
		},
		{
			name:           "invalid field name error",
			initialContent: "go_version: 1.22.0",
			field:          "invalid_field",
			value:          "value",
			expectedErr:    "is not a valid spec field",
		},
		{
			name:           "ignored field error",
			initialContent: "go_version: 1.22.0",
			field:          "-",
			value:          "value",
			expectedErr:    "cannot be updated",
		},
		{
			name: "non-scalar field update error",
			initialContent: `components:
  receivers: [foo]`,
			field:       "components",
			value:       "bar",
			expectedErr: "is not a scalar type and cannot be updated via CLI",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tempDir := t.TempDir()
			specPath := filepath.Join(tempDir, "spec.yaml")

			err := os.WriteFile(specPath, []byte(tc.initialContent), 0644)
			assert.NilError(t, err)

			err = UpdateDistributionSpecFile(specPath, tc.field, tc.value)
			if tc.expectedErr != "" {
				assert.ErrorContains(t, err, tc.expectedErr)
				return
			}

			assert.NilError(t, err)
			updatedContent, err := os.ReadFile(specPath)
			assert.NilError(t, err)
			assert.Equal(t, string(updatedContent), tc.expectedOutput)
		})
	}
}
