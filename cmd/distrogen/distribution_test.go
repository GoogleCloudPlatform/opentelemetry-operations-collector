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
	"strings"
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

	// The module versions are injected rather than resolved so that generation
	// stays offline. yamlprovider is the only module in the testdata specs that
	// upstream publishes at a version other than the release version.
	registry.moduleVersions = otelModuleVersions{
		"go.opentelemetry.io/collector/confmap/provider/yamlprovider": "v1.27.0",
	}

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
			name:        "boringcrypto_and_gofips140",
			expectedErr: ErrSpecValidationBoringCryptoAndGoFIPS140,
		},
		{
			name:        "gofips140_bool_value",
			expectedErr: ErrSpecValidationInvalidGoFIPS140,
		},
		{
			name:        "gofips140_invalid_version",
			expectedErr: ErrSpecValidationInvalidGoFIPS140,
		},
		{
			name:        "gofips140_purego_build_tag",
			expectedErr: ErrSpecValidationGoFIPS140WithPureGo,
		},
		{
			name:        "gofips140_unsupported_go_version",
			expectedErr: ErrSpecValidationGoFIPS140UnsupportedGoVersion,
		},
		{
			name:        "vendor_deps_without_permanent_ocb",
			expectedErr: ErrSpecValidationVendorDepsWithoutPermanentOCB,
		},
		{
			name:        "missing_otel_version",
			expectedErr: ErrSpecValidationMissingOTelVersion,
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

// TestSpecContribVersionDefault also covers a spec that still sets the removed
// stable version fields, which is warned about but must still load.
func TestSpecContribVersionDefault(t *testing.T) {
	specPath := filepath.Join(t.TempDir(), "spec.yaml")
	err := os.WriteFile(specPath, []byte(`name: basic-distro
opentelemetry_version: 0.161.0
opentelemetry_stable_version: 1.67.0
opentelemetry_contrib_stable_version: 1.0.0
`), DefaultFileMode)
	assert.NilError(t, err)

	spec, err := NewDistributionSpec(specPath)
	assert.NilError(t, err)
	assert.Equal(t, spec.OpenTelemetryContribVersion, "0.161.0")
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

func TestDistributionGenerate_LocalComponentReplaces(t *testing.T) {
	registry := NewRegistry()
	registry.Processors["localproc"] = &RegistryComponent{
		Name:  "localproc",
		GoMod: &GoModuleID{URL: "github.com/example/repo/components/processor/localproc"},
		Path:  "../components/processor/localproc",
	}

	spec := &DistributionSpec{
		Name:                        "test-distro",
		BinaryName:                  "otelcol-test",
		Version:                     "0.165.1",
		OpenTelemetryVersion:        "0.165.0",
		OpenTelemetryContribVersion: "0.165.1",
		GoVersion:                   "1.26.10",
		BuildContainer:              Debian,
		OCBOutputDir:                permanentOCBDirname,
		Components: &DistributionComponents{
			Processors: []string{"localproc"},
		},
	}

	g, err := NewDistributionGenerator(spec, registry, true)
	assert.NilError(t, err)
	t.Cleanup(func() { g.Clean() })

	assert.NilError(t, g.Generate())

	manifestBytes, err := os.ReadFile(filepath.Join(g.GeneratePath, "manifest.yaml"))
	assert.NilError(t, err)
	manifest := string(manifestBytes)

	assert.Assert(t, strings.Contains(manifest, "  path: ../components/processor/localproc\n"), "manifest component entry should retain path:\n%s", manifest)
	assert.Assert(t, strings.Contains(manifest, "replaces:\n- github.com/example/repo/components/processor/localproc => ../../components/processor/localproc\n"), "manifest missing unversioned local replace:\n%s", manifest)
}

func TestDistributionGenerate_LocalTelemetryReplace(t *testing.T) {
	registry := NewRegistry()
	registry.Telemetry["localtelemetry"] = &RegistryComponent{
		Name:  "localtelemetry",
		GoMod: &GoModuleID{URL: "github.com/example/repo/components/telemetry/localtelemetry"},
		Path:  "../components/telemetry/localtelemetry",
	}

	spec := &DistributionSpec{
		Name:                        "test-distro",
		BinaryName:                  "otelcol-test",
		Version:                     "0.165.1",
		OpenTelemetryVersion:        "0.165.0",
		OpenTelemetryContribVersion: "0.165.1",
		GoVersion:                   "1.26.10",
		BuildContainer:              Debian,
		OCBOutputDir:                permanentOCBDirname,
		Components: &DistributionComponents{
			Telemetry: "localtelemetry",
		},
	}

	g, err := NewDistributionGenerator(spec, registry, true)
	assert.NilError(t, err)
	t.Cleanup(func() { g.Clean() })

	assert.NilError(t, g.Generate())

	manifestBytes, err := os.ReadFile(filepath.Join(g.GeneratePath, "manifest.yaml"))
	assert.NilError(t, err)
	manifest := string(manifestBytes)

	assert.Assert(t, strings.Contains(manifest, "\ntelemetry:\n"), "manifest missing telemetry stanza:\n%s", manifest)
	assert.Assert(t, strings.Contains(manifest, "gomod: github.com/example/repo/components/telemetry/localtelemetry v0.165.0\n"), "manifest missing telemetry module:\n%s", manifest)
	assert.Assert(t, strings.Contains(manifest, "replaces:\n- github.com/example/repo/components/telemetry/localtelemetry => ../../components/telemetry/localtelemetry\n"), "manifest missing unversioned local telemetry replace:\n%s", manifest)
}

func TestSpecGoFIPS140(t *testing.T) {
	testCases := []struct {
		name               string
		specYAML           string
		expectedErr        error
		expectedEnabled    bool
		expectedQueryValue string
	}{
		{
			name: "omitted",
			specYAML: `name: test-distro
opentelemetry_version: 0.165.0
go_version: 1.26.10
`,
			expectedEnabled:    false,
			expectedQueryValue: "",
		},
		{
			name: "v1.0.0",
			specYAML: `name: test-distro
opentelemetry_version: 0.165.0
go_version: 1.26.10
gofips140: v1.0.0
`,
			expectedEnabled:    true,
			expectedQueryValue: "v1.0.0",
		},
		{
			name: "v1.26.0",
			specYAML: `name: test-distro
opentelemetry_version: 0.165.0
go_version: 1.26.10
gofips140: v1.26.0
`,
			expectedEnabled:    true,
			expectedQueryValue: "v1.26.0",
		},
		{
			name: "prerelease snapshot tag",
			specYAML: `name: test-distro
opentelemetry_version: 0.165.0
go_version: 1.26.10
gofips140: v1.0.0-c2097c7c
`,
			expectedEnabled:    true,
			expectedQueryValue: "v1.0.0-c2097c7c",
		},
		{
			name: "certified with alpine and cgo off",
			specYAML: `name: test-distro
opentelemetry_version: 0.165.0
go_version: 1.26.10
build_container: alpine
collector_cgo: false
gofips140: certified
`,
			expectedEnabled:    true,
			expectedQueryValue: "certified",
		},
		{
			name: "inprocess mode",
			specYAML: `name: test-distro
opentelemetry_version: 0.165.0
go_version: 1.26.10
gofips140: inprocess
`,
			expectedEnabled:    true,
			expectedQueryValue: "inprocess",
		},
		{
			name: "latest mode",
			specYAML: `name: test-distro
opentelemetry_version: 0.165.0
go_version: 1.26.10
gofips140: latest
`,
			expectedEnabled:    true,
			expectedQueryValue: "latest",
		},
		{
			name: "off coexists with boringcrypto",
			specYAML: `name: test-distro
opentelemetry_version: 0.165.0
go_version: 1.26.10
boringcrypto: true
collector_cgo: true
gofips140: off
`,
			expectedEnabled:    false,
			expectedQueryValue: "off",
		},
		{
			name: "shell metacharacters rejected",
			specYAML: `name: test-distro
opentelemetry_version: 0.165.0
go_version: 1.26.10
gofips140: "v1.0.0; echo pwned"
`,
			expectedErr: ErrSpecValidationInvalidGoFIPS140,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			specPath := filepath.Join(t.TempDir(), "spec.yaml")
			assert.NilError(t, os.WriteFile(specPath, []byte(tc.specYAML), DefaultFileMode))

			spec, err := NewDistributionSpec(specPath)
			if tc.expectedErr != nil {
				assert.ErrorIs(t, err, tc.expectedErr)
				return
			}
			assert.NilError(t, err)
			assert.Equal(t, spec.GoFIPS140Enabled(), tc.expectedEnabled)

			qVal, err := spec.Query("gofips140")
			assert.NilError(t, err)
			assert.Equal(t, qVal, tc.expectedQueryValue)
		})
	}
}

func TestSpecGoFIPS140CacheRoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	specPath := filepath.Join(dir, "spec.yaml")
	err := os.WriteFile(specPath, []byte(`name: test-distro
opentelemetry_version: 0.165.0
go_version: 1.26.10
gofips140: v1.0.0
`), DefaultFileMode)
	assert.NilError(t, err)

	spec, err := NewDistributionSpec(specPath)
	assert.NilError(t, err)

	cachedDir := t.TempDir()
	cachedPath := filepath.Join(cachedDir, "spec.yaml")
	assert.NilError(t, yamlMarshalToFile(spec, cachedPath, DefaultFileMode))

	reloadedSpec, err := NewDistributionSpec(cachedPath)
	assert.NilError(t, err)
	assert.Equal(t, spec.Diff(reloadedSpec), false)
	assert.Equal(t, reloadedSpec.GoFIPS140, "v1.0.0")
}

func TestUpdateDistributionSpecFile_GoFIPS140(t *testing.T) {
	t.Parallel()
	specPath := filepath.Join(t.TempDir(), "spec.yaml")
	initial := `name: test-distro
opentelemetry_version: 0.165.0
go_version: 1.26.10
gofips140: v1.0.0
`
	assert.NilError(t, os.WriteFile(specPath, []byte(initial), DefaultFileMode))
	assert.NilError(t, UpdateDistributionSpecFile(specPath, "gofips140", []byte("v1.26.0"), false))

	spec, err := NewDistributionSpec(specPath)
	assert.NilError(t, err)
	assert.Equal(t, spec.GoFIPS140, "v1.26.0")
	assert.Equal(t, spec.GoFIPS140Enabled(), true)
}
