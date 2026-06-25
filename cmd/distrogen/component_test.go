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
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gotest.tools/v3/assert"
)

func TestComponentGenerator(t *testing.T) {
	// Save current working directory
	oldWD, err := os.Getwd()
	assert.NilError(t, err)

	// Create temp dir
	tempDir, err := os.MkdirTemp("", "component-generator-test")
	assert.NilError(t, err)
	t.Cleanup(func() {
		os.RemoveAll(tempDir)
	})

	// Change to temp dir
	err = os.Chdir(tempDir)
	assert.NilError(t, err)
	t.Cleanup(func() {
		err := os.Chdir(oldWD)
		assert.NilError(t, err)
	})

	spec := &DistributionSpec{
		Name:                "test-collector",
		Module:              "github.com/example/test-collector",
		GoVersion:           "1.23.4",
		ComponentModuleBase: "github.com/example/test-collector/components",
	}

	g, err := NewComponentGenerator(spec, Receiver, "my")
	assert.NilError(t, err)

	err = g.Generate()
	assert.NilError(t, err)

	// Verify go.mod content
	goModPath := filepath.Join(g.Path, "go.mod")
	content, err := os.ReadFile(goModPath)
	assert.NilError(t, err)

	expectedGoVersion := "go 1.23"
	assert.Assert(t, strings.Contains(string(content), expectedGoVersion), "expected %q in go.mod, got %q", expectedGoVersion, string(content))
}
