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

package main

import (
	"maps"
	"os"
	"path/filepath"
	"testing"

	"go.yaml.in/yaml/v4"
	"gotest.tools/v3/assert"
)

// distributionConfig has the shape of the configurations that distrogen
// generates for the distributions of this repository.
const distributionConfig = `version: 2
env:
  - CGO_ENABLED=0
builds:
  - id: collector-linux
    goos:
      - linux
    goarch:
      - amd64
      - arm64
    binary: collector
  - id: collector-windows
    goos:
      - windows
    goarch:
      - amd64
    binary: collector
  - id: collector-darwin
    goos:
      - darwin
    goarch:
      - arm64
    binary: collector
nfpms:
  - package_name: collector
    ids:
      - collector-linux
    formats:
      - deb
      - rpm
    contents:
      - src: collector.conf
        dst: /etc/collector/collector.conf
        file_info:
          mode: 0644
archives:
  - ids:
      - collector-linux
      - collector-windows
      - collector-darwin
`

// parseConfig returns the content of the YAML document text.
func parseConfig(t *testing.T, text []byte) map[string]any {
	t.Helper()
	var config map[string]any
	assert.NilError(t, yaml.Unmarshal(text, &config))
	return config
}

// withoutBuilds returns a copy of config that has no builds.
func withoutBuilds(config map[string]any) map[string]any {
	rest := maps.Clone(config)
	delete(rest, "builds")
	return rest
}

func TestSingleTarget(t *testing.T) {
	tests := []struct {
		name           string
		config         string
		goos           string
		goarch         string
		expectedBuilds string
	}{
		{
			name:   "keeps the build for the target and drops its other architecture",
			config: distributionConfig,
			goos:   "linux",
			goarch: "amd64",
			expectedBuilds: `
- id: collector-linux
  goos: [linux]
  goarch: [amd64]
  binary: collector
`,
		},
		{
			name:   "the other architecture of the same build",
			config: distributionConfig,
			goos:   "linux",
			goarch: "arm64",
			expectedBuilds: `
- id: collector-linux
  goos: [linux]
  goarch: [arm64]
  binary: collector
`,
		},
		{
			name:   "the same architecture under another operating system",
			config: distributionConfig,
			goos:   "darwin",
			goarch: "arm64",
			expectedBuilds: `
- id: collector-darwin
  goos: [darwin]
  goarch: [arm64]
  binary: collector
`,
		},
		{
			name: "keeps every build for the target, in order",
			config: `builds:
  - id: collector
    goos: [linux, windows]
    goarch: [amd64]
  - id: other
    goos: [windows]
    goarch: [amd64]
  - id: helper
    goos: [linux]
    goarch: [arm64, amd64]
`,
			goos:   "linux",
			goarch: "amd64",
			expectedBuilds: `
- id: collector
  goos: [linux]
  goarch: [amd64]
- id: helper
  goos: [linux]
  goarch: [amd64]
`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			output, err := singleTarget([]byte(test.config), test.goos, test.goarch)
			assert.NilError(t, err)

			got := parseConfig(t, output)
			var expectedBuilds any
			assert.NilError(t, yaml.Unmarshal([]byte(test.expectedBuilds), &expectedBuilds))
			assert.DeepEqual(t, got["builds"], expectedBuilds)
			assert.DeepEqual(t, withoutBuilds(got), withoutBuilds(parseConfig(t, []byte(test.config))))
		})
	}
}

func TestSingleTargetErrors(t *testing.T) {
	tests := []struct {
		name     string
		config   string
		goos     string
		goarch   string
		expected string
	}{
		{
			name:     "no build is for the operating system",
			config:   distributionConfig,
			goos:     "freebsd",
			goarch:   "amd64",
			expected: "no build in the configuration is for freebsd/amd64",
		},
		{
			name:     "no build for the operating system has the architecture",
			config:   distributionConfig,
			goos:     "windows",
			goarch:   "arm64",
			expected: "no build in the configuration is for windows/arm64",
		},
		{
			name: "a build without goos",
			config: `builds:
  - id: collector
    goarch: [amd64]
`,
			goos:     "linux",
			goarch:   "amd64",
			expected: `build "collector"`,
		},
		{
			name: "a build without goarch and without id",
			config: `builds:
  - goos: [linux]
`,
			goos:     "linux",
			goarch:   "amd64",
			expected: "build 1",
		},
		{
			name: "a build whose goos is not a list",
			config: `builds:
  - id: collector
    goos: linux
    goarch: [amd64]
`,
			goos:     "linux",
			goarch:   "amd64",
			expected: `build "collector"`,
		},
		{
			name: "a build whose goarch is an empty list",
			config: `builds:
  - id: collector
    goos: [linux]
    goarch: []
`,
			goos:     "linux",
			goarch:   "amd64",
			expected: `build "collector"`,
		},
		{
			name: "a build that names its operating system with an alias",
			config: `builds:
  - id: collector
    goos: [&os linux]
    goarch: [amd64]
  - id: other
    goos: [*os]
    goarch: [amd64]
`,
			goos:     "linux",
			goarch:   "amd64",
			expected: `build "other"`,
		},
		{
			name: "a build with a targets list",
			config: `builds:
  - id: collector
    goos: [linux]
    goarch: [amd64]
    targets: [linux_amd64_v1, windows_amd64_v1]
`,
			goos:     "linux",
			goarch:   "amd64",
			expected: `build "collector"`,
		},
		{
			name:     "a configuration without builds",
			config:   "version: 2\n",
			goos:     "linux",
			goarch:   "amd64",
			expected: "no list of builds",
		},
		{
			name:     "builds that are not a list",
			config:   "builds: {}\n",
			goos:     "linux",
			goarch:   "amd64",
			expected: "no list of builds",
		},
		{
			name:     "no configuration",
			config:   "",
			goos:     "linux",
			goarch:   "amd64",
			expected: "no list of builds",
		},
		{
			name:     "not YAML",
			config:   "builds: [",
			goos:     "linux",
			goarch:   "amd64",
			expected: "could not parse the configuration",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := singleTarget([]byte(test.config), test.goos, test.goarch)
			assert.ErrorContains(t, err, test.expected)
		})
	}
}

// TestDistributionConfigs runs singleTarget on the configurations that it is
// there for: the ones that distrogen generated for the distributions of this
// repository.
func TestDistributionConfigs(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("..", "..", "..", "*", ".goreleaser.yaml"))
	assert.NilError(t, err)
	assert.Assert(t, len(paths) > 0, "found no distribution")
	for _, path := range paths {
		t.Run(filepath.Base(filepath.Dir(path)), func(t *testing.T) {
			input, err := os.ReadFile(path)
			assert.NilError(t, err)

			output, err := singleTarget(input, "linux", "amd64")
			assert.NilError(t, err)

			got := parseConfig(t, output)
			builds, _ := got["builds"].([]any)
			assert.Assert(t, len(builds) > 0, "builds: %v", got["builds"])
			for _, build := range builds {
				build, _ := build.(map[string]any)
				assert.DeepEqual(t, build["goos"], []any{"linux"})
				assert.DeepEqual(t, build["goarch"], []any{"amd64"})
			}
			assert.DeepEqual(t, withoutBuilds(got), withoutBuilds(parseConfig(t, input)))
		})
	}
}
